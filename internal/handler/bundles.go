package handler

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/pkg/version"
)

const maxZip = 256 << 20

// putBundle stores the raw body as versions/<name>.zip, replacing a zip of the
// same name, and reloads before answering.
func (rt *Router) putBundle(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	v, ok := strings.CutSuffix(name, ".zip")
	layout := rt.cat.Current().Config.Dates
	if layout.String() == "" {
		layout = version.Default
	}
	if !ok || strings.HasPrefix(name, ".") || layout.Kind(v) == version.Invalid {
		http.Error(w, "name must be <version>.zip, a version like 4.81.0 or a date like "+layout.String(), http.StatusBadRequest)
		return
	}
	start := time.Now()
	files, replaced, err := rt.cat.PutBundle(r.Context(), v, http.MaxBytesReader(w, r.Body, maxZip))
	if err != nil {
		status := http.StatusUnprocessableEntity
		var big *http.MaxBytesError
		if errors.As(err, &big) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}
	rt.log.Info("bundle uploaded", "version", v, "files", files, "replaced", replaced)
	writeJSON(w, http.StatusOK, struct {
		Version  string `json:"version"`
		Files    int    `json:"files"`
		Replaced bool   `json:"replaced"`
		ReloadMs int64  `json:"reload_ms"`
	}{v, files, replaced, time.Since(start).Milliseconds()})
}

// deleteBundle refuses the default bundle so the gateway never runs out of bundles.
func (rt *Router) deleteBundle(w http.ResponseWriter, r *http.Request) {
	v := strings.TrimSuffix(r.PathValue("name"), ".zip")
	if rt.cat.Current().Default.Version == v {
		http.Error(w, v+" is the default bundle; set another default first", http.StatusConflict)
		return
	}
	if err := rt.cat.DeleteBundle(r.Context(), v); errors.Is(err, fs.ErrNotExist) {
		http.Error(w, v+" is not on disk", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.log.Info("bundle deleted", "version", v)
	w.WriteHeader(http.StatusNoContent)
}

func (rt *Router) reload(w http.ResponseWriter, _ *http.Request) {
	start := time.Now()
	err := rt.cat.Reload()
	snap := rt.cat.Current()
	out := struct {
		ReloadMs int64    `json:"reload_ms"`
		Default  string   `json:"default"`
		Bundles  int      `json:"bundles"`
		Error    string   `json:"error,omitempty"`
		Errors   []string `json:"errors"`
		Issues   []string `json:"issues"`
	}{time.Since(start).Milliseconds(), snap.Default.Version, len(snap.Bundles()), "", snap.Errors, snap.Issues}
	status := http.StatusOK
	if err != nil {
		out.Error, status = err.Error(), http.StatusInternalServerError
	}
	writeJSON(w, status, out)
}
