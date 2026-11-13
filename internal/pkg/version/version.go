package version

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

// Semver has no leading zeros; coerced, 07.30.2026 in a YYYY.MM.dd gateway would be a version.
func init() { semver.CoerceNewVersion = false }

// Compare orders dates by day, else versions by semver; anything else sorts below versions, by text.
func (l Layout) Compare(a, b string) int {
	if da := l.Date(a); da != 0 {
		if db := l.Date(b); db != 0 {
			return cmp.Compare(da, db)
		}
	}
	va, errA := semver.NewVersion(a)
	vb, errB := semver.NewVersion(b)
	switch {
	case errA != nil && errB != nil:
		return strings.Compare(a, b)
	case errA != nil:
		return -1
	case errB != nil:
		return 1
	}
	return va.Compare(vb)
}

type Kind uint8

const (
	Invalid Kind = iota
	Date
	Dotted
)

// Kind treats a date-shaped non-date such as 02.30.2026 as Invalid, not as a version.
func (l Layout) Kind(s string) Kind {
	if d, shaped := l.date(s); shaped {
		if d == 0 {
			return Invalid
		}
		return Date
	}
	if _, err := semver.NewVersion(s); err != nil {
		return Invalid
	}
	return Dotted
}

// Layout is a date format such as MM.dd.YYYY, kept as its time.Parse layout.
type Layout struct {
	name   string
	layout string
	shape  *regexp.Regexp // what a date in the layout starts with
}

// shapes lists 2006 before 2 so it wins.
var shapes = strings.NewReplacer("2006", `\d{4}`, "01", `\d\d`, "02", `\d\d`, "1", `\d\d?`, "2", `\d\d?`, ".", `\.`)

type part struct {
	std  string // its time.Parse layout, such as "2006"
	unit int    // 0 year, 1 month, 2 day
}

// Default is the date format used when config.pb sets none.
var Default, _ = ParseLayout("MM.dd.YYYY")

func ParseLayout(s string) (Layout, error) {
	l := Layout{name: s}
	var seen [3]bool
	prev := ""
	for i := 0; i < len(s); {
		// tok is a run of one repeated character, such as YYYY or "."
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
	l.shape = regexp.MustCompile("^" + shapes.Replace(l.layout)) // a prefix, so 12.31.2026-rc.1 is a bad date
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

// date also says whether s is shaped like a date, such as 02.30.2026. 13.0.0 is not, so it stays
// a version: time.Parse alone would reject its month first.
func (l Layout) date(s string) (int, bool) {
	if l.shape == nil || !l.shape.MatchString(s) {
		return 0, false
	}
	t, err := time.Parse(l.layout, s)
	if err != nil {
		return 0, true
	}
	return t.Year()*10000 + int(t.Month())*100 + t.Day(), true
}
