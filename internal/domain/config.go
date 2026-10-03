package domain

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"hash/fnv"
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
}

// Source is where versions/*.zip come from; the zero value is the local
// directory, "s3" is any S3-compatible bucket mirrored into it.
type Source struct {
	Type            string
	Bucket          string
	Prefix          string
	Endpoint        string
	Region          string
	AccessKeyID     string `json:"-"`
	SecretAccessKey string `json:"-"`
	Poll            time.Duration
}

func (s Source) String() string {
	if s.Type == "" {
		return "local"
	}
	return s.Type + "://" + s.Bucket + "/" + s.Prefix
}

type Rule struct {
	ID     string         `json:"id"`
	Note   string         `json:"note,omitempty"`
	When   jsontext.Value `json:"when"`
	Bundle string         `json:"bundle"`
	Until  time.Time      `json:"until,omitzero"`
	Active bool           `json:"active"`
	conds  []cond
	key    string
}

type cond struct {
	fact  string
	l     version.Layout
	pct   int
	exact map[string]struct{}
	cmps  [][]bound
}

type bound struct {
	op   string
	val  string
	kind version.Kind
}

type Facts map[string]string

// DecodeConfig reads config.pb, or its JSON form, which rejects unknown fields.
func DecodeConfig(data []byte, asJSON bool) (*configv1.Config, error) {
	pb := &configv1.Config{}
	if asJSON {
		return pb, protojson.Unmarshal(data, pb)
	}
	return pb, proto.Unmarshal(data, pb)
}

func EncodeConfig(pb *configv1.Config) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(pb)
}

func ParseConfig(pb *configv1.Config) (Config, error) {
	l := version.Default
	if pb.DateFormat != "" {
		var err error
		if l, err = version.ParseLayout(pb.DateFormat); err != nil {
			return Config{}, fmt.Errorf("dateFormat %q: %w", pb.DateFormat, err)
		}
	}
	src, err := parseSource(pb.Source)
	if err != nil {
		return Config{}, err
	}
	c := Config{Project: strings.TrimSpace(pb.ProjectName), Environment: strings.TrimSpace(pb.Environment), Default: strings.TrimSpace(pb.DefaultBundle), Dates: l, Backend: NewMapping(l, pb.Backend), Source: src}
	if c.Auth, err = parseAuth(pb.Auth); err != nil {
		return Config{}, err
	}
	kind, err := c.Backend.kind(l)
	if err != nil {
		return Config{}, err
	}
	ids := map[string]bool{}
	for i, rf := range pb.Rules {
		r, err := parseRule(i, rf, l)
		if err == nil && ids[r.ID] {
			err = errors.New("duplicate id")
		}
		for _, cd := range r.conds {
			if cd.fact == "backend" && kind != version.Invalid && len(cd.cmps) > 0 && cd.cmps[0][0].kind != kind {
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

// ParseFacts reads the flat JSON object the frontend posts; names are
// case-insensitive, numbers keep their literal text and null means absent.
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
	kind := c.l.Kind(v)
	for _, all := range c.cmps {
		if all[0].kind == kind && !slices.ContainsFunc(all, func(b bound) bool { return !b.holds(c.l, v) }) {
			return true
		}
	}
	return false
}

// bucket places v in a stable 0-99 slot, so raising a percentage only adds users.
func bucket(v string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(v))
	return int(h.Sum32() % 100)
}

func (b bound) holds(l version.Layout, v string) bool {
	c := l.Compare(v, b.val)
	switch b.op {
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	}
	return c >= 0
}

func parseRule(i int, rf *configv1.Rule, l version.Layout) (Rule, error) {
	r := Rule{ID: strings.TrimSpace(rf.Id), Note: strings.TrimSpace(rf.Note), Bundle: strings.TrimSpace(rf.Bundle)}
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
	for name, v := range when {
		key := strings.ToLower(name)
		if !validName(name) {
			return r, fmt.Errorf("fact name %q: use 1-64 of A-Z a-z 0-9 . _ -", name)
		}
		if other, dup := names[key]; dup {
			return r, fmt.Errorf("facts %q and %q differ only by case", other, name)
		}
		names[key] = name
		c, err := parseCond(key, v, l)
		if err != nil {
			return r, fmt.Errorf("when.%s: %w", name, err)
		}
		r.conds = append(r.conds, c)
	}
	r.When = jsontext.Value(raw)
	if err := r.When.Canonicalize(); err == nil {
		r.key = strings.ToLower(string(r.When))
	}
	return r, nil
}

// parseAuth hashes a plaintext password into pb, so what gets stored never holds it.
func parseAuth(f *configv1.Auth) (Auth, error) {
	if f == nil {
		return Auth{}, nil
	}
	f.Username = strings.TrimSpace(f.Username)
	if f.Password != "" {
		if len(f.Password) < 8 {
			return Auth{}, errors.New("auth.password: at least 8 characters")
		}
		f.PasswordHash, f.Password = HashPassword(f.Password), ""
	}
	if (f.Username == "") != (f.PasswordHash == "") {
		return Auth{}, errors.New("auth: username and password go together")
	}
	return Auth{Username: f.Username, Hash: f.PasswordHash}, nil
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
	s := Source{Type: "s3", Bucket: strings.TrimSpace(f.Bucket), Prefix: strings.Trim(f.Prefix, "/ "), Endpoint: strings.TrimRight(strings.TrimSpace(f.Endpoint), "/"),
		Region: strings.TrimSpace(f.Region), AccessKeyID: strings.TrimSpace(f.AccessKeyId), SecretAccessKey: strings.TrimSpace(f.SecretAccessKey), Poll: 30 * time.Second}
	for _, req := range [...][2]string{{"bucket", s.Bucket}, {"region", s.Region}} {
		if req[1] == "" {
			return s, fmt.Errorf("source.%s is required", req[0])
		}
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

// parseUntil reads a day, active through its end in UTC, or an exact RFC 3339 instant.
func parseUntil(s string) (time.Time, error) {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.AddDate(0, 0, 1), nil
	}
	return time.Parse(time.RFC3339, s)
}

func parseCond(fact string, v jsontext.Value, l version.Layout) (cond, error) {
	c := cond{fact: fact, l: l, exact: map[string]struct{}{}}
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
			c.pct = n
		case s[0] == '<' || s[0] == '>':
			bounds, err := parseBounds(s, l)
			if err != nil {
				return c, err
			}
			c.cmps = append(c.cmps, bounds)
		default:
			c.exact[s] = struct{}{}
		}
	}
	return c, nil
}

// parseBounds reads space-separated comparisons that must all hold, such as
// ">=2.0.0 <3.0.0"; the operand decides whether versions or dates are compared.
func parseBounds(s string, l version.Layout) ([]bound, error) {
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
		if val == "" && i+1 < len(toks) {
			i++
			val = toks[i]
		}
		kind := l.Kind(val)
		switch {
		case kind == version.Invalid:
			return nil, fmt.Errorf("%q: %q is neither a version nor a date like %s", s, val, l)
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
		return string(bytes.TrimSpace(v)), nil
	case 't':
		return "true", nil
	case 'f':
		return "false", nil
	case 'n':
		return "", nil
	}
	return "", errors.New("want a string, number or boolean")
}

func validName(s string) bool {
	return s != "" && len(s) <= 64 && !strings.ContainsFunc(s, func(c rune) bool {
		return (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '.' && c != '_' && c != '-'
	})
}
