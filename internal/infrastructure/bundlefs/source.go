package bundlefs

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"hash/maphash"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
	"github.com/zenkiet/bundle-pilot/internal/infrastructure/mirror"
	"github.com/zenkiet/bundle-pilot/internal/infrastructure/mirror/s3"
)

const (
	versionsDir = "versions"
	configFile  = "config.pb"
)

// seed keys Stamp's hash, which only has to compare within one run.
var seed = maphash.MakeSeed()

// Source reads DIST/versions/*.zip and DIST/config.pb and runs the configured
// mirror. Load must not run concurrently; Stamp, PutBundle and DeleteBundle may.
type Source struct {
	root       *os.Root // DIST
	log        *slog.Logger
	key        ed25519.PublicKey
	cache      map[string]zipped // the zips of the last load, by version
	haveConfig bool              // a config.pb has loaded since start
	setup      string            // the first-run token while config.pb is missing
	active     domain.Source     // the source the mirror runs for
	mirror     atomic.Pointer[mirror.Mirror]
	stop       context.CancelFunc
}

// zipFile is a zip found in versions/.
type zipFile struct {
	name string // the version, the file name without .zip
	fp   [2]int64
}

// New opens the DIST directory dir.
func New(dir string, log *slog.Logger, key ed25519.PublicKey) (*Source, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Source{root: root, log: log, key: key, cache: map[string]zipped{}}, nil
}

// Stamp changes when config.pb or any zip is written to, or the mirror's last error changes.
func (s *Source) Stamp() uint64 {
	var h maphash.Hash
	h.SetSeed(seed)
	if info, err := s.root.Stat(configFile); err == nil {
		maphash.WriteComparable(&h, fingerprint(info))
	}
	zips, _ := s.listZips()
	for _, file := range zips {
		maphash.WriteComparable(&h, file)
	}
	if m := s.mirror.Load(); m != nil {
		_, _, lastErr := m.Status()
		maphash.WriteComparable(&h, lastErr)
	}
	return h.Sum64()
}

// Load reads config.pb and every zip. A zip whose file has not changed since the last load is
// reused without being checked again.
func (s *Source) Load() (domain.Inventory, error) {
	inv := domain.Inventory{Signed: s.key != nil}
	var err error
	if inv.Config, err = s.loadConfig(&inv); err != nil {
		return inv, err
	}
	if err := s.applySource(inv.Config.Source); err != nil {
		inv.Errors = append(inv.Errors, "source "+inv.Config.Source.String()+": "+err.Error())
	}
	if err := s.sourceErr(); err != nil {
		inv.Issues = append(inv.Issues, err.Error())
	}
	zips, err := s.listZips()
	if err != nil {
		return inv, cmp.Or(s.sourceErr(), err)
	}

	next := make(map[string]zipped, len(zips))
	var changed []zipFile
	for _, file := range zips {
		if z, ok := s.cache[file.name]; ok && z.fp == file.fp {
			next[file.name] = z
		} else {
			changed = append(changed, file)
		}
	}
	if len(changed) > 0 {
		start := time.Now()
		loaded, errs := loadBundles(s.root, s.key, changed)
		var names []string
		for i, file := range changed {
			if errs[i] != nil {
				s.keepPrevious(&inv, next, file.name, errs[i])
				continue
			}
			next[file.name] = loaded[i]
			names = append(names, file.name)
		}
		if len(names) > 0 {
			s.log.Info("bundles loaded", "versions", names, "took", time.Since(start).Round(time.Millisecond).String())
		}
	}

	for _, file := range zips {
		if z, ok := next[file.name]; ok {
			inv.Bundles = append(inv.Bundles, z.bundle)
			inv.Issues = append(inv.Issues, z.issues...)
		}
	}
	s.cache = next
	return inv, nil
}

// keepPrevious handles a zip that failed to load: its previous load keeps serving while that
// file is unchanged, as after a replace by rename; else the zip is skipped.
func (s *Source) keepPrevious(inv *domain.Inventory, next map[string]zipped, name string, err error) {
	// A missing index.html stays missing; other failures, such as a half-copied zip, may clear.
	inv.Retry = inv.Retry || !errors.Is(err, domain.ErrNoIndex)
	if prev, ok := s.cache[name]; ok && prev.unchanged() {
		next[name] = prev
		inv.Issues = append(inv.Issues, fmt.Sprintf("bundle %s: %v, keeping previous load", name, err))
		return
	}
	inv.Errors = append(inv.Errors, fmt.Sprintf("bundle %s skipped: %v", name, err))
}

// unchanged reports whether z's open file still holds the bytes it was loaded from: a replace
// by rename leaves it alone, a write in place does not.
func (z zipped) unchanged() bool {
	info, err := z.f.Stat()
	return err == nil && fingerprint(info) == z.fp
}

// sourceErr is the mirror's last failure, the cause worth showing when versions/ cannot be read.
func (s *Source) sourceErr() error {
	if m := s.mirror.Load(); m != nil {
		if _, _, lastErr := m.Status(); lastErr != "" {
			return errors.New("source " + s.active.String() + ": " + lastErr)
		}
	}
	return nil
}

