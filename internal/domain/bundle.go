package domain

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/pkg/asset"
	"github.com/zenkiet/bundle-pilot/internal/pkg/version"
)

var ErrNoIndex = errors.New("index.html missing at the root of the zip: zip the build folder's contents, not the folder")

// Bundle is one loaded build of the app, its files served in place from its zip.
type Bundle struct {
	Version  string
	Zip      io.ReaderAt // the zip as loaded, for downloads
	ZipBytes int64
	ModTime  time.Time
	Link     []string // preloads for the page's main scripts and styles, `url>; rel=...` with url as index.html has it
	Native   string   // the app versions it runs on, from <meta name="bundle-pilot:native">
	base     string
	tag      []string
	files    map[string]*asset.Asset
	index    *asset.Asset
	native   []bound
}

// NewBundle builds a bundle. index is the page served for index.html, stamped with the version;
// files keeps the raw page for OTA downloads. native limits the native app versions the bundle
// runs on, such as ">=4.102.0"; empty means any.
func NewBundle(ver, baseHref, native string, files map[string]*asset.Asset, index *asset.Asset) (*Bundle, error) {
	b := &Bundle{
		Version: ver,
		Native:  strings.TrimSpace(native),
		base:    normalizeBase(baseHref),
		tag:     []string{ver},
		files:   files,
		index:   index,
	}
	if b.Native != "" {
		bounds, err := parseBounds(b.Native, version.Default)
		if err != nil || bounds[0].kind != version.Dotted {
			return nil, fmt.Errorf(`<meta name="bundle-pilot:native" content=%q>: `+
				`want comparisons with app versions, such as ">=4.102.0"`, b.Native)
		}
		b.native = bounds
	}
	return b, nil
}

// runs reports whether b works on the given native app version. A bundle with no requirement
// runs anywhere; otherwise native must be a version that meets it.
func (b *Bundle) runs(native string) bool {
	if len(b.native) == 0 {
		return true
	}
	return version.Default.Kind(native) == version.Dotted && allHold(b.native, version.Default, native)
}

func (b *Bundle) Tag() []string { return b.tag }

func (b *Bundle) Index() *asset.Asset { return b.index }

func (b *Bundle) Len() int { return len(b.files) }

// File is rel exactly as zipped, without the stamped index or other bundles' files.
func (b *Bundle) File(rel string) *asset.Asset { return b.files[rel] }

// Inventory is what a Source found on disk. Errors are refusals, Issues are warnings, and Retry
// means a failure may clear by itself, so the watcher tries again.
type Inventory struct {
	Bundles []*Bundle
	Config  Config
	Errors  []string
	Issues  []string
	Retry   bool
	Signed  bool
	Setup   string // the first-run token while config.pb is missing
}

// owned is a file and the bundle it came from.
type owned struct {
	asset  *asset.Asset
	bundle *Bundle
}

// Snapshot is an immutable view of every loaded bundle; it is swapped atomically on reload.
type Snapshot struct {
	BasePath    string
	Default     *Bundle
	DefaultFrom string // why Default was chosen: "config", "previous", "newest" or "none"
	Config      Config
	Errors      []string
	Issues      []string
	LoadedAt    time.Time
	Signed      bool
	Setup       string
	bundles     map[string]*Bundle // the bundles that serve, by version
	order       []*Bundle          // every loaded bundle, blocked ones included, newest first
	fallback    map[string]owned   // every bundle's files but pages and ngsw.json, newest owner
}

