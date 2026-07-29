package domain

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zenkiet/edge-gateway/internal/pkg/version"
)

// Step serves Bundle to backends at or above Backend.
type Step struct {
	Backend string `json:"backend"`
	Bundle  string `json:"bundle"`
}

// Mapping is sorted by Backend, oldest first.
type Mapping []Step

func NewMapping(l version.Layout, raw map[string]string) Mapping {
	m := make(Mapping, 0, len(raw))
	for backend, bundle := range raw {
		m = append(m, Step{strings.TrimSpace(backend), strings.TrimSpace(bundle)})
	}
	slices.SortFunc(m, func(a, b Step) int {
		return cmp.Or(l.Compare(a.Backend, b.Backend), strings.Compare(a.Backend, b.Backend))
	})
	return m
}

// Resolve returns the newest step whose Backend <= backend, or the oldest step
// when backend predates them all. m must not be empty.
func (m Mapping) Resolve(l version.Layout, backend string) Step {
	i, found := slices.BinarySearchFunc(m, backend, func(s Step, b string) int { return l.Compare(s.Backend, b) })
	if !found {
		i--
	}
	return m[max(i, 0)]
}

func (m Mapping) kind(l version.Layout) (version.Kind, error) {
	kind := version.Invalid
	for _, s := range m {
		k := l.Kind(s.Backend)
		switch {
		case k == version.Invalid:
			return k, fmt.Errorf("backend key %q: want a date like %s or a dotted version", s.Backend, l)
		case kind != version.Invalid && k != kind:
			return k, fmt.Errorf("backend key %q: keys mix dates and dotted versions", s.Backend)
		}
		kind = k
	}
	return kind, nil
}

func (m Mapping) validate(l version.Layout, bundles map[string]*Bundle, now time.Time, issues []string) (Mapping, []string) {
	out := make(Mapping, 0, len(m))
	y, mo, d := now.AddDate(0, 0, 90).Date()
	soon := y*10000 + int(mo)*100 + d
	for _, s := range m {
		if bundles[s.Bundle] == nil {
			issues = append(issues, fmt.Sprintf("backend %s -> bundle %s not on disk, step ignored", s.Backend, s.Bundle))
			continue
		}
		if l.Date(s.Backend) > soon {
			issues = append(issues, fmt.Sprintf("backend key %s is more than 90 days ahead, typo?", s.Backend))
		}
		out = append(out, s)
	}
	for i := 1; i < len(out); i++ {
		if version.Compare(out[i].Bundle, out[i-1].Bundle) < 0 {
			issues = append(issues, fmt.Sprintf("backend %s -> %s is older than backend %s -> %s",
				out[i].Backend, out[i].Bundle, out[i-1].Backend, out[i-1].Bundle))
		}
	}
	return out, issues
}
