package handler

import (
	"bytes"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/zenkiet/edge-gateway/internal/domain"
	"github.com/zenkiet/edge-gateway/internal/pkg/asset"
)

// Large files get a write deadline scaled to their size, so slow clients can
// finish them while the server-wide WriteTimeout still bounds everything else.
const (
	largeBody = 1 << 20
	minRate   = 16 << 10
)

func (rt *Router) static(w http.ResponseWriter, r *http.Request, snap *domain.Snapshot, rel string) {
	query, cookie := r.URL.Query().Get(cookieName), cookieValue(r, cookieName)
	b, cur := snap.Pick(query, cookie)
	a, owner := snap.Lookup(b, rel)
	if a == nil {
		if path.Ext(rel) != "" && !strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.NotFound(w, r)
			return
		}
		a, owner = b.Index(), b
	}
	if query == b.Version && query != cookie && !a.Immutable() && r.Header.Get("Sec-Fetch-Site") != "cross-site" {
		setBundle(w, r, b.Version, cookieAge, cur.Version != b.Version)
	}
	if n := len(a.Body()); n > largeBody {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute + time.Duration(n/minRate)*time.Second))
	}
	w.Header()["X-Bundle-Version"] = owner.Tag()
	switch {
	case rel == "/ngsw.json":
		serveManifest(w, r, a)
	case a == b.Index():
		serveIndex(w, r, a, b.Version)
	default:
		a.ServeHTTP(w, r)
	}
}

// serveIndex stamps <meta name="edge-gateway:bundle"> into <head>, so the app
// learns the version it runs as without a build-time copy of the zip name.
func serveIndex(w http.ResponseWriter, r *http.Request, a *asset.Asset, version string) {
	body := a.Body()
	i := bytes.Index(body, []byte("<head"))
	if i >= 0 {
		if n := bytes.IndexByte(body[i:], '>'); n >= 0 {
			i += n + 1
		} else {
			i = -1
		}
	}
	if i < 0 {
		a.ServeHTTP(w, r)
		return
	}
	etag := strings.TrimSuffix(a.ETag(), `"`) + "-" + version + `"`
	a.ServeParts(w, r, etag, append(body[:i:i], `<meta name="edge-gateway:bundle" content="`+version+`">`...), body[i:])
}

func serveManifest(w http.ResponseWriter, r *http.Request, a *asset.Asset) {
	body := a.Body()
	if len(body) == 0 || body[0] != '{' {
		a.ServeHTTP(w, r)
		return
	}
	rev := cookieValue(r, revCookie)
	if rev == "" || strings.Trim(rev, "0123456789") != "" {
		rev = "0"
	}
	etag := strings.TrimSuffix(a.ETag(), `"`) + "-" + rev + `"`
	a.ServeParts(w, r, etag, []byte(`{"gatewayRev":"`+rev+`",`), body[1:])
}
