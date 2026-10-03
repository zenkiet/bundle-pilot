package version

import (
	"cmp"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// Compare orders two dates in l by day, else by semver (4.100.0 > 4.90.0,
// 1.0.0-rc.1 < 1.0.0); anything else sorts below versions, by text.
func (l Layout) Compare(a, b string) int {
	if da := l.Date(a); da != 0 {
		if db := l.Date(b); db != 0 {
			return cmp.Compare(da, db)
		}
	}
	ca, cb := canon(a), canon(b)
	if c := semver.Compare(ca, cb); c != 0 || semver.IsValid(ca) {
		return c
	}
	return strings.Compare(a, b)
}

type Kind uint8

const (
	Invalid Kind = iota
	Date
	Dotted
)

// Kind classifies s as a real date in l or a semantic version (optional v, -pre,
// +build); a date-shaped non-date such as 02.30.2026 is Invalid.
func (l Layout) Kind(s string) Kind {
	if d, shaped := l.date(s); shaped {
		if d == 0 {
			return Invalid
		}
		return Date
	}
	if !semver.IsValid(canon(s)) {
		return Invalid
	}
	return Dotted
}

// canon adds the v prefix semver wants; bundles are named without it.
func canon(s string) string {
	if strings.HasPrefix(s, "v") {
		return s
	}
	return "v" + s
}

// Layout is a date format written with YYYY, MM or M, dd or d (DD and yyyy
// also work), separated by . - or /, kept as the time.Parse layout it maps to.
type Layout struct {
	name   string
	layout string
}

type part struct {
	std  string
	unit int
}

var Default, _ = ParseLayout("MM.dd.YYYY")

func ParseLayout(s string) (Layout, error) {
	l := Layout{name: s}
	var seen [3]bool
	prev := ""
	for i := 0; i < len(s); {
		n := 1
		for i+n < len(s) && s[i+n] == s[i] {
			n++
		}
		tok := s[i : i+n]
		i += n
		p, ok := tokens[tok]
		switch {
		case !ok && strings.Trim(tok, ".-/") == "":
			l.layout += tok
			prev = ""
			continue
		case tok == "mm":
			return l, errors.New("mm means minutes, write MM for the month")
		case !ok:
			return l, fmt.Errorf("unknown %q: use YYYY, MM or M, dd or d, separated by . - or /", tok)
		case seen[p.unit]:
			return l, fmt.Errorf("%q repeats a part", tok)
		case prev != "" && (len(prev) == 1 || len(p.std) == 1):
			return l, errors.New("the one-or-two-digit M and d need a separator next to them")
		}
		seen[p.unit] = true
		l.layout += p.std
		prev = p.std
	}
	if seen != [3]bool{true, true, true} {
		return l, errors.New("needs a year (YYYY), a month (MM or M) and a day (dd or d)")
	}
	return l, nil
}

var tokens = map[string]part{
	"YYYY": {"2006", 0}, "yyyy": {"2006", 0},
	"MM": {"01", 1}, "M": {"1", 1},
	"dd": {"02", 2}, "DD": {"02", 2}, "d": {"2", 2}, "D": {"2", 2},
}

func (l Layout) String() string { return l.name }

// Date returns s as yyyymmdd, or 0 when s is not a valid date in l.
func (l Layout) Date(s string) int {
	d, _ := l.date(s)
	return d
}

// date also reports whether s has the layout's shape even if the day does not
// exist: time.Parse then fails with a range message instead of on an element.
func (l Layout) date(s string) (int, bool) {
	if l.layout == "" {
		return 0, false
	}
	t, err := time.Parse(l.layout, s)
	if err == nil {
		return t.Year()*10000 + int(t.Month())*100 + t.Day(), true
	}
	var pe *time.ParseError
	return 0, errors.As(err, &pe) && pe.Message != ""
}