// applySource swaps the mirror when the source changes. A new remote syncs once before
// returning, so this load sees its zips, for at most 20 s while bundles already serve; one that
// fails to set up is retried next reload.
func (s *Source) applySource(src domain.Source) error {
	if src == s.active {
		return nil
	}
	if s.stop != nil {
		s.stop()
		s.stop = nil
		s.mirror.Store(nil)
	}
	s.active = src
	s.log.Info("bundle source", "source", src.String())
	if src.Type == "" {
		return nil
	}
	store, err := s3.New(src)
	if err != nil {
		s.active = domain.Source{}
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := mirror.New(store, filepath.Join(s.root.Name(), versionsDir), src.String(), s.log)
	first := ctx
	if len(s.cache) > 0 { // a slow bucket must not hold up reloads and saves; Run fetches the rest
		var stop context.CancelFunc
		first, stop = context.WithTimeout(ctx, 20*time.Second)
		defer stop()
	}
	_ = m.Sync(first)
	s.mirror.Store(m)
	s.stop = cancel
	go m.Run(ctx, src.Poll)
	return nil
}

// loadConfig reads config.pb. One that goes missing, cannot be read or is rejected fails the
// load, so the snapshot already serving stays. Setup mode, with the admin API open, is only for
// a start without config.pb.
func (s *Source) loadConfig(inv *domain.Inventory) (domain.Config, error) {
	pb, err := s.ReadConfig()
	switch {
	case errors.Is(err, fs.ErrNotExist) && s.haveConfig:
		return domain.Config{}, errors.New("config.pb missing: restore it, or restart to run setup again")
	case errors.Is(err, fs.ErrNotExist):
		s.setup = cmp.Or(s.setup, rand.Text())
		inv.Setup = s.setup
		msg := "config.pb not found: no rules, every visitor gets the default bundle"
		if _, err := s.root.Stat("config.json"); err == nil {
			msg += "; convert config.json with: gateway import config.json"
		}
		inv.Issues = append(inv.Issues, msg)
		return domain.ParseConfig(&configv1.Config{}) // the defaults, such as the date layout
	}
	var c domain.Config
	if err == nil {
		c, err = domain.ParseConfig(pb)
	}
	if err != nil {
		return c, fmt.Errorf("config.pb: %w (fix it, or remove it and restart to run setup again)", err)
	}
	s.haveConfig = true
	return c, nil
}

// ReadConfig returns config.pb as stored, or fs.ErrNotExist before the first save.
func (s *Source) ReadConfig() (*configv1.Config, error) {
	data, err := s.root.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	return domain.DecodeConfig(data, false)
}

// WriteConfig replaces config.pb atomically, then records it in DIST/history as saved by by.
// The watcher picks up the new file.
func (s *Source) WriteConfig(pb *configv1.Config, by string) error {
	data, err := domain.EncodeConfig(pb)
	if err != nil {
		return err
	}
	old, err := s.ReadConfig()
	if err != nil {
		old = nil // missing or broken: nothing worth recording
	}
	var oldAt time.Time
	if info, err := s.root.Stat(configFile); err == nil {
		oldAt = info.ModTime()
	}
	err = mirror.Replace(s.root, configFile, 0o600, func(f *os.File) (time.Time, error) {
		_, err := f.Write(data)
		if err == nil {
			err = f.Sync() // a crash must not leave an empty config.pb, which would load without auth
		}
		return time.Time{}, err
	})
	if err != nil {
		return err
	}
	if err := s.record(old, oldAt, pb, by); err != nil {
		s.log.Warn("config saved, but not recorded in history/", "error", err)
	}
	return nil
}

// PutBundle stores <name>.zip once it proves loadable. A remote source gets it first, and the
// local copy takes the remote's modification time, so the mirror does not fetch it again.
func (s *Source) PutBundle(ctx context.Context, name string, r io.Reader) (files int, replaced bool, err error) {
	if err := s.root.MkdirAll(versionsDir, 0o755); err != nil {
		return 0, false, err
	}
	dst := path.Join(versionsDir, name+".zip")
	_, err = s.root.Stat(dst)
	replaced = err == nil
	err = mirror.Replace(s.root, dst, 0o666, func(f *os.File) (time.Time, error) {
		size, err := io.Copy(f, r)
		if err != nil {
			return time.Time{}, err
		}
		z, err := loadZip(f, s.key, name)
		if err != nil {
			return time.Time{}, err
		}
		files = z.bundle.Len()
		m := s.mirror.Load()
		if m == nil {
			return time.Time{}, nil
		}
		mt, err := m.Store().Put(ctx, name+".zip", io.NewSectionReader(f, 0, size), size)
		if err != nil {
			return time.Time{}, fmt.Errorf("upload to the bucket: %w", err)
		}
		return mt, nil
	})
	return files, replaced, err
}

func (s *Source) DeleteBundle(ctx context.Context, name string) error {
	if m := s.mirror.Load(); m != nil {
		if err := m.Store().Delete(ctx, name+".zip"); err != nil {
			return fmt.Errorf("delete from the bucket: %w", err)
		}
	}
	return s.root.Remove(path.Join(versionsDir, name+".zip"))
}

func (s *Source) SyncState() (at time.Time, objects int, lastErr string) {
	if m := s.mirror.Load(); m != nil {
		return m.Status()
	}
	return time.Time{}, 0, ""
}

// listZips lists the zips in versions/, skipping hidden and partial files.
func (s *Source) listZips() ([]zipFile, error) {
	entries, err := fs.ReadDir(s.root.FS(), versionsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // no versions/ yet
	}
	var out []zipFile
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".zip")
		if !ok || strings.HasPrefix(name, ".") {
			continue
		}
		if info, err := s.root.Stat(path.Join(versionsDir, e.Name())); err == nil && info.Mode().IsRegular() {
			out = append(out, zipFile{name: name, fp: fingerprint(info)})
		}
	}
	return out, err
}

// fingerprint changes with any write to the file that moves its size or mtime.
func fingerprint(info fs.FileInfo) [2]int64 {
	return [2]int64{info.Size(), info.ModTime().UnixNano()}
}
