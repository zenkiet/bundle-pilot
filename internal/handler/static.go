package handler

import (
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/pkg/asset"
)

const (
	largeBody = 1 << 20
	minRate   = 16 << 10
)

// extendDeadline lets slow clients finish a large body; WriteTimeout still bounds the rest.
func extendDeadline(w http.ResponseWriter, n int64) {
	if n > largeBody {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute + time.Duration(n/minRate)*time.Second))
	}
}

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
	if a == b.Index() && query == "" {
		if f := cookieFacts(r); f != nil {
			if d, via, err := snap.Decide(f, time.Now()); err == nil {
				rt.apply(w, r, snap, d, via)
				b, a, owner = d, d.Index(), d
			}
		}
	}
	if query == b.Version && query != cookie && !a.Immutable() && r.Header.Get("Sec-Fetch-Site") != "cross-site" {
		setBundle(w, r, b.Version, cookieAge, cur.Version != b.Version)
	}
	extendDeadline(w, a.Size())
	w.Header()["X-Bundle-Version"] = owner.Tag()
	if a == b.Index() && b.Link != "" {
		w.Header().Set("Link", b.Link)
	}
	if rel == "/ngsw.json" {
		serveManifest(w, r, a)
		return
	}
	a.ServeHTTP(w, r)
}

// serveManifest stamps ngsw.json with the bundle_rev cookie, so the service worker
// sees every bundle switch as an update, even back to a version it knows.
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
