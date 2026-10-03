package handler

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/zenkiet/bundle-pilot/internal/usecase"
)

type Router struct {
	cat       *usecase.Catalog
	log       *slog.Logger
	mu        sync.Mutex
	decisions map[string]uint64
	session   atomic.Pointer[session]
}

func New(cat *usecase.Catalog, ui fs.FS, log *slog.Logger) http.Handler {
	rt := &Router{cat: cat, log: log, decisions: map[string]uint64{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("POST /__gateway/data", rt.data)
	mux.HandleFunc("GET /__gateway/status", rt.auth(rt.status))
	mux.HandleFunc("GET /__gateway/config", rt.auth(rt.getConfig))
	mux.HandleFunc("PUT /__gateway/config", rt.auth(rt.putConfig))
	mux.HandleFunc("GET /__gateway/bundles/{name}", rt.getBundle)
	mux.HandleFunc("GET /__gateway/bundles/{version}/{path...}", rt.getBundleFile)
	mux.HandleFunc("PUT /__gateway/bundles/{name}", rt.auth(rt.putBundle))
	mux.HandleFunc("DELETE /__gateway/bundles/{name}", rt.auth(rt.deleteBundle))
	mux.HandleFunc("POST /__gateway/reload", rt.auth(rt.reload))
	mux.Handle("GET /__gateway/ui/", http.StripPrefix("/__gateway/ui/", http.FileServerFS(ui)))
	mux.Handle("GET /__gateway/", http.NotFoundHandler())
	mux.HandleFunc("GET /", rt.root)
	return mux
}

func (rt *Router) root(w http.ResponseWriter, r *http.Request) {
	p, snap := r.URL.Path, rt.cat.Current()
	if len(snap.Bundles()) == 0 {
		http.Error(w, "no bundle deployed yet", http.StatusServiceUnavailable)
		return
	}
	switch base := snap.BasePath; {
	case strings.HasPrefix(p, base):
		rt.static(w, r, snap, p[len(base)-1:])
	case p == "/" || len(p)+1 == len(base) && strings.HasPrefix(base, p):
		if r.URL.RawQuery != "" {
			base += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, base, http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}
