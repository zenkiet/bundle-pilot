package handler

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/zenkiet/bundle-pilot/internal/usecase"
)

// Router serves the app and the admin API from the catalog's current snapshot.
type Router struct {
	cat      *usecase.Catalog
	log      *slog.Logger
	mu       sync.Mutex             // guards byVia and byBundle
	byVia    map[string]uint64      // decisions by how they were made, plus "refused"
	byBundle map[string]uint64      // decisions by the bundle chosen
	session  atomic.Pointer[string] // the password hash and Basic header that last passed
	failures failures
}

// New returns the gateway's HTTP handler; ui is the built admin app.
func New(cat *usecase.Catalog, ui fs.FS, log *slog.Logger) http.Handler {
	rt := &Router{
		cat:      cat,
		log:      log,
		byVia:    map[string]uint64{},
		byBundle: map[string]uint64{},
		failures: failures{m: map[netip.Prefix]failure{}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("POST /__gateway/data", rt.data)
	mux.HandleFunc("GET /__gateway/status", rt.auth("read", rt.status))
	mux.HandleFunc("GET /__gateway/config", rt.auth("read", rt.getConfig))
	mux.HandleFunc("PUT /__gateway/config", rt.auth("admin", rt.putConfig))
	mux.HandleFunc("POST /__gateway/decide", rt.auth("read", rt.decide))
	mux.HandleFunc("GET /__gateway/history", rt.auth("read", rt.history))
	mux.HandleFunc("GET /__gateway/history/{id}", rt.auth("read", rt.revision))
	mux.HandleFunc("GET /__gateway/bundles/{name}", rt.getBundle)
	mux.HandleFunc("GET /__gateway/bundles/{version}/{path...}", rt.getBundleFile)
	mux.HandleFunc("PUT /__gateway/bundles/{name}", rt.auth("upload", rt.putBundle))
	mux.HandleFunc("DELETE /__gateway/bundles/{name}", rt.auth("admin", rt.deleteBundle))
	mux.HandleFunc("PUT /__gateway/blocked/{version}", rt.auth("admin", rt.setBlocked))
	mux.HandleFunc("DELETE /__gateway/blocked/{version}", rt.auth("admin", rt.setBlocked))
	mux.HandleFunc("GET /__gateway/tokens", rt.auth("admin", rt.listTokens))
	mux.HandleFunc("POST /__gateway/tokens", rt.auth("admin", rt.createToken))
	mux.HandleFunc("DELETE /__gateway/tokens/{name}", rt.auth("admin", rt.deleteToken))
	mux.HandleFunc("POST /__gateway/reload", rt.auth("admin", rt.reload))
	mux.Handle("GET /__gateway/ui/", http.StripPrefix("/__gateway/ui/", adminUI(ui)))
	mux.Handle("GET /__gateway/", http.NotFoundHandler())
	mux.HandleFunc("GET /", rt.root)
	return mux
}

// root serves the app under its base path and redirects "/", or the base path without its
// trailing slash, to it.
func (rt *Router) root(w http.ResponseWriter, r *http.Request) {
	p, snap := r.URL.Path, rt.cat.Current()
	if len(snap.Bundles()) == 0 {
		http.Error(w, "no bundle deployed yet", http.StatusServiceUnavailable)
		return
	}
	base := snap.BasePath // always ends with "/"
	switch {
	case strings.HasPrefix(p, base):
		rel := p[len(base)-1:] // the path under base, keeping its leading "/"
		rt.static(w, r, snap, rel)
	case p == "/" || p == strings.TrimSuffix(base, "/"):
		if r.URL.RawQuery != "" {
			base += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, base, http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}

// adminUI serves the embedded admin app: hashed files are cached for a year, pages revalidate,
// and nothing may frame it.
func adminUI(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-cache")
		if strings.HasPrefix(r.URL.Path, "_app/immutable/") {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		files.ServeHTTP(w, r)
	})
}
