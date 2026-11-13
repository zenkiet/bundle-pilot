package domain

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/pkg/version"
)

// Step serves Bundle to backends at or above Backend.
type Step struct {
	Backend string
	Bundle  string
}

// Mapping is the backend table, oldest backend first.
type Mapping []Step

func NewMapping(layout version.Layout, raw map[string]string) Mapping {
	m := make(Mapping, 0, len(raw))
	for backend, bundle := range raw {
		m = append(m, Step{Backend: strings.TrimSpace(backend), Bundle: strings.TrimSpace(bundle)})
	}
	slices.SortFunc(m, func(a, b Step) int {
		return cmp.Or(layout.Compare(a.Backend, b.Backend), strings.Compare(a.Backend, b.Backend))
	})
	return m
}

// at is the newest step at or below backend, else the oldest; m must not be empty.
func (m Mapping) at(layout version.Layout, backend string) int {
	i, found := slices.BinarySearchFunc(m, backend, func(s Step, b string) int { return layout.Compare(s.Backend, b) })
	if !found {
		i--
	}
	return max(i, 0)
}

// kind is the keys' shape, all dates or all versions; Invalid for an empty table.
func (m Mapping) kind(layout version.Layout) (version.Kind, error) {
	kind := version.Invalid
	for _, s := range m {
		k := layout.Kind(s.Backend)
		switch {
		case k == version.Invalid:
			return k, fmt.Errorf("backend key %q: want a date like %s or a dotted version", s.Backend, layout)
		case kind != version.Invalid && k != kind:
			return k, fmt.Errorf("backend key %q: keys mix dates and dotted versions", s.Backend)
		}
		kind = k
	}
	return kind, nil
}

func (m Mapping) validate(layout version.Layout, unserved func(string) string, now time.Time, issues []Issue) []Issue {
	y, mo, d := now.AddDate(0, 0, 90).Date()
	soon := y*10000 + int(mo)*100 + d // yyyymmdd, like layout.Date
	for i, s := range m {
		if why := unserved(s.Bundle); why != "" {
			issues = append(issues, Issue{
				Code: "backend_unserved",
				Text: fmt.Sprintf("backend %s -> bundle %s %s: its backends get the closest older step that serves",
					s.Backend, s.Bundle, why),
				Bundle: s.Bundle,
			})
		}
		if layout.Date(s.Backend) > soon {
			issues = append(issues, Issue{
				Code: "backend_future",
				Text: fmt.Sprintf("backend key %s is more than 90 days ahead, typo?", s.Backend),
			})
		}
		if i > 0 && layout.Compare(s.Bundle, m[i-1].Bundle) < 0 {
			issues = append(issues, Issue{
				Code: "backend_order",
				Text: fmt.Sprintf("backend %s -> %s is older than backend %s -> %s",
					s.Backend, s.Bundle, m[i-1].Backend, m[i-1].Bundle),
			})
		}
	}
	return issues
}
