package handler

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
	"github.com/zenkiet/bundle-pilot/internal/pkg/version"
)

const maxZip = 256 << 20

// putBundle stores the body as versions/<name>.zip and reloads.
func (rt *Router) putBundle(w http.ResponseWriter, r *http.Request) {
	v, ok := rt.zipVersion(r.PathValue("name"))
	if !ok {
		layout := rt.cat.Current().Config.Dates.String()
		http.Error(w, "name must be <version>.zip, a version like 4.81.0 or a date like "+layout, http.StatusBadRequest)
		return
	}
	if scope(r) != "admin" && rt.cat.Current().Loaded(v) {
		http.Error(w, v+" exists: replacing a bundle takes an admin", http.StatusConflict)
		return
	}
	size := r.ContentLength
	if size <= 0 || size > maxZip {
		size = maxZip // unknown or past the limit: allow time for the largest zip, never longer
	}
	extendDeadline(w, size)
	start := time.Now()
	files, replaced, err := rt.cat.PutBundle(r.Context(), v, http.MaxBytesReader(w, r.Body, maxZip))
	if err != nil {
		status := http.StatusUnprocessableEntity
		if _, big := errors.AsType[*http.MaxBytesError](err); big {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}
	rt.log.Info("bundle uploaded",
		"version", v, "files", files, "replaced", replaced, "by", actor(r), "remote", clientIP(r))
	writeJSON(w, http.StatusOK, struct {
		Version  string `json:"version"`
		Files    int    `json:"files"`
		Replaced bool   `json:"replaced"`
		ReloadMs int64  `json:"reload_ms"`
	}{Version: v, Files: files, Replaced: replaced, ReloadMs: time.Since(start).Milliseconds()})
}

// getBundle serves versions/<v>.zip, without auth: OTA updaters need its URL. Only the zip a
// served bundle loaded from goes out, so a blocked or refused one cannot be installed.
func (rt *Router) getBundle(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	v, ok := rt.zipVersion(name)
	b := rt.cat.Current().Bundle(v)
	if !ok || b == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "no-cache")
	extendDeadline(w, b.ZipBytes)
	http.ServeContent(w, r, name, b.ModTime, io.NewSectionReader(b.Zip, 0, b.ZipBytes))
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

// zipVersion reads "<version>.zip", where version is a dotted version or a date in the
// config's date format.
func (rt *Router) zipVersion(name string) (string, bool) {
	v, ok := strings.CutSuffix(name, ".zip")
	if !ok || strings.HasPrefix(name, ".") {
		return v, false
	}
	return v, rt.cat.Current().Config.Dates.Kind(v) != version.Invalid
}

// deleteBundle refuses the default bundle so the gateway never runs out of bundles.
func (rt *Router) deleteBundle(w http.ResponseWriter, r *http.Request) {
	v, ok := rt.zipVersion(r.PathValue("name"))
	if !ok {
		http.Error(w, "name must be <version>.zip", http.StatusBadRequest)
		return
	}
	if rt.cat.Current().Default.Version == v {
		http.Error(w, v+" is the default bundle; set another default first", http.StatusConflict)
		return
	}
	err := rt.cat.DeleteBundle(r.Context(), v)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		http.Error(w, v+" is not on disk", http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.log.Info("bundle deleted", "version", v, "by", actor(r), "remote", clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

var errServing = errors.New("serves as the default")

// setBlocked blocks (PUT) or unblocks (DELETE) a bundle: it stays on disk, but nothing routes
// to it. The blocked list is not part of the editable config, so no config save or revert
// undoes it.
func (rt *Router) setBlocked(w http.ResponseWriter, r *http.Request) {
	v, on := r.PathValue("version"), r.Method == http.MethodPut
	if on && !rt.cat.Current().Loaded(v) {
		http.Error(w, v+" is not loaded", http.StatusNotFound)
		return
	}
	err := rt.cat.UpdateConfig(actor(r), func(cur *configv1.Config) (*configv1.Config, error) {
		cur.Blocked = slices.DeleteFunc(cur.Blocked, func(b string) bool { return b == v })
		if on {
			if v == rt.cat.Current().Default.Version {
				return nil, errServing
			}
			cur.Blocked = append(cur.Blocked, v)
		}
		return cur, nil
	})
	switch {
	case errors.Is(err, errServing):
		http.Error(w, v+" serves as the default: set another default first", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	rt.log.Info("bundle blocked", "version", v, "blocked", on, "by", actor(r), "remote", clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}

func (rt *Router) reload(w http.ResponseWriter, _ *http.Request) {
	start := time.Now()
	err := rt.cat.Reload()
	out := struct {
		ReloadMs int64  `json:"reload_ms"`
		Error    string `json:"error,omitempty"`
	}{ReloadMs: time.Since(start).Milliseconds()}
	status := http.StatusOK
	if err != nil {
		out.Error, status = err.Error(), http.StatusInternalServerError
	}
	writeJSON(w, status, out)
}
