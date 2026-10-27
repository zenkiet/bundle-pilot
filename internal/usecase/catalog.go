package usecase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
)

// Source is where bundles and config.pb are stored; bundlefs implements it.
type Source interface {
	// Stamp changes whenever something Load reads changes.
	Stamp() uint64
	Load() (domain.Inventory, error)
	ReadConfig() (*configv1.Config, error)
	WriteConfig(pb *configv1.Config, by string) error
	History() ([]domain.Revision, error)
	Revision(id string) (*configv1.Config, error)
	PutBundle(ctx context.Context, name string, r io.Reader) (files int, replaced bool, err error)
	DeleteBundle(ctx context.Context, name string) error
	SyncState() (at time.Time, objects int, lastErr string)
}

// Catalog holds the snapshot being served. Every change to what is stored (config saves,
// uploads, deletes) goes through it and ends with a reload.
type Catalog struct {
	src       Source
	log       *slog.Logger
	reloading sync.Mutex // one reload at a time
	writing   sync.Mutex // one config update at a time
	seen      atomic.Uint64
	retry     atomic.Bool
	reloads   atomic.Uint64
	lastErr   atomic.Pointer[string]
	cur       atomic.Pointer[domain.Snapshot]
}

func NewCatalog(src Source, log *slog.Logger) *Catalog {
	return &Catalog{src: src, log: log}
}

func (c *Catalog) Current() *domain.Snapshot { return c.cur.Load() }

func (c *Catalog) Stats() (reloads uint64, lastErr string) {
	if p := c.lastErr.Load(); p != nil {
		lastErr = *p
	}
	return c.reloads.Load(), lastErr
}

// Config returns config.pb as stored, secrets included; an empty one before the first save.
func (c *Catalog) Config() (*configv1.Config, error) {
	pb, err := c.src.ReadConfig()
	if errors.Is(err, fs.ErrNotExist) {
		return &configv1.Config{}, nil
	}
	return pb, err
}

// UpdateConfig applies edit to the stored config, validates the result, saves it as by and
// reloads. Updates run one at a time, so two read-modify-writes cannot lose each other. A
// config.pb that cannot be read is never overwritten, because it holds the admin account. A
// reload that fails after the save is only logged: it shows as the last error, and the watcher
// retries it.
func (c *Catalog) UpdateConfig(by string, edit func(cur *configv1.Config) (*configv1.Config, error)) error {
	c.writing.Lock()
	defer c.writing.Unlock()
	cur, err := c.src.ReadConfig()
	firstSave := c.Current().Setup != ""
	switch {
	case errors.Is(err, fs.ErrNotExist) && firstSave:
		cur = &configv1.Config{}
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("config.pb is missing: restore it, or restart the gateway to run setup again")
	case err != nil:
		return fmt.Errorf("config.pb is unreadable, so it is not overwritten: %w", err)
	}
	next, err := edit(cur)
	if err == nil {
		_, err = domain.ParseConfig(next)
	}
	if err != nil {
		return err
	}
	domain.HashAuth(next)
	if err := c.src.WriteConfig(next, by); err != nil {
		return err
	}
	if err := c.Reload(); err != nil {
		c.log.Error("config saved, but the reload failed, keeping previous snapshot", "error", err)
	}
	return nil
}

// History lists the saved configs, newest first.
func (c *Catalog) History() ([]domain.Revision, error) { return c.src.History() }

// Revision returns one saved config, as clients see it.
func (c *Catalog) Revision(id string) (*configv1.Config, error) { return c.src.Revision(id) }

// PutBundle stores an uploaded zip and reloads. The upload itself runs outside the reload
// lock, so a slow client never blocks the watcher.
func (c *Catalog) PutBundle(ctx context.Context, name string, r io.Reader) (files int, replaced bool, err error) {
	if files, replaced, err = c.src.PutBundle(ctx, name, r); err == nil {
		err = c.Reload()
	}
	return files, replaced, err
}

func (c *Catalog) DeleteBundle(ctx context.Context, name string) error {
	if err := c.src.DeleteBundle(ctx, name); err != nil {
		return err
	}
	return c.Reload()
}

func (c *Catalog) SyncState() (at time.Time, objects int, lastErr string) { return c.src.SyncState() }

// Reload loads the source again and swaps in the new snapshot. On error the current snapshot
// keeps serving.
func (c *Catalog) Reload() error {
	c.reloading.Lock()
	defer c.reloading.Unlock()
	err := c.reload()
	if err == nil {
		c.reloads.Add(1)
		c.lastErr.Store(nil)
	} else {
		c.lastErr.Store(new(err.Error()))
	}
	return err
}

// reload records the stamp before loading, so a change that lands mid-load leaves a different
// stamp and triggers the next reload.
func (c *Catalog) reload() error {
	start := time.Now()
	c.seen.Store(c.src.Stamp())
	inv, err := c.src.Load()
	c.retry.Store(err != nil || inv.Retry)
	if err != nil {
		return err
	}
	prev, prevDefault := c.cur.Load(), ""
	if prev != nil {
		prevDefault = prev.Default.Version
	}
	snap := domain.NewSnapshot(inv, time.Now(), prevDefault)
	c.cur.Store(snap)
	for _, e := range snap.Errors {
		c.log.Error(e)
	}
	for _, issue := range snap.Issues {
		c.log.Warn(issue)
	}
	versions := make([]string, 0, len(snap.Bundles()))
	for _, b := range snap.Bundles() {
		versions = append(versions, b.Version)
	}
	c.log.Info("snapshot ready",
		"bundles", versions,
		"default", snap.Default.Version,
		"default_from", snap.DefaultFrom,
		"base_path", snap.BasePath,
		"rules", len(snap.Config.Rules),
		"steps", len(snap.Config.Backend),
		"took", time.Since(start).Round(time.Millisecond).String())
	if prev == nil || !slices.Equal(prev.Bundles(), snap.Bundles()) {
		debug.FreeOSMemory() // checking zips leaves tens of MiB of decoder garbage behind
	}
	return nil
}

// Watch reloads after any change on disk. A failure that may clear by itself is retried with
// a backoff that doubles up to a minute.
func (c *Catalog) Watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	backoff, waited := every, time.Duration(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if c.src.Stamp() == c.seen.Load() {
			if !c.retry.Load() {
				continue
			}
			waited += every
			if waited < backoff {
				continue
			}
		}
		if err := c.Reload(); err != nil {
			c.log.Error("reload failed, keeping previous snapshot", "error", err)
		}
		waited, backoff = 0, min(2*backoff, time.Minute)
		if !c.retry.Load() {
			backoff = every
		}
	}
}
