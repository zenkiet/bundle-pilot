package domain

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
	"github.com/zenkiet/bundle-pilot/internal/pkg/version"
)

const (
	maxFacts   = 32
	maxFactLen = 256
)

// Config is config.pb, parsed and validated.
type Config struct {
	Project     string
	Environment string
	Default     string
	Dates       version.Layout
	Backend     Mapping
	Rules       []Rule
	Source      Source
	Auth        Auth
	Blocked     []string
	Tokens      []Token
}

func (c Config) NewestFirst(a, b string) int {
	return cmp.Or(c.Dates.Compare(b, a), strings.Compare(b, a))
}

type Revision struct {
	ID     string
	At     time.Time
	By     string
	Note   string
	Config *configv1.Config // secrets masked
}

// Source is where versions/*.zip come from; the zero value is the local directory.
type Source struct {
	Type            string
	Bucket          string
	Prefix          string
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Poll            time.Duration
}

func (s Source) String() string {
	if s.Type == "" {
		return "local"
	}
	return s.Type + "://" + s.Bucket + "/" + s.Prefix
}

// Rule sends visitors who match all its conditions to Bundle until Until. NewSnapshot sets
// Active when its bundle serves and nothing in it is broken.
type Rule struct {
	ID     string
	Bundle string
	Until  time.Time
	Active bool
	conds  []cond
	key    string // the conditions, normalized, to spot duplicate rules
}

// cond is one fact of a rule's When.
type cond struct {
	fact   string
	layout version.Layout
	pct    int
	exact  map[string]struct{}
	cmps   [][]bound
	lossy  bool // a number >= 2^53, whose last digits Struct's float64 lost
}

// bound is one comparison, such as ">=2.0.0".
type bound struct {
	op   string
	val  string
	kind version.Kind
}

// Facts are a visitor's facts, keyed by lower-case name.
type Facts map[string]string

// DecodeConfig reads config.pb, or its JSON form, which rejects unknown fields.
func DecodeConfig(data []byte, asJSON bool) (*configv1.Config, error) {
	pb := &configv1.Config{}
	if asJSON {
		return pb, protojson.Unmarshal(data, pb)
	}
	return pb, proto.Unmarshal(data, pb)
}

// EncodeConfig is deterministic, so equal configs get equal ETags.
func EncodeConfig(pb *configv1.Config) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(pb)
}

// ParseConfig validates pb and leaves it unchanged.
func ParseConfig(pb *configv1.Config) (Config, error) {
	layout, err := version.ParseLayout(cmp.Or(pb.DateFormat, version.Default.String()))
	if err != nil {
		return Config{}, fmt.Errorf("dateFormat %q: %w", pb.DateFormat, err)
	}
	src, err := parseSource(pb.Source)
	if err != nil {
		return Config{}, err
	}
	c := Config{
		Project:     strings.TrimSpace(pb.ProjectName),
		Environment: strings.TrimSpace(pb.Environment),
		Default:     strings.TrimSpace(pb.DefaultBundle),
		Dates:       layout,
		Backend:     NewMapping(layout, pb.Backend),
		Source:      src,
	}
	if c.Auth, err = parseAuth(pb.Auth); err != nil {
		return Config{}, err
	}
	if c.Tokens, err = parseTokens(pb.Tokens); err != nil {
		return Config{}, err
	}
	for _, v := range pb.Blocked {
		v = strings.TrimSpace(v)
		switch {
		case v == "" || slices.Contains(c.Blocked, v):
			return Config{}, fmt.Errorf("blocked %q: empty or listed twice", v)
		case v == c.Default:
			return Config{}, fmt.Errorf("default %s is blocked: unblock it, or set another default", v)
		}
		c.Blocked = append(c.Blocked, v)
	}
	backendKind, err := c.Backend.kind(layout)
	if err != nil {
		return Config{}, err
	}
	ids := map[string]bool{}
	for i, rf := range pb.Rules {
		r, err := parseRule(i, rf, layout)
		if err == nil && ids[r.ID] {
			err = errors.New("duplicate id")
		}
		for _, cd := range r.conds {
			other := slices.ContainsFunc(cd.cmps, func(b []bound) bool { return b[0].kind != backendKind })
			if err == nil && cd.fact == "backend" && backendKind != version.Invalid && other {
				err = errors.New("when.backend compares a different shape than the backend keys")
			}
		}
		if err != nil {
			return Config{}, fmt.Errorf("rule %s: %w", r.ID, err)
		}
		ids[r.ID] = true
		c.Rules = append(c.Rules, r)
	}
	return c, nil
}

