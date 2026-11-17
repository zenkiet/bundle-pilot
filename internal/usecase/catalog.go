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
	WriteConfig(pb *configv1.Config, by, note string) (revision string, err error)
	History() ([]domain.Revision, error)
	Revision(id string) (*configv1.Config, error)
	PutBundle(ctx context.Context, name string, r io.Reader) (files int, replaced bool, err error)
	DeleteBundle(ctx context.Context, name string) error
	SyncState() (at time.Time, objects int, lastErr string)
	CheckSource(ctx context.Context, src domain.Source) (zips, ignored, wouldRemove []string, err error)
	Record(e domain.Event)
	Events(after int64, limit int) []domain.Event
	TokenUsed(name string, now time.Time)
	TokensUsed() map[string]time.Time
}

var errFirstAdmin = errors.New("finish setup first: save a config that sets the admin username and password")

// Catalog holds the served snapshot; every stored change goes through it and ends with a reload.
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

// Config includes secrets; it is empty before the first save.
func (c *Catalog) Config() (*configv1.Config, error) {
	pb, err := c.src.ReadConfig()
	if errors.Is(err, fs.ErrNotExist) {
		return &configv1.Config{}, nil
	}
	return pb, err
}

// UpdateConfig runs one at a time, so read-modify-writes cannot lose each other, and never
// overwrites an unreadable config.pb, which holds the admin account. It returns the revision id,
// "" when the save made none.
func (c *Catalog) UpdateConfig(by, note string, edit func(cur *configv1.Config) (*configv1.Config, error)) (string, error) {
	c.writing.Lock()
	defer c.writing.Unlock()
	cur, err := c.src.ReadConfig()
	firstSave := c.Current().Setup != ""
	switch {
	case errors.Is(err, fs.ErrNotExist) && firstSave:
		cur = &configv1.Config{}
	case errors.Is(err, fs.ErrNotExist):
		return "", errors.New("config.pb is missing: restore it, or restart the gateway to run setup again")
	case err != nil:
		return "", fmt.Errorf("config.pb is unreadable, so it is not overwritten: %w", err)
	}
	next, err := edit(cur)
	if err == nil {
		_, err = domain.ParseConfig(next)
	}
	if err != nil {
		return "", err
	}
	// The first save ends setup and the setup token with it: without an admin account the
	// admin API would be left open to anyone.
	if firstSave && next.GetAuth().GetUsername() == "" {
		return "", errFirstAdmin
	}
	domain.HashAuth(next)
	rev, err := c.src.WriteConfig(next, by, note)
	if err != nil {
		return "", err
	}
	if err := c.Reload(); err != nil {
		c.log.Error("config saved, but the reload failed, keeping previous snapshot", "error", err)
	}
	return rev, nil
}

// History is newest first.
func (c *Catalog) History() ([]domain.Revision, error) { return c.src.History() }

func (c *Catalog) Revision(id string) (*configv1.Config, error) { return c.src.Revision(id) }

// PutBundle stores outside the reload lock, so a slow upload never blocks the watcher.
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

func (c *Catalog) CheckSource(ctx context.Context, src domain.Source) (zips, ignored, wouldRemove []string, err error) {
	return c.src.CheckSource(ctx, src)
}

func (c *Catalog) TokenUsed(name string, now time.Time) { c.src.TokenUsed(name, now) }

func (c *Catalog) TokensUsed() map[string]time.Time { return c.src.TokensUsed() }

func (c *Catalog) Record(e domain.Event) { c.src.Record(e) }

// Events is oldest first; after 0 gives the newest.
func (c *Catalog) Events(after int64, limit int) []domain.Event { return c.src.Events(after, limit) }

// Reload keeps the current snapshot serving on error.
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
	if prev != nil { // the first load only learns them, so a restart repeats none
		c.recordMissing("issue", snap, prev)
		c.recordMissing("issue_cleared", prev, snap)
	}
	for _, e := range snap.Errors {
		c.log.Error(e.Text, "code", e.Code)
	}
	for _, issue := range snap.Issues {
		c.log.Warn(issue.Text, "code", issue.Code)
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

// recordMissing matches issues on what they are about, not on their text, which may change.
func (c *Catalog) recordMissing(kind string, from, other *domain.Snapshot) {
	key := func(i domain.Issue) domain.Issue {
		return domain.Issue{Code: i.Code, Rule: i.Rule, Bundle: i.Bundle, Token: i.Token}
	}
	seen := map[domain.Issue]bool{}
	for _, i := range slices.Concat(other.Errors, other.Issues) {
		seen[key(i)] = true
	}
	for _, i := range slices.Concat(from.Errors, from.Issues) {
		if seen[key(i)] {
			continue
		}
		seen[key(i)] = true // once per key
		c.src.Record(domain.Event{Kind: kind, Code: i.Code, Rule: i.Rule, Bundle: i.Bundle, Token: i.Token, Reason: i.Reason})
	}
}

// Watch retries a failure that may clear by itself, backing off up to a minute.
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
