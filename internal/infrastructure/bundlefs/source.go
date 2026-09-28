package bundlefs

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
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
	"github.com/zenkiet/bundle-pilot/internal/pkg/asset"
)

const (
	versionsDir = "versions"
	configFile  = "config.pb"
	maxConfig   = 1 << 20
)

var errChanged = errors.New("zip changed while loading")

// Source reads <root>/versions/<v>.zip and <root>/config.pb, and runs the mirror
// the config's source block asks for. Load must not run concurrently; Stamp,
// PutBundle and DeleteBundle may run alongside it.
type Source struct {
	fsys   fs.FS
	dir    string
	log    *slog.Logger
	key    ed25519.PublicKey
	cache  map[string]entry
	config domain.Config
	active domain.Source
	mirror atomic.Pointer[mirror.Mirror]
	stop   context.CancelFunc
}

type entry struct {
	fp     uint64
	bundle *domain.Bundle
}

type version struct {
	name, path string
	size       int64
	mtime      time.Time
	fp         uint64
}

func New(fsys fs.FS, dir string, log *slog.Logger, key ed25519.PublicKey) *Source {
	return &Source{fsys: fsys, dir: dir, log: log, key: key, cache: map[string]entry{}}
}

// Stamp hashes config.pb metadata and every zip's size and mtime, so a
// rewritten file is noticed, not only a renamed one.
func (s *Source) Stamp() uint64 {
	h := fnv.New64a()
	var b [8]byte
	if info, err := fs.Stat(s.fsys, configFile); err == nil {
		_, _ = h.Write(binary.LittleEndian.AppendUint64(b[:0], fingerprint(info)))
	}
	vs, _ := s.versions()
	for _, v := range vs {
		_, _ = h.Write([]byte(v.path))
		_, _ = h.Write(binary.LittleEndian.AppendUint64(b[:0], v.fp))
	}
	if m := s.mirror.Load(); m != nil {
		_, gen := m.State()
		_, _ = h.Write(binary.LittleEndian.AppendUint64(b[:0], gen))
	}
	return h.Sum64()
}

func (s *Source) Load() (domain.Inventory, error) {
	inv := domain.Inventory{Signed: s.key != nil}
	inv.Config = s.loadConfig(&inv)
	if err := s.applySource(inv.Config.Source); err != nil {
		inv.Errors = append(inv.Errors, "source "+inv.Config.Source.String()+": "+err.Error())
	}
	if err := s.sourceErr(); err != nil {
		inv.Issues = append(inv.Issues, err.Error())
	}
	vs, err := s.versions()
	if err != nil {
		return inv, cmp.Or(s.sourceErr(), err)
	}
	blobs := map[[32]byte]*asset.Blob{}
	for _, e := range s.cache {
		for _, a := range e.bundle.All() {
			blobs[a.Blob().Sum()] = a.Blob()
		}
	}
	next := make(map[string]entry, len(vs))
	var stale []version
	for _, v := range vs {
		if e, ok := s.cache[v.name]; ok && e.fp == v.fp {
			next[v.name] = e
		} else {
			stale = append(stale, v)
		}
	}
	if len(stale) > 0 {
		start := time.Now()
		bundles, errs := loadBundles(s.fsys, s.key, stale, blobs)
		var loaded []string
		for i, v := range stale {
			err := errs[i]
			if info, serr := fs.Stat(s.fsys, v.path); err == nil && (serr != nil || fingerprint(info) != v.fp) {
				err = errChanged
			}
			if err != nil {
				s.keepPrevious(&inv, next, v.name, err)
				continue
			}
			next[v.name] = entry{v.fp, bundles[i]}
			loaded = append(loaded, v.name)
		}
		if len(loaded) > 0 {
			s.log.Info("bundles loaded", "versions", loaded, "took", time.Since(start).Round(time.Millisecond).String())
		}
	}
	for _, v := range vs {
		if e, ok := next[v.name]; ok {
			inv.Bundles = append(inv.Bundles, e.bundle)
		}
	}
	if len(next) > 0 {
		s.cache = next
	}
	if len(inv.Bundles) == 0 {
		return inv, s.sourceErr()
	}
	return inv, nil
}

// sourceErr is the mirror's last failure: when nothing loads, that is the
// cause worth showing rather than the empty directory.
func (s *Source) sourceErr() error {
	if m := s.mirror.Load(); m != nil {
		if msg, _ := m.State(); msg != "" {
			return errors.New("source " + s.active.String() + ": " + msg)
		}
	}
	return nil
}

// applySource swaps the mirror when config.json changes where zips come
// from. A new remote syncs once inline, so its zips are there for the load;
// one that cannot be set up leaves the source local until the next reload.
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
	m := mirror.New(store, filepath.Join(s.dir, versionsDir), src.String(), s.log)
	_ = m.Sync(ctx)
	s.mirror.Store(m)
	s.stop = cancel
	go m.Run(ctx, src.Poll)
	return nil
}

func (s *Source) keepPrevious(inv *domain.Inventory, next map[string]entry, name string, err error) {
	inv.Retry = inv.Retry || !errors.Is(err, domain.ErrNoIndex)
	if prev, ok := s.cache[name]; ok {
		next[name] = prev
		inv.Issues = append(inv.Issues, fmt.Sprintf("bundle %s: %v, keeping previous load", name, err))
		return
	}
	inv.Errors = append(inv.Errors, fmt.Sprintf("bundle %s skipped: %v", name, err))
}