// ParseFacts reads the posted JSON object: names are case-insensitive, numbers keep their text
// and null means absent.
func ParseFacts(data []byte) (Facts, error) {
	var raw map[string]jsontext.Value
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if len(raw) > maxFacts {
		return nil, fmt.Errorf("at most %d facts", maxFacts)
	}
	f := make(Facts, len(raw))
	for name, v := range raw {
		if !validName(name) {
			return nil, fmt.Errorf("fact name %q: use 1-64 of A-Z a-z 0-9 . _ -", name)
		}
		key := strings.ToLower(name)
		if _, dup := f[key]; dup {
			return nil, fmt.Errorf("fact %q sent twice", name)
		}
		s, err := scalar(v)
		switch {
		case err != nil:
			return nil, fmt.Errorf("fact %s: %w", name, err)
		case len(s) > maxFactLen || strings.ContainsFunc(s, func(c rune) bool { return c < 0x20 || c == 0x7f }):
			return nil, fmt.Errorf("fact %s: at most %d bytes, no control characters", name, maxFactLen)
		case s != "":
			f[key] = s
		}
	}
	return f, nil
}

func (r *Rule) lossy() bool {
	return slices.ContainsFunc(r.conds, func(c cond) bool { return c.lossy })
}

func (r *Rule) expired(now time.Time) bool {
	return !r.Until.IsZero() && !now.Before(r.Until)
}

// Skip says why Explain passed a rule over; Fact and Value are the condition that failed.
type Skip struct {
	Rule    string
	Reason  string
	Fact    string
	Value   string
	Bucket  int
	Percent int
}

func (r *Rule) skipped(f Facts, now time.Time) Skip {
	switch {
	case !r.Active:
		return Skip{Rule: r.ID, Reason: "inactive"}
	case r.expired(now):
		return Skip{Rule: r.ID, Reason: "expired"}
	}
	for _, c := range r.conds {
		v, ok := f[c.fact]
		if !ok {
			return Skip{Rule: r.ID, Reason: "no_fact", Fact: c.fact}
		}
		if !c.match(v) {
			skip := Skip{Rule: r.ID, Reason: "no_match", Fact: c.fact, Value: v}
			if c.pct > 0 {
				skip.Bucket, skip.Percent = bucket(v), c.pct
			}
			return skip
		}
	}
	// The rule matched, so its bundle cannot serve this visitor.
	return Skip{Rule: r.ID, Reason: "needs_native"}
}

// Match is how a condition of the deciding rule held; a listed value holds whatever its bucket.
type Match struct {
	Fact    string
	Value   string
	Bucket  int
	Percent int
}

func (r *Rule) held(f Facts) []Match {
	out := make([]Match, 0, len(r.conds))
	for _, c := range r.conds {
		m := Match{Fact: c.fact, Value: f[c.fact]}
		if c.pct > 0 {
			m.Bucket, m.Percent = bucket(m.Value), c.pct
		}
		out = append(out, m)
	}
	return out
}

func (r *Rule) match(f Facts) bool {
	for _, c := range r.conds {
		v, ok := f[c.fact]
		if !ok || !c.match(v) {
			return false
		}
	}
	return true
}

func (c *cond) match(v string) bool {
	if _, ok := c.exact[v]; ok {
		return true
	}
	if c.pct > 0 && bucket(v) < c.pct {
		return true
	}
	if len(c.cmps) == 0 {
		return false
	}
	kind := c.layout.Kind(v)
	for _, bounds := range c.cmps {
		if bounds[0].kind == kind && allHold(bounds, c.layout, v) {
			return true
		}
	}
	return false
}

// bucket is stable, so raising a percentage only adds users.
func bucket(v string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(v))
	return int(h.Sum32() % 100)
}

func allHold(bounds []bound, layout version.Layout, v string) bool {
	for _, b := range bounds {
		if !b.holds(layout, v) {
			return false
		}
	}
	return true
}

func (b bound) holds(layout version.Layout, v string) bool {
	order := layout.Compare(v, b.val)
	switch b.op {
	case "<":
		return order < 0
	case "<=":
		return order <= 0
	case ">":
		return order > 0
	}
	return order >= 0
}

