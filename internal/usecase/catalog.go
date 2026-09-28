package usecase

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zenkiet/edge-gateway/internal/domain"
	"github.com/zenkiet/edge-gateway/internal/gen/configv1"
)

type Source interface {
	Stamp() uint64
	Load() (domain.Inventory, error)
	ReadConfig() (*configv1.Config, error)
	WriteConfig(*configv1.Config) error
	PutBundle(ctx context.Context, name string, r io.Reader) (files int, replaced bool, err error)
	DeleteBundle(ctx context.Context, name string) error
	SyncState() (at time.Time, objects int, err string)
}

type Catalog struct {
	src     Source
	log     *slog.Logger
	mu      sync.Mutex
	seen    atomic.Uint64
	retry   atomic.Bool
	reloads atomic.Uint64
	lastErr atomic.Pointer[string]
	cur     atomic.Pointer[domain.Snapshot]
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

func (c *Catalog) Config() (*configv1.Config, error) { return c.src.ReadConfig() }

// SaveConfig writes config.pb and reloads, so the caller sees the outcome.
func (c *Catalog) SaveConfig(pb *configv1.Config) error {
	if err := c.src.WriteConfig(pb); err != nil {
		return err
	}
	return c.Reload()
}

// PutBundle stores an uploaded zip and reloads; the upload itself runs
// outside the lock so a slow client never blocks the watcher.
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

func (c *Catalog) SyncState() (at time.Time, objects int, err string) { return c.src.SyncState() }

func (c *Catalog) Reload() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.reload()
	if err == nil {
		c.reloads.Add(1)
		c.lastErr.Store(nil)
	} else {
		msg := err.Error()
		c.lastErr.Store(&msg)
	}
	return err
}

// reload records the stamp before loading, so a change landing mid-load
// differs from it and triggers the next reload.
func (c *Catalog) reload() error {
	start := time.Now()
	c.seen.Store(c.src.Stamp())
	inv, err := c.src.Load()
	c.retry.Store(err != nil || inv.Retry)
	if err != nil {
		return err
	}
	snap, err := domain.NewSnapshot(inv, time.Now())
	if err != nil {
		if all := slices.Concat(inv.Errors, inv.Issues); len(all) > 0 {
			return fmt.Errorf("%w: %s", err, strings.Join(all, "; "))
		}
		return err
	}
	prev := c.cur.Swap(snap)
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
	c.log.Info("snapshot ready", "bundles", versions, "default", snap.Default.Version, "default_from", snap.DefaultFrom,
		"base_path", snap.BasePath, "rules", len(snap.Config.Rules), "steps", len(snap.Config.Backend),
		"took", time.Since(start).Round(time.Millisecond).String())
	if prev == nil || !slices.Equal(prev.Bundles(), snap.Bundles()) {
		debug.FreeOSMemory()
	}
	return nil
}

// Watch reloads on any change on disk, and retries failures that need none
// with a backoff doubling up to a minute.
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
			if waited += every; waited < backoff {
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
