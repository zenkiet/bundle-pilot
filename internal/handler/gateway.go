package handler

import (
	"encoding/json/v2"
	"io"
	"maps"
	"mime"
	"net/http"
	"time"

	"github.com/zenkiet/edge-gateway/internal/domain"
)

const maxFactsBody = 4 << 10

var crossOrigin = http.NewCrossOriginProtection()

// data decides the bundle from the facts the frontend posts and pins it in the
// cookie; a default decision clears the cookie so later default changes apply.
// X-Dry-Run answers without touching cookies or counters.
func (rt *Router) data(w http.ResponseWriter, r *http.Request) {
	if err := crossOrigin.Check(r); err != nil {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		http.Error(w, "send Content-Type: application/json", http.StatusUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxFactsBody))
	if err != nil {
		http.Error(w, "facts body over 4 KiB", http.StatusRequestEntityTooLarge)
		return
	}
	facts, err := domain.ParseFacts(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	snap := rt.cat.Current()
	b, via, err := snap.Decide(facts, time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if r.Header.Get("X-Dry-Run") == "" {
		cookie := cookieValue(r, cookieName)
		_, cur := snap.Pick("", cookie)
		switch {
		case via != "default":
			setBundle(w, r, b.Version, decisionAge, cur.Version != b.Version)
		case cookie != "":
			setBundle(w, r, "", -1, cur.Version != b.Version)
		}
		rt.mu.Lock()
		rt.decisions[via]++
		rt.mu.Unlock()
		if cur.Version != b.Version {
			rt.log.Debug("bundle switch", "from", cur.Version, "to", b.Version, "via", via)
		}
	}
	writeJSON(w, struct {
		Bundle string `json:"bundle"`
		Via    string `json:"via"`
	}{b.Version, via})
}

type bundleStatus struct {
	Version string `json:"version"`
	Files   int    `json:"files"`
	Bytes   int    `json:"bytes"`
}

func (rt *Router) status(w http.ResponseWriter, _ *http.Request) {
	snap := rt.cat.Current()
	reloads, lastErr := rt.cat.Stats()
	rt.mu.Lock()
	decisions := maps.Clone(rt.decisions)
	rt.mu.Unlock()
	out := struct {
		Default     string            `json:"default"`
		DefaultFrom string            `json:"default_from"`
		BasePath    string            `json:"base_path"`
		Source      string            `json:"source"`
		LoadedAt    time.Time         `json:"loaded_at"`
		Reloads     uint64            `json:"reloads"`
		LastError   string            `json:"last_error"`
		Errors      []string          `json:"errors"`
		Resident    int               `json:"resident_bytes"`
		Bundles     []bundleStatus    `json:"bundles"`
		Rules       []domain.Rule     `json:"rules"`
		Backend     domain.Mapping    `json:"backend"`
		Decisions   map[string]uint64 `json:"decisions"`
		Issues      []string          `json:"issues"`
	}{
		snap.Default.Version, snap.DefaultFrom, snap.BasePath, snap.Config.Source.String(), snap.LoadedAt, reloads, lastErr, snap.Errors,
		snap.Resident, nil, snap.Config.Rules, snap.Config.Backend, decisions, snap.Issues,
	}
	for _, b := range snap.Bundles() {
		out.Bundles = append(out.Bundles, bundleStatus{b.Version, b.Len(), b.Bytes()})
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.MarshalWrite(w, v)
}
