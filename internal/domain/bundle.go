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

var ErrNoIndex error = &RefusedError{
	Reason: "no_index",
	Err:    errors.New("index.html missing at the root of the zip: zip the build folder's contents, not the folder"),
}

// RefusedError is why a zip is refused; Reason is a stable code apps can suggest a fix by.
type RefusedError struct {
	Reason string
	Err    error
}

func (e *RefusedError) Error() string { return e.Err.Error() }

func (e *RefusedError) Unwrap() error { return e.Err }

func RefusalReason(err error) string {
	if e, ok := errors.AsType[*RefusedError](err); ok {
		return e.Reason
	}
	return ""
}

// Bundle is one build of the app, served in place from its zip.
type Bundle struct {
	Version  string
	Zip      io.ReaderAt
	ZipBytes int64
	ModTime  time.Time
	Link     []string // Link header values without the leading "<", url as index.html has it
	Native   string   // the native app versions it runs on, such as ">=4.102.0"
	SHA256   string   // of the zip, hex
	base     string
	tag      []string
	files    map[string]*asset.Asset
	index    *asset.Asset
	native   []bound
}

// NewBundle builds a bundle. index is the stamped page; files keeps the raw one for OTA downloads.
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
			return nil, &RefusedError{Reason: "bad_native", Err: fmt.Errorf(`<meta name="bundle-pilot:native" content=%q>: `+
				`want comparisons with app versions, such as ">=4.102.0"`, b.Native)}
		}
		b.native = bounds
	}
	return b, nil
}

func (b *Bundle) runs(native string) bool {
	if len(b.native) == 0 {
		return true
	}
	return version.Default.Kind(native) == version.Dotted && allHold(b.native, version.Default, native)
}

func (b *Bundle) Tag() []string { return b.tag }

func (b *Bundle) Index() *asset.Asset { return b.index }

func (b *Bundle) Len() int { return len(b.files) }

// File is rel as zipped: the raw index.html, and no fallback to other bundles.
func (b *Bundle) File(rel string) *asset.Asset { return b.files[rel] }

// Issue is a load error or warning. Code and Reason are stable for apps; Text is for people.
type Issue struct {
	Code   string
	Text   string
	Rule   string
	Bundle string
	Reason string
}

// Inventory is what a Source loaded. Retry means a failure may clear by itself.
type Inventory struct {
	Bundles []*Bundle
	Config  Config
	Errors  []Issue
	Issues  []Issue
	Retry   bool
	Signed  bool
	Setup   string // the first-run token while config.pb is missing
}

type owned struct {
	asset  *asset.Asset
	bundle *Bundle
}

// Snapshot is immutable: a reload swaps in a new one.
type Snapshot struct {
	BasePath    string
	Default     *Bundle
	DefaultFrom string // "config", "previous", "newest" or "none"
	Config      Config
	Errors      []Issue
	Issues      []Issue
	LoadedAt    time.Time
	Signed      bool
	Setup       string
	bundles     map[string]*Bundle // served ones only
	order       []*Bundle          // all loaded, newest first
	fallback    map[string]owned   // files but pages and ngsw.json, by newest owner
}

// NewSnapshot applies the config to the bundles. A blocked bundle's files still load for pages
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
	// Oldest first, so the newest owner wins. Pages stay with their bundle so a removed page never
	// comes back, and ngsw.json too: its 404 retires Angular's service worker.
	for _, b := range slices.Backward(s.order) {
		for rel, a := range b.files {
			if path.Ext(rel) != ".html" && rel != "/ngsw.json" {
				s.fallback[rel] = owned{a, b}
			}
		}
	}
	s.pickDefault(inv.Config.Default, prevDefault)
	// One mount path serves every bundle: one built for another <base href> would 404 on its files.
	for _, b := range s.order {
		if s.bundles[b.Version] != nil && b.base != s.Default.base {
			delete(s.bundles, b.Version)
			s.Issues = append(s.Issues, Issue{
				Code: "base_mismatch",
				Text: fmt.Sprintf("bundle %s has <base href> %s, the gateway serves %s (the default's): not served",
					b.Version, b.base, s.Default.base),
				Bundle: b.Version,
			})
		}
	}
	s.Issues = s.Config.Backend.validate(s.Config.Dates, s.unserved, now, s.Issues)
	if s.Config.Auth.Username == "" && s.Setup == "" {
		s.Issues = append(s.Issues, Issue{
			Code: "no_password",
			Text: "no admin password: anyone who reaches /__gateway/ can change the config",
		})
	}
	keys := map[string]string{}
	for i := range s.Config.Rules {
		r := &s.Config.Rules[i]
		why := s.unserved(r.Bundle)
		switch {
		case why != "":
			s.Issues = append(s.Issues, Issue{
				Code:   "rule_inactive",
				Text:   fmt.Sprintf("rule %s: bundle %s %s, inactive", r.ID, r.Bundle, why),
				Rule:   r.ID,
				Bundle: r.Bundle,
			})
		case r.lossy():
			s.Issues = append(s.Issues, Issue{
				Code: "rule_lossy",
				Text: fmt.Sprintf("rule %s: a number in when is 2^53 or more and lost its last digits: "+
					"quote it as a string, inactive", r.ID),
				Rule: r.ID,
			})
		case r.expired(now):
			s.Issues = append(s.Issues, Issue{
				Code: "rule_expired",
				Text: fmt.Sprintf("rule %s: until has passed, delete it", r.ID),
				Rule: r.ID,
			})
		default:
			r.Active = true
		}
		if !r.Active {
			continue // an inactive rule shadows nothing
		}
		if first, dup := keys[r.key]; dup {
			s.Issues = append(s.Issues, Issue{
				Code: "rule_shadowed",
				Text: fmt.Sprintf("rule %s: same conditions as rule %s, never matches", r.ID, first),
				Rule: r.ID,
			})
		}
		keys[r.key] = r.ID
	}
	s.BasePath = s.Default.base
	return s
}