// loadConfig keeps the last good config when the file is unreadable or invalid;
// on a first boot that means an empty config, so everyone gets the default.
func (s *Source) loadConfig(inv *domain.Inventory) domain.Config {
	info, err := fs.Stat(s.fsys, configFile)
	if errors.Is(err, fs.ErrNotExist) {
		s.config = domain.Config{}
		inv.Setup = true
		msg := "config.pb not found: no rules, every visitor gets the default bundle"
		if _, err := fs.Stat(s.fsys, "config.json"); err == nil {
			msg += "; convert config.json with: gateway import config.json"
		}
		inv.Issues = append(inv.Issues, msg)
		return s.config
	}
	if err == nil && info.Size() > maxConfig {
		err = errors.New("larger than 1 MiB")
	}
	var data []byte
	if err == nil {
		data, err = fs.ReadFile(s.fsys, configFile)
	}
	if err != nil {
		inv.Errors = append(inv.Errors, "config.pb unreadable, keeping previous: "+err.Error())
		return s.config
	}
	pb, err := domain.DecodeConfig(data, false)
	var c domain.Config
	if err == nil {
		c, err = domain.ParseConfig(pb)
	}
	if err != nil {
		inv.Errors = append(inv.Errors, "config.pb rejected, keeping previous: "+err.Error())
		return s.config
	}
	s.config = c
	return c
}

func (s *Source) ReadConfig() (*configv1.Config, error) {
	data, err := fs.ReadFile(s.fsys, configFile)
	if errors.Is(err, fs.ErrNotExist) {
		return &configv1.Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	return domain.DecodeConfig(data, false)
}

// WriteConfig replaces config.pb atomically; the watcher picks it up.
func (s *Source) WriteConfig(pb *configv1.Config) error {
	data, err := domain.EncodeConfig(pb)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.WriteFile("."+configFile+".part", data, 0o600); err != nil {
		return err
	}
	return root.Rename("."+configFile+".part", configFile)
}

// PutBundle stores <name>.zip once it proves loadable. A remote source gets it
// first, and the local copy takes the store's mtime so the mirror keeps it.
func (s *Source) PutBundle(ctx context.Context, name string, r io.Reader) (files int, replaced bool, err error) {
	root, err := s.versionsRoot()
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = root.Close() }()
	tmp := fmt.Sprintf(".%s.%d.part", name, time.Now().UnixNano())
	f, err := root.Create(tmp)
	if err != nil {
		return 0, false, err
	}
	defer func() {
		if err != nil {
			_ = root.Remove(tmp)
		}
	}()
	size, err := io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, false, err
	}
	items, closer, err := open(s.fsys, version{name: name, path: path.Join(versionsDir, tmp), size: size})
	if err != nil {
		return 0, false, fmt.Errorf("not a zip: %w", err)
	}
	if items["/index.html"].open == nil {
		err = domain.ErrNoIndex
	} else if s.key != nil {
		err = verify(s.key, items, nil, 0)
	}
	_ = closer.Close()
	if err != nil {
		return 0, false, err
	}
	if m := s.mirror.Load(); m != nil {
		f, err := root.Open(tmp)
		if err != nil {
			return 0, false, err
		}
		mt, err := m.Store().Put(ctx, name+".zip", f, size)
		_ = f.Close()
		if err != nil {
			return 0, false, fmt.Errorf("upload to %s: %w", s.active.String(), err)
		}
		if err := root.Chtimes(tmp, mt, mt); err != nil {
			return 0, false, err
		}
	}
	_, statErr := root.Stat(name + ".zip")
	if err = root.Rename(tmp, name+".zip"); err != nil {
		return 0, false, err
	}
	return len(items), statErr == nil, nil
}

func (s *Source) DeleteBundle(ctx context.Context, name string) error {
	root, err := s.versionsRoot()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if m := s.mirror.Load(); m != nil {
		if err := m.Store().Delete(ctx, name+".zip"); err != nil {
			return fmt.Errorf("delete from %s: %w", s.active.String(), err)
		}
	}
	return root.Remove(name + ".zip")
}

func (s *Source) SyncState() (at time.Time, objects int, err string) {
	if m := s.mirror.Load(); m != nil {
		return m.Status()
	}
	return time.Time{}, 0, ""
}

func (s *Source) versionsRoot() (*os.Root, error) {
	dir := filepath.Join(s.dir, versionsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

func (s *Source) versions() ([]version, error) {
	entries, err := fs.ReadDir(s.fsys, versionsDir)
	if err != nil {
		return nil, err
	}
	var out []version
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".zip")
		if !ok || strings.HasPrefix(name, ".") {
			continue
		}
		p := path.Join(versionsDir, e.Name())
		if info, err := fs.Stat(s.fsys, p); err == nil && info.Mode().IsRegular() {
			out = append(out, version{name, p, info.Size(), info.ModTime(), fingerprint(info)})
		}
	}
	return out, nil
}

func fingerprint(info fs.FileInfo) uint64 {
	return uint64(info.Size())<<20 ^ uint64(info.ModTime().UnixNano())
}
