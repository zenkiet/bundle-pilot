package asset

import (
	"encoding/hex"
	"mime"
	"net/http"
	"path"
	"strings"
)

type encoding uint8

const (
	identity encoding = iota
	gzipped
	brotli
)

var (
	cacheImmutable  = []string{"public, max-age=31536000, immutable"}
	cacheRevalidate = []string{"private, no-cache"}
	nosniff         = []string{"nosniff"}
	varyEncoding    = []string{"Accept-Encoding"}
	encodings       = [...][]string{gzipped: {"gzip"}, brotli: {"br"}}
	etagSuffix      = [...]string{identity: `"`, gzipped: `-gz"`, brotli: `-br"`}
)

type variant struct {
	body []byte
	etag []string
	enc  []string
}

// Blob is one unique content with its precomputed encodings; it is shared by
// every asset whose bytes hash the same.
type Blob struct {
	sum     [32]byte
	v       [3]variant
	encoded bool
}

func NewBlob(sum [32]byte, body, gz, br []byte) *Blob {
	b := &Blob{sum: sum}
	tag := `"` + hex.EncodeToString(sum[:8])
	b.set(identity, tag, body)
	if len(gz) > 0 && len(gz) < len(body) {
		b.set(gzipped, tag, gz)
	}
	if len(br) > 0 && len(br) < len(body) {
		b.set(brotli, tag, br)
	}
	b.encoded = b.has(gzipped) || b.has(brotli)
	return b
}

func (b *Blob) set(e encoding, tag string, body []byte) {
	b.v[e] = variant{
		body: body,
		etag: []string{tag + etagSuffix[e]},
		enc:  encodings[e],
	}
}

func (b *Blob) Sum() [32]byte { return b.sum }

// Size is the RAM held by every representation of the blob.
func (b *Blob) Size() int {
	n := 0
	for _, v := range b.v {
		n += len(v.body)
	}
	return n
}

func (b *Blob) has(e encoding) bool { return b.v[e].body != nil }

type Asset struct {
	blob      *Blob
	ctype     []string
	cache     []string
	immutable bool
}

func New(name string, b *Blob) *Asset {
	a := &Asset{
		blob:      b,
		ctype:     []string{contentType(name, b.v[identity].body)},
		cache:     cacheRevalidate,
		immutable: hashed(name),
	}
	if a.immutable {
		a.cache = cacheImmutable
	}
	return a
}

func (a *Asset) Blob() *Blob { return a.blob }

func (a *Asset) Body() []byte { return a.blob.v[identity].body }

func (a *Asset) ETag() string { return a.blob.v[identity].etag[0] }

func (a *Asset) Immutable() bool { return a.immutable }

func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	_ = mime.AddExtensionType(".xlsm", "application/vnd.ms-excel.sheet.macroEnabled.12")
}

func contentType(name string, body []byte) string {
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return http.DetectContentType(body)
}

func Compressible(name string, body []byte) bool {
	if len(body) <= 256 {
		return false
	}
	ct := contentType(name, body)
	return strings.HasPrefix(ct, "text/") || strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "json") || strings.Contains(ct, "xml") || ct == "application/wasm"
}

func hashed(name string) bool {
	if strings.Contains(name, "/immutable/") {
		return true
	}
	base := path.Base(name)
	ext := path.Ext(base)
	if ext == "" || ext == ".html" {
		return false
	}
	stem := strings.TrimSuffix(base, ext)
	i := strings.LastIndexAny(stem, "-.")
	if i < 0 {
		return false
	}
	tok := stem[i+1:]
	digit, upper, lower, hexOnly := false, false, false, true
	for _, c := range tok {
		switch {
		case c >= '0' && c <= '9':
			digit = true
		case c >= 'a' && c <= 'f':
			lower = true
		case c >= 'a' && c <= 'z':
			lower, hexOnly = true, false
		case c >= 'A' && c <= 'Z':
			upper, hexOnly = true, false
		case c == '_':
			hexOnly = false
		default:
			return false
		}
	}
	switch {
	case len(tok) >= 16:
		return hexOnly
	case len(tok) == 8:
		return upper && !lower || digit && (upper || lower)
	}
	return false
}
