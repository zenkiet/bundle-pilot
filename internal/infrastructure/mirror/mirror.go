// Package mirror keeps a local versions directory equal to the zips a Store
// lists, so the rest of the gateway keeps reading plain files.
package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
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

type Mirror struct {
	store Store
	dir   string
	name  string
	log   *slog.Logger
	mu    sync.Mutex
	err   string
	gen   uint64
	at    time.Time
	n     int
}

func New(store Store, dir, name string, log *slog.Logger) *Mirror {
	return &Mirror{store: store, dir: dir, name: name, log: log}
}

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

// State returns the last sync error and a counter that moves when it changes.
func (m *Mirror) State() (string, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err, m.gen
}

func (m *Mirror) Status() (at time.Time, objects int, err string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.at, m.n, m.err
}

func (m *Mirror) Store() Store { return m.store }

// Sync downloads new or changed zips through a temp file and deletes the ones
// the store no longer lists. A failed listing changes nothing on disk.
func (m *Mirror) Sync(ctx context.Context) error {
	n, err := m.sync(ctx)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	m.mu.Lock()
	changed := msg != m.err
	if changed {
		m.err = msg
		m.gen++
	}
	m.at, m.n = time.Now(), n
	m.mu.Unlock()
	switch {
	case changed && err != nil:
		m.log.Warn("sync failed", "source", m.name, "error", err)
	case changed:
		m.log.Info("sync recovered", "source", m.name)
	}
	return err
}

func (m *Mirror) sync(ctx context.Context) (int, error) {
	objs, err := m.store.List(ctx)
	if err != nil {
		return 0, err
	}
	want := map[string]Object{}
	for _, o := range objs {
		if strings.HasSuffix(o.Key, ".zip") && !strings.ContainsAny(o.Key, `/\`) && !strings.HasPrefix(o.Key, ".") {
			want[o.Key] = o
		}
	}
	if len(want) == 0 {
		return 0, errors.New("lists no zips, keeping the local files")
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(m.dir)
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(want)) {
		o := want[name]
		if info, err := root.Stat(name); err == nil && info.Size() == o.Size && info.ModTime().Unix() == o.ModTime.Unix() {
			continue
		}
		start := time.Now()
		if err := m.fetch(ctx, root, o); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		m.log.Info("bundle fetched", "source", m.name, "zip", name, "bytes", o.Size, "took", time.Since(start).Round(time.Millisecond).String())
	}
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".zip") && want[name].Key == "" {
			if root.Remove(name) == nil {
				m.log.Info("bundle removed", "source", m.name, "zip", name)
			}
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
	tmp := "." + o.Key + ".part"
	f, err := root.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, rc)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != o.Size {
		err = fmt.Errorf("got %d of %d bytes", n, o.Size)
	}
	if err == nil {
		err = root.Chtimes(tmp, o.ModTime, o.ModTime)
	}
	if err == nil {
		err = root.Rename(tmp, o.Key)
	}
	if err != nil {
		_ = root.Remove(tmp)
	}
	return err
}