func (s *Snapshot) unserved(v string) string {
	switch {
	case s.bundles[v] != nil:
		return ""
	case slices.Contains(s.Config.Blocked, v):
		return "is blocked"
	case s.Loaded(v):
		return "has another <base href> than the default"
	}
	return "is not on disk"
}

// pickDefault keeps the previous default while the config's is not served, so a typo or a
// pending upload switches nobody.
func (s *Snapshot) pickDefault(want, prev string) {
	if len(s.order) == 0 {
		s.Default, s.DefaultFrom = &Bundle{base: "/"}, "none"
		s.Errors = append(s.Errors, Issue{Code: "no_bundles", Text: "no bundles in versions/ yet: upload one"})
		return
	}
	served := slices.IndexFunc(s.order, func(b *Bundle) bool { return s.bundles[b.Version] != nil })
	newest := s.order[max(served, 0)]
	if served < 0 {
		// Serving a blocked bundle beats serving nothing.
		s.bundles[newest.Version] = newest
		s.Errors = append(s.Errors, Issue{
			Code:   "all_blocked",
			Text:   "every bundle is blocked: serving " + newest.Version,
			Bundle: newest.Version,
		})
	}
	s.Default, s.DefaultFrom = newest, "newest"
	switch {
	case s.bundles[want] != nil:
		s.Default, s.DefaultFrom = s.bundles[want], "config"
	case want == "":
		s.Issues = append(s.Issues, Issue{
			Code: "no_default",
			Text: "config.pb sets no default: the newest bundle serves every visitor without a cookie",
		})
	default:
		if b := s.bundles[prev]; b != nil {
			s.Default, s.DefaultFrom = b, "previous"
		}
		s.Issues = append(s.Issues, Issue{
			Code:   "default_missing",
			Text:   fmt.Sprintf("config default %s not on disk, using %s", want, s.Default.Version),
			Bundle: want,
		})
	}
}

func (s *Snapshot) Bundles() []*Bundle { return s.order }

// Bundle is nil unless that version is served; Loaded counts blocked ones too.
func (s *Snapshot) Bundle(version string) *Bundle { return s.bundles[version] }

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

// Lookup falls back to the newest bundle that has rel, so pages opened before a switch keep
// loading their chunks.
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

// Decide tries the rules, then the backend table, then the default. A bundle that is not served,
// or needs a newer app than the native fact, is skipped: the table and the default fall back to
// an older one.
func (s *Snapshot) Decide(f Facts, now time.Time) (*Bundle, string, error) {
	return s.decide(f, now, nil)
}

// Explain is Decide plus why each earlier rule was skipped and how the deciding one matched.
func (s *Snapshot) Explain(f Facts, now time.Time) (b *Bundle, via string, why []Skip, match []Match, err error) {
	b, via, err = s.decide(f, now, &why)
	if id, ok := strings.CutPrefix(via, "rule:"); ok {
		i := slices.IndexFunc(s.Config.Rules, func(r Rule) bool { return r.ID == id })
		match = s.Config.Rules[i].held(f)
	}
	return b, via, why, match, err
}

func (s *Snapshot) decide(f Facts, now time.Time, why *[]Skip) (*Bundle, string, error) {
	native, hasNative := f["native"]
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
			*why = append(*why, r.skipped(f, now))
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
