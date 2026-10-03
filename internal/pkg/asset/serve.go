package asset

import (
	"bytes"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// ServeHTTP sends the negotiated encoding through http.ServeContent; ranges get the identity body.
// A read error aborts the response: encoded bodies have no Content-Length, so a short one would
// otherwise end as a well-formed chunked reply.
func (a *Asset) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Range"), ",") {
		r.Header.Del("Range") // a multi-range reader would outlive ServeContent: send it all
	}
	v := &a.v[identity]
	if r.Header.Get("Range") == "" {
		v = &a.v[negotiate(r.Header["Accept-Encoding"], a)]
	}
	h := w.Header()
	h["Cache-Control"] = a.cache
	h["Content-Type"] = a.ctype
	h["X-Content-Type-Options"] = nosniff
	h["Etag"] = v.etag
	if a.encoded() {
		h["Vary"] = varyEncoding
	}
	if v.enc != nil {
		h["Content-Encoding"] = v.enc
	}
	c := &checked{ReadSeekCloser: v.open()}
	http.ServeContent(w, r, "", time.Time{}, c)
	_ = c.Close()
	if c.err != nil {
		panic(http.ErrAbortHandler)
	}
}

// checked remembers a read error, which ServeContent drops.
type checked struct {
	io.ReadSeekCloser
	err error
}

func (c *checked) Read(p []byte) (int, error) {
	n, err := c.ReadSeekCloser.Read(p)
	if err != nil && err != io.EOF {
		c.err = err
	}
	return n, err
}

// ServeParts serves head followed by body under the asset's type and cache.
func (a *Asset) ServeParts(w http.ResponseWriter, r *http.Request, etag string, head, body []byte) {
	h := w.Header()
	h["Cache-Control"] = a.cache
	h["Content-Type"] = a.ctype
	h["X-Content-Type-Options"] = nosniff
	h["Etag"] = []string{etag}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(slices.Concat(head, body)))
}

const (
	unset int8 = iota
	refused
	accepted
)

func negotiate(accept []string, a *Asset) encoding {
	if !a.encoded() || len(accept) == 0 {
		return identity
	}
	br, gz, star := unset, unset, unset
	for _, line := range accept {
		for line != "" {
			var tok string
			tok, line, _ = strings.Cut(line, ",")
			name, params, _ := strings.Cut(tok, ";")
			q := accepted
			if qZero(params) {
				q = refused
			}
			switch name = strings.TrimSpace(name); {
			case strings.EqualFold(name, "br"):
				br = q
			case strings.EqualFold(name, "gzip"), strings.EqualFold(name, "x-gzip"):
				gz = q
			case name == "*":
				star = q
			}
		}
	}
	if br == unset {
		br = star
	}
	if gz == unset {
		gz = star
	}
	switch {
	case br == accepted && a.has(brotli):
		return brotli
	case gz == accepted && a.has(gzipped):
		return gzipped
	}
	return identity
}

func qZero(params string) bool {
	for params != "" {
		var p string
		p, params, _ = strings.Cut(params, ";")
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok && (k == "q" || k == "Q") {
			return strings.Trim(strings.TrimSpace(v), "0.") == ""
		}
	}
	return false
}