func parseRule(i int, rf *configv1.Rule, layout version.Layout) (Rule, error) {
	r := Rule{ID: strings.TrimSpace(rf.Id), Bundle: strings.TrimSpace(rf.Bundle)}
	switch {
	case r.ID == "":
		r.ID = "#" + strconv.Itoa(i+1)
	case !validName(r.ID):
		return r, errors.New("id: use 1-64 of A-Z a-z 0-9 . _ -")
	}
	if r.Bundle == "" {
		return r, errors.New("bundle is required")
	}
	if rf.Until != "" {
		var err error
		if r.Until, err = parseUntil(rf.Until); err != nil {
			return r, fmt.Errorf("until %q: want YYYY-MM-DD or an RFC 3339 time", rf.Until)
		}
	}
	if rf.When == nil {
		return r, errors.New(`when is required: {"fact": value}`)
	}
	raw, err := protojson.Marshal(rf.When)
	if err != nil {
		return r, err
	}
	var when map[string]jsontext.Value
	if err := json.Unmarshal(raw, &when); err != nil {
		return r, err
	}
	if len(when) == 0 {
		return r, errors.New(`when is empty, which would match everyone: set "default" instead`)
	}
	names := map[string]string{}
	lower := map[string]jsontext.Value{}
	for _, name := range slices.Sorted(maps.Keys(when)) {
		v := when[name]
		key := strings.ToLower(name)
		if !validName(name) {
			return r, fmt.Errorf("fact name %q: use 1-64 of A-Z a-z 0-9 . _ -", name)
		}
		if other, dup := names[key]; dup {
			return r, fmt.Errorf("facts %q and %q differ only by case", other, name)
		}
		names[key] = name
		lower[key] = v
		c, err := parseCond(key, v, layout)
		if err != nil {
			return r, fmt.Errorf("when.%s: %w", name, err)
		}
		r.conds = append(r.conds, c)
	}
	key, _ := json.Marshal(lower, json.Deterministic(true)) // raw values that just parsed: cannot fail
	r.key = string(key)
	return r, nil
}

const masked = "***"

// Redacted is pb as clients see it: secrets masked, blocked and tokens left out, since they have
// calls of their own and must not change the ETag or history.
func Redacted(pb *configv1.Config) *configv1.Config {
	out := proto.CloneOf(pb)
	out.Blocked, out.Tokens = nil, nil
	if out.GetSource().GetSecretAccessKey() != "" {
		out.Source.SecretAccessKey = masked
	}
	if out.GetAuth().GetPasswordHash() != "" {
		out.Auth.PasswordHash = masked
	}
	return out
}

// ETag hashes Redacted(pb), so a save fails only on changes the client could have seen.
func ETag(pb *configv1.Config) string {
	b, _ := EncodeConfig(Redacted(pb))
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// KeepStored copies from cur what clients cannot send back. A missing auth keeps the stored
// account; an empty "auth": {} removes it.
func KeepStored(pb, cur *configv1.Config) {
	pb.Blocked, pb.Tokens = cur.GetBlocked(), cur.GetTokens()
	if pb.GetSource().GetSecretAccessKey() == masked {
		pb.Source.SecretAccessKey = cur.GetSource().GetSecretAccessKey()
	}
	switch {
	case pb.Auth == nil:
		pb.Auth = cur.GetAuth()
	case pb.Auth.PasswordHash == masked:
		pb.Auth.PasswordHash = cur.GetAuth().GetPasswordHash()
	}
}

// HashAuth runs just before writing: PBKDF2 is slow on purpose, so validation skips it.
func HashAuth(pb *configv1.Config) {
	if a := pb.GetAuth(); a.GetPassword() != "" {
		a.PasswordHash, a.Password = HashPassword(a.Password), ""
	}
}

func parseAuth(f *configv1.Auth) (Auth, error) {
	if f == nil {
		return Auth{}, nil
	}
	user := strings.TrimSpace(f.Username)
	if f.Password != "" && len(f.Password) < 8 {
		return Auth{}, errors.New("auth.password: at least 8 characters")
	}
	hasPassword := f.Password != "" || f.PasswordHash != ""
	if (user != "") != hasPassword {
		return Auth{}, errors.New("auth: username and password go together")
	}
	return Auth{Username: user, Hash: f.PasswordHash}, nil
}

func parseSource(f *configv1.Source) (Source, error) {
	if f == nil {
		return Source{}, nil
	}
	switch f.Type {
	case "", "local":
		if !proto.Equal(f, &configv1.Source{Type: f.Type}) {
			return Source{}, errors.New("source: a local source takes no other fields")
		}
		return Source{}, nil
	case "s3":
	default:
		return Source{}, fmt.Errorf("source.type %q: want local or s3", f.Type)
	}
	s := Source{
		Type:            "s3",
		Bucket:          strings.TrimSpace(f.Bucket),
		Prefix:          strings.Trim(f.Prefix, "/ "),
		Endpoint:        strings.TrimRight(strings.TrimSpace(f.Endpoint), "/"),
		Region:          strings.TrimSpace(f.Region),
		AccessKeyID:     strings.TrimSpace(f.AccessKeyId),
		SecretAccessKey: strings.TrimSpace(f.SecretAccessKey),
		Poll:            30 * time.Second,
	}
	if s.Bucket == "" {
		return s, errors.New("source.bucket is required")
	}
	if s.Region == "" {
		return s, errors.New("source.region is required")
	}
	if (s.AccessKeyID == "") != (s.SecretAccessKey == "") {
		return s, errors.New("source: accessKeyId and secretAccessKey go together; leave both out for the AWS credential chain")
	}
	if s.Endpoint != "" && !strings.HasPrefix(s.Endpoint, "http://") && !strings.HasPrefix(s.Endpoint, "https://") {
		return s, fmt.Errorf("source.endpoint %q: want http(s)://host, or leave it out for AWS", f.Endpoint)
	}
	if s.Prefix != "" {
		s.Prefix += "/"
	}
	if f.Poll != "" {
		d, err := time.ParseDuration(f.Poll)
		if err != nil || d < 5*time.Second {
			return s, fmt.Errorf("source.poll %q: want a duration of at least 5s, e.g. 30s", f.Poll)
		}
		s.Poll = d
	}
	return s, nil
}

// parseUntil reads a date, meaning the end of that day in UTC, or an RFC 3339 time.
func parseUntil(s string) (time.Time, error) {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.AddDate(0, 0, 1), nil
	}
	return time.Parse(time.RFC3339, s)
}

