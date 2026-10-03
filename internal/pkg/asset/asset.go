package asset

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
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

// variant is one encoding of an asset, read from memory or from a span of an open zip.
type variant struct {
	size int64
	open func() io.ReadSeekCloser
	etag []string
	enc  []string
}

type Asset struct {
	v         [3]variant
	body      []byte
	ctype     []string
	cache     []string
	immutable bool
}

// FromZip serves a file in place from the open zip ra: sum is its SHA-256 (the ETag), head its
// first bytes (the content type), br its precompressed sidecar or nil. A deflate entry also
// goes out as gzip, its stream framed rather than recompressed.
func FromZip(name string, ra io.ReaderAt, f Span, sum [32]byte, head []byte, br *Span) *Asset {
	a := newAsset(name, head)
	tag := `"` + hex.EncodeToString(sum[:8])
	a.set(identity, tag, f.Size, func() io.ReadSeekCloser { return f.Open(ra) })
	best := f.Size
	if f.Framable && f.Raw+frameLen < best {
		best = f.Raw + frameLen
		a.set(gzipped, tag, best, func() io.ReadSeekCloser { return frame(ra, f) })
	}
	if br != nil && br.Size < best {
		a.set(brotli, tag, br.Size, func() io.ReadSeekCloser { return br.Open(ra) })
	}
	return a
}

// New keeps body and gz in memory: the stamped index.html and ngsw.json.
func New(name string, body, gz []byte) *Asset {
	a := newAsset(name, body)
	sum := sha256.Sum256(body)
	tag := `"` + hex.EncodeToString(sum[:8])
	a.body = body
	a.set(identity, tag, int64(len(body)), func() io.ReadSeekCloser { return nopCloser{bytes.NewReader(body)} })
	if len(gz) > 0 && len(gz) < len(body) {
		a.set(gzipped, tag, int64(len(gz)), func() io.ReadSeekCloser { return nopCloser{bytes.NewReader(gz)} })
	}
	return a
}

func newAsset(name string, head []byte) *Asset {
	a := &Asset{ctype: []string{contentType(name, head)}, cache: cacheRevalidate, immutable: hashed(name)}
	if a.immutable {
		a.cache = cacheImmutable
	}
	return a
}

func (a *Asset) set(e encoding, tag string, size int64, open func() io.ReadSeekCloser) {
	a.v[e] = variant{size: size, open: open, etag: []string{tag + etagSuffix[e]}, enc: encodings[e]}
}

func (a *Asset) has(e encoding) bool { return a.v[e].open != nil }

func (a *Asset) encoded() bool { return a.has(gzipped) || a.has(brotli) }

// Body is the content of an in-memory asset, nil for one served from a zip.
func (a *Asset) Body() []byte { return a.body }

func (a *Asset) Size() int64 { return a.v[identity].size }

func (a *Asset) ETag() string { return a.v[identity].etag[0] }

func (a *Asset) Immutable() bool { return a.immutable }

func init() {
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
	_ = mime.AddExtensionType(".xlsm", "application/vnd.ms-excel.sheet.macroEnabled.12")
}

func contentType(name string, head []byte) string {
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return http.DetectContentType(head)
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
