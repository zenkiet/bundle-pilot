// Package mirror keeps a local versions directory equal to the zips a Store
// lists, so the rest of the gateway keeps reading plain files.
package mirror

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

// Store is one remote's zips by file name. Put returns the modification time
// the store recorded, so the local copy can match it and skip the next fetch.
type Store interface {
	List(ctx context.Context) ([]Object, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, body io.ReadSeeker, size int64) (time.Time, error)
	Delete(ctx context.Context, key string) error
}

type Object struct {
	Key     string
	Size    int64
	ModTime time.Time
}

// Mirror copies a Store's zips into dir. The fields under mu describe the last sync.
type Mirror struct {
	store Store
	dir   string
	log   *slog.Logger
	mu    sync.Mutex
	err   string    // the last sync error, "" after a success
	at    time.Time // when the last sync ran
	n     int       // how many zips the store listed
}

// New mirrors store into dir; name is the source, for logs.
func New(store Store, dir, name string, log *slog.Logger) *Mirror {
	return &Mirror{store: store, dir: dir, log: log.With("source", name)}
}

// Run syncs every interval until ctx is done.
func (m *Mirror) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = m.Sync(ctx)
		}
	}
}

func (m *Mirror) Status() (at time.Time, objects int, lastErr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.at, m.n, m.err
}

func (m *Mirror) Store() Store { return m.store }

// Sync fetches new or changed zips and deletes those the store no longer lists;
// a failed or empty listing changes nothing.
func (m *Mirror) Sync(ctx context.Context) error {
	n, err := m.sync(ctx)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	m.mu.Lock()
	changed := msg != m.err
	m.err, m.at, m.n = msg, time.Now(), n
	m.mu.Unlock()
	switch {
	case changed && err != nil:
		m.log.Warn("sync failed", "error", err)
	case changed:
		m.log.Info("sync recovered")
	}
	return err
}

func (m *Mirror) sync(ctx context.Context) (int, error) {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(m.dir)
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()
	// Read before listing, so a zip uploaded in between is never taken for one the store dropped.
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return 0, err
	}
	objs, err := m.store.List(ctx)
	if err != nil {
		return 0, err
	}
	want := map[string]Object{}
	for _, o := range objs {
		inFolder := strings.ContainsAny(o.Key, `/\`)
		if strings.HasSuffix(o.Key, ".zip") && !inFolder && !strings.HasPrefix(o.Key, ".") {
			want[o.Key] = o
		}
	}
	if len(want) == 0 {
		return 0, errors.New("lists no zips, keeping the local files")
	}
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(want)) {
		o := want[name]
		info, err := root.Stat(name)
		if err == nil && info.Size() == o.Size && info.ModTime().Unix() == o.ModTime.Unix() {
			continue // already fetched
		}
		start := time.Now()
		if err := m.fetch(ctx, root, o); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		m.log.Info("bundle fetched", "zip", name, "bytes", o.Size, "took", time.Since(start).Round(time.Millisecond).String())
	}
	for _, e := range entries {
		name := e.Name()
		if _, wanted := want[name]; wanted || !strings.HasSuffix(name, ".zip") {
			continue
		}
		if root.Remove(name) == nil {
			m.log.Info("bundle removed", "zip", name)
		}
	}
	return len(want), errors.Join(errs...)
}

func (m *Mirror) fetch(ctx context.Context, root *os.Root, o Object) error {
	rc, err := m.store.Get(ctx, o.Key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	return Replace(root, o.Key, 0o666, func(f *os.File) (time.Time, error) {
		n, err := io.Copy(f, rc)
		if err == nil && n != o.Size {
			err = fmt.Errorf("got %d of %d bytes", n, o.Size)
		}
		return o.ModTime, err
	})
}

func Replace(root *os.Root, name string, perm os.FileMode, write func(*os.File) (time.Time, error)) error {
	dir, base := path.Split(name)
	tmp := dir + "." + base + "." + rand.Text() + ".part"
	f, err := root.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	mt, err := write(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && !mt.IsZero() {
		err = root.Chtimes(tmp, mt, mt)
	}
	if err == nil {
		err = root.Rename(tmp, name)
	}
	if err != nil {
		_ = root.Remove(tmp)
	}
	return err
}