func parseCond(fact string, v jsontext.Value, layout version.Layout) (cond, error) {
	c := cond{fact: fact, layout: layout, exact: map[string]struct{}{}}
	items := []jsontext.Value{v}
	if v.Kind() == '[' {
		if err := json.Unmarshal(v, &items); err != nil {
			return c, err
		}
		if len(items) == 0 {
			return c, errors.New("empty list")
		}
	}
	for _, it := range items {
		s, err := scalar(it)
		switch {
		case err != nil:
			return c, err
		case s == "":
			return c, errors.New("empty value")
		case s == "*":
			c.pct = 100
		case strings.HasSuffix(s, "%"):
			n, err := strconv.Atoi(strings.TrimSuffix(s, "%"))
			if err != nil || n < 0 || n > 100 {
				return c, fmt.Errorf("%q: want a whole number from 0 to 100 before %%", s)
			}
			c.pct = max(c.pct, n) // a list matches any element: ["*", "10%"] is everyone
		case s[0] == '<' || s[0] == '>':
			bounds, err := parseBounds(s, layout)
			if err != nil {
				return c, err
			}
			c.cmps = append(c.cmps, bounds)
		default:
			c.exact[s] = struct{}{}
		}
		if it.Kind() == '0' {
			if n, err := strconv.ParseFloat(s, 64); err == nil && math.Abs(n) >= 1<<53 {
				c.lossy = true
			}
		}
	}
	return c, nil
}

// parseBounds reads comparisons that must all hold, such as ">=2.0.0 <3.0.0".
func parseBounds(s string, layout version.Layout) ([]bound, error) {
	var out []bound
	toks := strings.Fields(s)
	for i := 0; i < len(toks); i++ {
		op := ""
		for _, o := range [...]string{"<=", ">=", "<", ">"} {
			if strings.HasPrefix(toks[i], o) {
				op = o
				break
			}
		}
		if op == "" {
			return nil, fmt.Errorf("%q: every part must start with <, <=, > or >=", s)
		}
		val := toks[i][len(op):]
		if val == "" && i+1 < len(toks) { // ">= 2.0.0", with a space after the operator
			i++
			val = toks[i]
		}
		kind := layout.Kind(val)
		switch {
		case kind == version.Invalid:
			return nil, fmt.Errorf("%q: %q is neither a version nor a date like %s", s, val, layout)
		case len(out) > 0 && out[0].kind != kind:
			return nil, fmt.Errorf("%q mixes dates and versions", s)
		}
		out = append(out, bound{op, val, kind})
	}
	return out, nil
}

func scalar(v jsontext.Value) (string, error) {
	switch v.Kind() {
	case '"':
		var s string
		err := json.Unmarshal(v, &s)
		return strings.TrimSpace(s), err
	case '0':
		return string(v), nil
	case 't':
		return "true", nil
	case 'f':
		return "false", nil
	case 'n':
		return "", nil
	}
	return "", errors.New("want a string, number or boolean")
}

const nameChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-"

func validName(s string) bool {
	return s != "" && len(s) <= 64 && strings.Trim(s, nameChars) == ""
}
