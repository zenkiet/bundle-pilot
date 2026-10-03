package handler

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/pkg/version"
)

const maxZip = 256 << 20

// putBundle stores the body as versions/<name>.zip and reloads.
func (rt *Router) putBundle(w http.ResponseWriter, r *http.Request) {
	v, ok := rt.zipVersion(r.PathValue("name"))
	if !ok {
		http.Error(w, "name must be <version>.zip, a version like 4.81.0 or a date like "+rt.cat.Current().Config.Dates.String(), http.StatusBadRequest)
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

// getBundle serves versions/<v>.zip, without auth: OTA updaters need its URL.
func (rt *Router) getBundle(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, ok := rt.zipVersion(name); !ok {
		http.NotFound(w, r)
		return
	}
	f, err := rt.cat.OpenBundle(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "no-cache")
	extendDeadline(w, info.Size())
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// getBundleFile serves one file of a bundle as zipped, the raw index.html included, for OTA
// updaters fetching what its signed bundle.sha256 lists: no cookies, no index fallback.
func (rt *Router) getBundleFile(w http.ResponseWriter, r *http.Request) {
	b := rt.cat.Current().Bundle(r.PathValue("version"))
	if b == nil {
		http.NotFound(w, r)
		return
	}
	a := b.File(path.Clean("/" + r.PathValue("path")))
	if a == nil {
		http.NotFound(w, r)
		return
	}
	w.Header()["X-Bundle-Version"] = b.Tag()
	extendDeadline(w, a.Size())
	a.ServeHTTP(w, r)
}

func (rt *Router) zipVersion(name string) (string, bool) {
	v, ok := strings.CutSuffix(name, ".zip")
	return v, ok && !strings.HasPrefix(name, ".") && rt.cat.Current().Config.Dates.Kind(v) != version.Invalid
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
