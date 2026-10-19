package bundlefs

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
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
	maxConfig   = 1 << 20
)

var errChanged = errors.New("zip changed while loading")

// Source reads DIST/versions/*.zip and DIST/config.pb and runs the configured
// mirror. Load must not run concurrently; Stamp, PutBundle and DeleteBundle may.
type Source struct {
	fsys       fs.FS
	dir        string
	log        *slog.Logger
	key        ed25519.PublicKey
	cache      map[string]entry // the zips of the last load, by version
	config     domain.Config    // the last config that loaded
	haveConfig bool             // a config.pb has loaded since start
	setup      string           // the first-run token while config.pb is missing
	active     domain.Source    // the source the mirror runs for
	mirror     atomic.Pointer[mirror.Mirror]
	stop       context.CancelFunc
}

// entry is a loaded zip. pathFP is the fingerprint of its path when it was listed; fileFP is
// the fingerprint of its open file when it last passed a check. They differ once the path is
// replaced by another file.
type entry struct {
	pathFP uint64
	fileFP uint64
	zipped
}

// zipFile is a zip found in versions/.
type zipFile struct {
	name string // the version, the file name without .zip
	path string
	fp   uint64
}

func New(dir string, log *slog.Logger, key ed25519.PublicKey) *Source {
	return &Source{fsys: os.DirFS(dir), dir: dir, log: log, key: key, cache: map[string]entry{}}
}

