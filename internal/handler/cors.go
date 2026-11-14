package handler

import (
	"net/http"
	"slices"
	"strings"
)

// cors allows only the listed origins, on /__gateway/. "*" lists every origin, but only once an
// admin password is set: until then the API is open, and the list is all that keeps another
// site's page from using it. Credentials are headers, never cookies, so it sends no
// Allow-Credentials.
func (rt *Router) cors(origins []string, next http.Handler) http.Handler {
	if len(origins) == 0 {
		return next
	}
	every := slices.Contains(origins, "*")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/__gateway/") {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		allowed := origin != "*" && slices.Contains(origins, origin)
		if every && origin != "" && rt.cat.Current().Config.Auth.Hash != "" {
			allowed = true
		}
		if !allowed {
			next.ServeHTTP(w, r)
			return
		}
		h.Set("Access-Control-Allow-Origin", origin)
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
			h.Set("Access-Control-Allow-Headers",
				"Authorization, Content-Type, Connect-Protocol-Version, Connect-Timeout-Ms, X-Setup-Token")
			h.Set("Access-Control-Max-Age", "7200")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.Set("Access-Control-Expose-Headers", "Retry-After")
		next.ServeHTTP(w, r)
	})
}
