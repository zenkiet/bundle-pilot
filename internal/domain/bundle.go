package domain

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/pkg/asset"
	"github.com/zenkiet/bundle-pilot/internal/pkg/version"
)

var ErrNoIndex = errors.New("index.html missing")

type Bundle struct {
	Version  string
	ZipBytes int64
	ModTime  time.Time
	Link     string
	base     string
	tag      []string
	files    map[string]*asset.Asset
	index    *asset.Asset
}

// NewBundle serves index, the stamped page, for index.html; files keeps the raw one.
func NewBundle(ver, baseHref string, files map[string]*asset.Asset, index *asset.Asset) (*Bundle, error) {
	if files["/index.html"] == nil {
		return nil, ErrNoIndex
	}
	return &Bundle{Version: ver, base: normalizeBase(baseHref), tag: []string{ver}, files: files, index: index}, nil
}

func (b *Bundle) Bytes() int64 {
	var n int64
	for _, a := range b.files {
		n += a.Size()
	}
	return n
}

func (b *Bundle) Tag() []string { return b.tag }

func (b *Bundle) Index() *asset.Asset { return b.index }

func (b *Bundle) Len() int { return len(b.files) }

// File is rel exactly as zipped, without the stamped index or other bundles' files.
func (b *Bundle) File(rel string) *asset.Asset { return b.files[rel] }

// Inventory is what a source found on disk: Errors are refusals, Issues are
// warnings, Retry marks failures that may clear with no change on disk.
type Inventory struct {
	Bundles []*Bundle
	Config  Config
	Errors  []string
	Issues  []string
	Retry   bool
	Signed  bool
	Setup   bool
}

type owned struct {
	asset  *asset.Asset
	bundle *Bundle
}

// Snapshot is an immutable view of every loaded bundle; it is swapped atomically on reload.
type Snapshot struct {
	BasePath    string
	Default     *Bundle
	DefaultFrom string
	Config      Config
	Errors      []string
	Issues      []string
	LoadedAt    time.Time
	Signed      bool
	Setup       bool
	bundles     map[string]*Bundle
	order       []*Bundle
	hashed      map[string]owned
}

// NewSnapshot applies the config to the bundles. keep is the default being served, kept
// when the config names one not on disk; the default's <base href> is the mount path.
func NewSnapshot(inv Inventory, now time.Time, keep string) *Snapshot {
	s := &Snapshot{
		Errors:   slices.Clone(inv.Errors),
		Issues:   slices.Clone(inv.Issues),
		LoadedAt: now,
		Signed:   inv.Signed,
		Setup:    inv.Setup,
		bundles:  make(map[string]*Bundle, len(inv.Bundles)),
		order:    slices.Clone(inv.Bundles),
		hashed:   map[string]owned{},
	}
	slices.SortFunc(s.order, func(a, b *Bundle) int {
		return cmp.Or(inv.Config.Dates.Compare(b.Version, a.Version), strings.Compare(b.Version, a.Version))
	})
	for _, b := range s.order {
		s.bundles[b.Version] = b
	}
	for _, b := range slices.Backward(s.order) {
		for rel, a := range b.files {
			if a.Immutable() {
				s.hashed[rel] = owned{a, b}
			}
		}
	}
	s.pickDefault(inv.Config.Default, keep)
	s.Config = inv.Config
	s.Config.Rules = slices.Clone(inv.Config.Rules)
	s.Config.Backend, s.Issues = inv.Config.Backend.validate(s.Config.Dates, s.bundles, now, s.Issues)
	keys := map[string]string{}
	for i := range s.Config.Rules {
		r := &s.Config.Rules[i]
		switch {
		case s.bundles[r.Bundle] == nil:
			s.Issues = append(s.Issues, fmt.Sprintf("rule %s: bundle %s not on disk, inactive", r.ID, r.Bundle))
		case !r.Until.IsZero() && !now.Before(r.Until):
			s.Issues = append(s.Issues, fmt.Sprintf("rule %s: until has passed, delete it", r.ID))
		default:
			r.Active = true
		}
		if first, dup := keys[r.key]; dup && r.key != "" {
			s.Issues = append(s.Issues, fmt.Sprintf("rule %s: same conditions as rule %s, never matches", r.ID, first))
		}
		keys[r.key] = r.ID
	}
	s.BasePath = s.Default.base
	return s
}

func (s *Snapshot) pickDefault(config, keep string) {
	if len(s.order) == 0 {
		s.Default, s.DefaultFrom = &Bundle{base: "/"}, "none"
		s.Errors = append(s.Errors, "no bundles in versions/ yet: upload one")
		return
	}
	s.Default, s.DefaultFrom = s.order[0], "newest"
	switch {
	case s.bundles[config] != nil:
		s.Default, s.DefaultFrom = s.bundles[config], "config"
	case config == "":
		s.Issues = append(s.Issues, "config.pb sets no default: the newest bundle serves every visitor without a cookie")
	default:
		if b := s.bundles[keep]; b != nil {
			s.Default, s.DefaultFrom = b, "previous"
		}
		s.Issues = append(s.Issues, fmt.Sprintf("config default %s not on disk, using %s", config, s.Default.Version))
	}
}

func (s *Snapshot) Bundles() []*Bundle { return s.order }

func (s *Snapshot) Bundle(version string) *Bundle { return s.bundles[version] }

// Pick returns the bundle to serve (query wins over cookie) and the one the cookie pins.
func (s *Snapshot) Pick(query, cookie string) (picked, current *Bundle) {
	current = s.Default
	if b := s.bundles[cookie]; b != nil {
		current = b
	}
	if b := s.bundles[query]; b != nil {
		return b, current
	}
	return current, current
}

// Lookup finds rel in b, falling back to content-hashed files of any bundle so
// sessions opened before a switch keep loading their chunks.
func (s *Snapshot) Lookup(b *Bundle, rel string) (*asset.Asset, *Bundle) {
	if rel == "/index.html" {
		return b.index, b
	}
	if a := b.files[rel]; a != nil {
		return a, b
	}
	if o, ok := s.hashed[rel]; ok {
		return o.asset, o.bundle
	}
	return nil, nil
}

// Decide picks the bundle for posted facts: the first active, unexpired rule
// that matches, else the backend table, else the default. via names the source.
func (s *Snapshot) Decide(f Facts, now time.Time) (*Bundle, string, error) {
	for i := range s.Config.Rules {
		if r := &s.Config.Rules[i]; r.Active && (r.Until.IsZero() || now.Before(r.Until)) && r.match(f) {
			return s.bundles[r.Bundle], "rule:" + r.ID, nil
		}
	}
	if backend, ok := f["backend"]; ok && len(s.Config.Backend) > 0 {
		l := s.Config.Dates
		if want := l.Kind(s.Config.Backend[0].Backend); l.Kind(backend) != want {
			shape := "a dotted version"
			if want == version.Date {
				shape = "a date like " + l.String()
			}
			return nil, "", errors.New("backend must have the same shape as the backend table keys: want " + shape)
		}
		st := s.Config.Backend.Resolve(l, backend)
		return s.bundles[st.Bundle], "backend:" + st.Backend, nil
	}
	return s.Default, "default", nil
}

func normalizeBase(s string) string {
	if u, err := url.Parse(s); err == nil {
		s = u.Path
	}
	if s = path.Clean("/" + s); s != "/" {
		s += "/"
	}
	return s
}