// NewSnapshot applies the config to the bundles. prevDefault is the version served so far; it
// stays the default when the config names one that is not on disk. The default's <base href>
// becomes the mount path. A blocked bundle stays listed and its files still load for pages
// already open, but nothing routes to it.
func NewSnapshot(inv Inventory, now time.Time, prevDefault string) *Snapshot {
	s := &Snapshot{
		Config:   inv.Config,
		Errors:   slices.Clone(inv.Errors),
		Issues:   slices.Clone(inv.Issues),
		LoadedAt: now,
		Signed:   inv.Signed,
		Setup:    inv.Setup,
		bundles:  make(map[string]*Bundle, len(inv.Bundles)),
		order:    slices.Clone(inv.Bundles),
		fallback: map[string]owned{},
	}
	s.Config.Rules = slices.Clone(inv.Config.Rules)
	slices.SortFunc(s.order, func(a, b *Bundle) int {
		return cmp.Or(inv.Config.Dates.Compare(b.Version, a.Version), strings.Compare(b.Version, a.Version))
	})
	for _, b := range s.order {
		if !slices.Contains(s.Config.Blocked, b.Version) {
			s.bundles[b.Version] = b
		}
	}
	// Oldest first, so a file name in several bundles belongs to the newest. Pages stay with
	// their bundle so a removed one never comes back, and so does ngsw.json, whose 404 retires
	// Angular's service worker.
	for _, b := range slices.Backward(s.order) {
		for rel, a := range b.files {
			if path.Ext(rel) != ".html" && rel != "/ngsw.json" {
				s.fallback[rel] = owned{a, b}
			}
		}
	}
	s.pickDefault(inv.Config.Default, prevDefault)
	s.Issues = s.Config.Backend.validate(s.Config.Dates, s.unserved, now, s.Issues)
	if s.Config.Auth.Username == "" && s.Setup == "" {
		s.Issues = append(s.Issues, "no admin password: anyone who reaches /__gateway/ can change the config")
	}
	keys := map[string]string{}
	for i := range s.Config.Rules {
		r := &s.Config.Rules[i]
		why := s.unserved(r.Bundle)
		switch {
		case why != "":
			s.Issues = append(s.Issues, fmt.Sprintf("rule %s: bundle %s %s, inactive", r.ID, r.Bundle, why))
		case r.lossy():
			s.Issues = append(s.Issues, fmt.Sprintf(
				"rule %s: a number in when is 2^53 or more and lost its last digits: quote it as a string, inactive", r.ID))
		case r.expired(now):
			s.Issues = append(s.Issues, fmt.Sprintf("rule %s: until has passed, delete it", r.ID))
		default:
			r.Active = true
		}
		if !r.Active {
			continue // an inactive rule shadows nothing
		}
		if first, dup := keys[r.key]; dup {
			s.Issues = append(s.Issues, fmt.Sprintf("rule %s: same conditions as rule %s, never matches", r.ID, first))
		}
		keys[r.key] = r.ID
	}
	s.BasePath = s.Default.base
	return s
}

// unserved says why version v is not served, or "" when it is.
func (s *Snapshot) unserved(v string) string {
	switch {
	case s.bundles[v] != nil:
		return ""
	case slices.Contains(s.Config.Blocked, v):
		return "is blocked"
	}
	return "is not on disk"
}

// pickDefault sets the default: the config's when it serves, else the previous default, else
// the newest bundle that serves.
func (s *Snapshot) pickDefault(want, prev string) {
	if len(s.order) == 0 {
		s.Default, s.DefaultFrom = &Bundle{base: "/"}, "none"
		s.Errors = append(s.Errors, "no bundles in versions/ yet: upload one")
		return
	}
	served := slices.IndexFunc(s.order, func(b *Bundle) bool { return s.bundles[b.Version] != nil })
	newest := s.order[max(served, 0)]
	if served < 0 {
		// Every bundle is blocked. Serving a blocked one beats serving nothing.
		s.bundles[newest.Version] = newest
		s.Errors = append(s.Errors, "every bundle is blocked: serving "+newest.Version)
	}
	s.Default, s.DefaultFrom = newest, "newest"
	switch {
	case s.bundles[want] != nil:
		s.Default, s.DefaultFrom = s.bundles[want], "config"
	case want == "":
		s.Issues = append(s.Issues, "config.pb sets no default: the newest bundle serves every visitor without a cookie")
	default:
		if b := s.bundles[prev]; b != nil {
			s.Default, s.DefaultFrom = b, "previous"
		}
		s.Issues = append(s.Issues, fmt.Sprintf("config default %s not on disk, using %s", want, s.Default.Version))
	}
}

