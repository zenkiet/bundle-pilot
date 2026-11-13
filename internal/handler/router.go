package handler

import (
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/time/rate"

	"github.com/zenkiet/bundle-pilot/internal/gen/gatewayv1/gatewayv1connect"
	"github.com/zenkiet/bundle-pilot/internal/usecase"
)

const maxRequest = 1 << 20

type Router struct {
	cat      *usecase.Catalog
	log      *slog.Logger
	stop     <-chan struct{} // closed on drain, which ends watches
	mu       sync.Mutex      // guards byVia, byBundle and traffic
	byVia    map[string]uint64
	byBundle map[string]uint64
	traffic  traffic
	logins   logins
}

func New(cat *usecase.Catalog, origins []string, log *slog.Logger, stop <-chan struct{}) http.Handler {
	rt := &Router{
		cat:      cat,
		log:      log,
		stop:     stop,
		byVia:    map[string]uint64{},
		byBundle: map[string]uint64{},
		traffic:  traffic{byBundle: map[string]*[60]uint64{}, byVia: map[string]*[60]uint64{}},
		logins:   logins{m: map[netip.Prefix]*rate.Limiter{}},
	}
	api, rpc := gatewayv1connect.NewGatewayServiceHandler(rt,
		connect.WithInterceptors(authInterceptor{rt: rt}),
		connect.WithReadMaxBytes(maxRequest),
	)
	rpc = http.StripPrefix("/__gateway", rpc)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("POST /__gateway/data", rt.data)
	mux.HandleFunc("GET /__gateway/bundles/{name}", rt.getBundle)
	mux.HandleFunc("GET /__gateway/bundles/{version}/{path...}", rt.getBundleFile)
	mux.HandleFunc("PUT /__gateway/bundles/{name}", rt.auth("upload", rt.putBundle))
	mux.HandleFunc("DELETE /__gateway/bundles/{name}", rt.auth("admin", rt.deleteBundle))
	mux.Handle("POST /__gateway"+api, rpc) // Connect calls are POSTs
	mux.HandleFunc("POST /__gateway"+gatewayv1connect.GatewayServiceWatchProcedure, func(w http.ResponseWriter, r *http.Request) {
		// A watch outlives the server's read and write timeouts.
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Time{})
		_ = rc.SetWriteDeadline(time.Time{})
		rpc.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /__gateway/", func(w http.ResponseWriter, r *http.Request) {
		problem(w, r, http.StatusNotFound, "no such endpoint")
	})
	mux.HandleFunc("GET /", rt.root)
	return cors(origins, mux)
}

func (rt *Router) root(w http.ResponseWriter, r *http.Request) {
	p, snap := r.URL.Path, rt.cat.Current()
	if len(snap.Bundles()) == 0 {
		problem(w, r, http.StatusServiceUnavailable, "no bundle deployed yet")
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
		problem(w, r, http.StatusNotFound, "outside the app's base path "+base)
	}
}
