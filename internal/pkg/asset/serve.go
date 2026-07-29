package asset

import (
	"bytes"
	"net/http"
	"slices"
	"strings"
	"time"
)

// ServeHTTP hands the negotiated representation to http.ServeContent, which
// covers conditional requests, ranges and HEAD; ranges get the identity body.
func (a *Asset) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	v := &a.blob.v[identity]
	if _, ranged := r.Header["Range"]; !ranged {
		v = &a.blob.v[negotiate(r.Header["Accept-Encoding"], a.blob)]
	}
	h := w.Header()
	h["Cache-Control"] = a.cache
	h["Content-Type"] = a.ctype
	h["X-Content-Type-Options"] = nosniff
	h["Etag"] = v.etag
	if a.blob.encoded {
		h["Vary"] = varyEncoding
	}
	if v.enc != nil {
		h["Content-Encoding"] = v.enc
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(v.body))
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

func negotiate(accept []string, b *Blob) encoding {
	if !b.encoded || len(accept) == 0 {
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
	case br == accepted && b.has(brotli):
		return brotli
	case gz == accepted && b.has(gzipped):
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