// Stamp changes when config.pb or any zip is written to, or the mirror's state moves.
func (s *Source) Stamp() uint64 {
	h := fnv.New64a()
	var b [8]byte
	if info, err := fs.Stat(s.fsys, configFile); err == nil {
		_, _ = h.Write(binary.LittleEndian.AppendUint64(b[:0], fingerprint(info)))
	}
	zips, _ := s.listZips()
	for _, file := range zips {
		_, _ = h.Write([]byte(file.path))
		_, _ = h.Write(binary.LittleEndian.AppendUint64(b[:0], file.fp))
	}
	if m := s.mirror.Load(); m != nil {
		_, gen := m.State()
		_, _ = h.Write(binary.LittleEndian.AppendUint64(b[:0], gen))
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

	next := make(map[string]entry, len(zips))
	var changed []zipFile
	for _, file := range zips {
		if e, ok := s.cache[file.name]; ok && e.pathFP == file.fp {
			next[file.name] = e
		} else {
			changed = append(changed, file)
		}
	}
	if len(changed) > 0 {
		start := time.Now()
		loaded, errs := loadBundles(s.fsys, s.key, changed)
		var names []string
		for i, file := range changed {
			if errs[i] != nil {
				s.keepPrevious(&inv, next, file.name, errs[i])
				continue
			}
			next[file.name] = entry{pathFP: file.fp, fileFP: fingerprint(loaded[i].info), zipped: loaded[i]}
			names = append(names, file.name)
		}
		if len(names) > 0 {
			s.log.Info("bundles loaded", "versions", names, "took", time.Since(start).Round(time.Millisecond).String())
		}
	}

	for _, file := range zips {
		if e, ok := next[file.name]; ok {
			inv.Bundles = append(inv.Bundles, e.bundle)
			inv.Issues = append(inv.Issues, e.issues...)
		}
	}
	s.keepVanished(&inv, zips, next)
	if len(next) > 0 {
		s.cache = next
	}
	return inv, nil
}

// keepVanished keeps serving a zip that left versions/, from its still-open file, while the
// config names it the default or no zip is left at all. A stray delete, on disk or in the
// bucket, then never moves every visitor at once.
func (s *Source) keepVanished(inv *domain.Inventory, zips []zipFile, next map[string]entry) {
	listed := make(map[string]bool, len(zips))
	for _, file := range zips {
		listed[file.name] = true
	}
	for _, name := range slices.Sorted(maps.Keys(s.cache)) {
		needed := len(zips) == 0 || name == inv.Config.Default
		if listed[name] || !needed {
			continue
		}
		e := s.cache[name]
		if !s.reverify(&e, name) {
			continue
		}
		next[name] = e
		inv.Bundles = append(inv.Bundles, e.bundle)
		inv.Errors = append(inv.Errors, "bundle "+name+": zip gone from versions/, serving the copy loaded before "+
			"until a restart: restore the zip, or set another default")
	}
}

// reverify checks a loaded zip's open file again (CRCs and signature) before it serves on:
// a zip rewritten in place would otherwise go out unchecked. Only a file that changed since
// its last check is read again. The file is never closed, as live snapshots still read from it.
func (s *Source) reverify(e *entry, name string) bool {
	info, err := e.f.Stat()
	if err != nil {
		return false
	}
	if fp := fingerprint(info); fp != e.fileFP {
		if _, err := loadZip(e.f, s.key, name); err != nil {
			return false
		}
		e.fileFP = fp
	}
	return true
}

// sourceErr is the mirror's last failure, the cause worth showing when versions/ cannot be read.
func (s *Source) sourceErr() error {
	if m := s.mirror.Load(); m != nil {
		if msg, _ := m.State(); msg != "" {
			return errors.New("source " + s.active.String() + ": " + msg)
		}
	}
	return nil
}

// applySource swaps the mirror when the source changes. A new remote syncs once before
// returning, so this load sees its zips; one that fails to set up is retried next reload.
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

// keepPrevious handles a zip that failed to load: its previous load keeps serving if that file
// still checks out, else the zip is skipped.
func (s *Source) keepPrevious(inv *domain.Inventory, next map[string]entry, name string, err error) {
	// A missing index.html stays missing; other failures, such as a half-copied zip, may clear.
	inv.Retry = inv.Retry || !errors.Is(err, domain.ErrNoIndex)
	if prev, ok := s.cache[name]; ok && s.reverify(&prev, name) {
		next[name] = prev
		inv.Issues = append(inv.Issues, fmt.Sprintf("bundle %s: %v, keeping previous load", name, err))
		return
	}
	inv.Errors = append(inv.Errors, fmt.Sprintf("bundle %s skipped: %v", name, err))
}

// loadConfig keeps the last good config when the file is unreadable, invalid or gone. Setup
// mode, with the admin API open, is only for a start without config.pb.
func (s *Source) loadConfig(inv *domain.Inventory) (domain.Config, error) {
	info, err := fs.Stat(s.fsys, configFile)
	if errors.Is(err, fs.ErrNotExist) {
		if s.haveConfig {
			inv.Errors = append(inv.Errors,
				"config.pb missing, keeping the loaded config: restore it, or restart to run setup again")
			return s.config, nil
		}
		s.config, _ = domain.ParseConfig(&configv1.Config{}) // the defaults, such as the date layout
		s.setup = cmp.Or(s.setup, rand.Text())
		inv.Setup = s.setup
		msg := "config.pb not found: no rules, every visitor gets the default bundle"
		if _, err := fs.Stat(s.fsys, "config.json"); err == nil {
			msg += "; convert config.json with: gateway import config.json"
		}
		inv.Issues = append(inv.Issues, msg)
		return s.config, nil
	}
	if err == nil && info.Size() > maxConfig {
		err = errors.New("larger than 1 MiB")
	}
	var data []byte
	if err == nil {
		data, err = fs.ReadFile(s.fsys, configFile)
	}
	if err != nil {
		return s.keepConfig(inv, "unreadable", err)
	}
	pb, err := domain.DecodeConfig(data, false)
	var c domain.Config
	if err == nil {
		c, err = domain.ParseConfig(pb)
	}
	if err != nil {
		return s.keepConfig(inv, "rejected", err)
	}
	s.config, s.haveConfig = c, true
	return c, nil
}

// keepConfig falls back to the last good config, or fails when none has loaded.
func (s *Source) keepConfig(inv *domain.Inventory, what string, err error) (domain.Config, error) {
	if !s.haveConfig {
		return domain.Config{}, fmt.Errorf("config.pb %s: %w (fix it, or remove it to run setup again)", what, err)
	}
	inv.Errors = append(inv.Errors, "config.pb "+what+", keeping previous: "+err.Error())
	return s.config, nil
}

// ReadConfig returns config.pb as stored, or fs.ErrNotExist before the first save.
func (s *Source) ReadConfig() (*configv1.Config, error) {
	data, err := fs.ReadFile(s.fsys, configFile)
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
	if info, err := fs.Stat(s.fsys, configFile); err == nil {
		oldAt = info.ModTime()
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	tmp := "." + configFile + "." + rand.Text() + ".part"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync() // a crash must not leave an empty config.pb, which would load without auth
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Rename(tmp, configFile)
	}
	if err != nil {
		_ = root.Remove(tmp)
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
	if f, err = root.Open(tmp); err != nil {
		return 0, false, err
	}
	z, err := loadZip(f, s.key, name)
	_ = f.Close()
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
	replaced = statErr == nil
	if err = root.Rename(tmp, name+".zip"); err != nil {
		return 0, false, err
	}
	return z.bundle.Len(), replaced, nil
}

// OpenBundle opens a file of versions/ for download; the Root keeps name inside it.
func (s *Source) OpenBundle(name string) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Join(s.dir, versionsDir))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.Open(name)
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

func (s *Source) SyncState() (at time.Time, objects int, lastErr string) {
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

// listZips lists the zips in versions/, skipping hidden and partial files.
func (s *Source) listZips() ([]zipFile, error) {
	entries, err := fs.ReadDir(s.fsys, versionsDir)
	if errors.Is(err, fs.ErrNotExist) {
		_, err = fs.Stat(s.fsys, ".") // no versions/ yet is empty; no DIST at all is an error
	}
	if err != nil {
		return nil, err
	}
	var out []zipFile
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".zip")
		if !ok || strings.HasPrefix(name, ".") {
			continue
		}
		p := path.Join(versionsDir, e.Name())
		if info, err := fs.Stat(s.fsys, p); err == nil && info.Mode().IsRegular() {
			out = append(out, zipFile{name: name, path: p, fp: fingerprint(info)})
		}
	}
	return out, nil
}

// fingerprint changes with any write to the file. A write also moves ctime, which, unlike
// mtime, cannot be set back.
func fingerprint(info fs.FileInfo) uint64 {
	var b []byte
	for _, n := range [...]int64{info.Size(), info.ModTime().UnixNano(), ctime(info)} {
		b = binary.LittleEndian.AppendUint64(b, uint64(n))
	}
	h := fnv.New64a()
	_, _ = h.Write(b)
	return h.Sum64()
}