func (s *Snapshot) Bundles() []*Bundle { return s.order }

// Bundle is the served bundle of that version: nil when it is not loaded, or blocked.
func (s *Snapshot) Bundle(version string) *Bundle { return s.bundles[version] }

// Loaded reports whether a bundle of that version is loaded, served or blocked.
func (s *Snapshot) Loaded(version string) bool {
	return slices.ContainsFunc(s.order, func(b *Bundle) bool { return b.Version == version })
}

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

// Lookup finds rel in b, falling back to the newest bundle that has it (pages and ngsw.json
// excepted), so sessions opened before a switch keep loading their chunks.
func (s *Snapshot) Lookup(b *Bundle, rel string) (*asset.Asset, *Bundle) {
	if rel == "/index.html" {
		return b.index, b
	}
	if a := b.files[rel]; a != nil {
		return a, b
	}
	if o, ok := s.fallback[rel]; ok {
		return o.asset, o.bundle
	}
	return nil, nil
}

// Decide picks the bundle for a visitor's facts, in this order:
//  1. the first active, unexpired rule that matches;
//  2. the backend table, when the backend fact is posted;
//  3. the default.
//
// via says which one decided. A bundle that is not served, or that needs a newer native app
// than the native fact, is passed over: the backend table falls back to the closest older step,
// and the default to the closest older bundle (via "native").
func (s *Snapshot) Decide(f Facts, now time.Time) (*Bundle, string, error) {
	return s.decide(f, now, nil)
}

// Explain is Decide that also says, for the admin's tester, why each earlier rule was passed over.
func (s *Snapshot) Explain(f Facts, now time.Time) (b *Bundle, via string, why []string, err error) {
	b, via, err = s.decide(f, now, &why)
	return b, via, why, err
}

func (s *Snapshot) decide(f Facts, now time.Time, why *[]string) (*Bundle, string, error) {
	native, hasNative := f["native"]
	// serving returns the bundle of version v if it serves and runs on the posted native version.
	serving := func(v string) *Bundle {
		if b := s.bundles[v]; b != nil && (!hasNative || b.runs(native)) {
			return b
		}
		return nil
	}

	for i := range s.Config.Rules {
		r := &s.Config.Rules[i]
		b := serving(r.Bundle)
		if b != nil && r.Active && !r.expired(now) && r.match(f) {
			return b, "rule:" + r.ID, nil
		}
		if why != nil {
			*why = append(*why, "rule "+r.ID+": "+r.skipped(f, now, b != nil))
		}
	}

	if backend, has := f["backend"]; has && len(s.Config.Backend) > 0 {
		layout, table := s.Config.Dates, s.Config.Backend
		if want := layout.Kind(table[0].Backend); layout.Kind(backend) != want {
			shape := "a dotted version"
			if want == version.Date {
				shape = "a date like " + layout.String()
			}
			return nil, "", errors.New("backend must have the same shape as the backend table keys: want " + shape)
		}
		for i := table.at(layout, backend); i >= 0; i-- {
			if b := serving(table[i].Bundle); b != nil {
				return b, "backend:" + table[i].Backend, nil
			}
		}
		return nil, "", fmt.Errorf("no served bundle in the backend table for backend %s and older", backend)
	}

	if !hasNative || s.Default.runs(native) {
		return s.Default, "default", nil
	}
	older := s.order[slices.Index(s.order, s.Default)+1:] // s.order is newest first
	for _, b := range older {
		if serving(b.Version) != nil {
			return b, "native", nil
		}
	}
	return nil, "", fmt.Errorf("no bundle runs on native %s", native)
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
