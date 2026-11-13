package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
	"github.com/zenkiet/bundle-pilot/internal/gen/gatewayv1"
	"github.com/zenkiet/bundle-pilot/internal/pkg/version"
)

const maxZip = 256 << 20

func (rt *Router) putBundle(w http.ResponseWriter, r *http.Request) {
	v, ok := rt.zipVersion(r.PathValue("name"))
	if !ok {
		layout := rt.cat.Current().Config.Dates.String()
		problem(w, r, http.StatusBadRequest, "name must be <version>.zip, a version like 4.81.0 or a date like "+layout)
		return
	}
	c := from(r.Context())
	if c.scope != "admin" && rt.cat.Current().Loaded(v) {
		problem(w, r, http.StatusConflict, v+" exists: replacing a bundle takes an admin")
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
		status, reason := http.StatusUnprocessableEntity, domain.RefusalReason(err)
		if _, big := errors.AsType[*http.MaxBytesError](err); big {
			status, reason = http.StatusRequestEntityTooLarge, "too_large"
		}
		writeProblem(w, r, status, err.Error(), reason)
		return
	}
	rt.log.Info("bundle uploaded",
		"version", v, "files", files, "replaced", replaced, "by", c.who, "remote", c.ip)
	rt.cat.Record(domain.Event{Kind: "upload", By: c.who, Bundle: v})
	writeJSON(w, http.StatusOK, struct {
		Version  string `json:"version"`
		Files    int    `json:"files"`
		Replaced bool   `json:"replaced"`
		ReloadMs int64  `json:"reload_ms"`
	}{Version: v, Files: files, Replaced: replaced, ReloadMs: time.Since(start).Milliseconds()})
}

// getBundle needs no auth, for OTA updaters, and sends only zips that serve: a blocked or
// refused one cannot be installed.
func (rt *Router) getBundle(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	v, ok := rt.zipVersion(name)
	b := rt.cat.Current().Bundle(v)
	if !ok || b == nil {
		problem(w, r, http.StatusNotFound, "no bundle "+name+" is served")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "no-cache")
	extendDeadline(w, b.ZipBytes)
	http.ServeContent(w, r, name, b.ModTime, io.NewSectionReader(b.Zip, 0, b.ZipBytes))
}

// getBundleFile serves files as zipped, the raw index.html included, for OTA updaters that check
// them against bundle.sha256.
func (rt *Router) getBundleFile(w http.ResponseWriter, r *http.Request) {
	b := rt.cat.Current().Bundle(r.PathValue("version"))
	if b == nil {
		problem(w, r, http.StatusNotFound, "no bundle "+r.PathValue("version")+" is served")
		return
	}
	a := b.File(path.Clean("/" + r.PathValue("path")))
	if a == nil {
		problem(w, r, http.StatusNotFound, "bundle "+b.Version+" has no such file")
		return
	}
	w.Header()["X-Bundle-Version"] = b.Tag()
	extendDeadline(w, a.Size())
	a.ServeHTTP(w, r)
}

func (rt *Router) zipVersion(name string) (string, bool) {
	v, ok := strings.CutSuffix(name, ".zip")
	if !ok || strings.HasPrefix(name, ".") {
		return v, false
	}
	return v, rt.cat.Current().Config.Dates.Kind(v) != version.Invalid
}

func (rt *Router) deleteBundle(w http.ResponseWriter, r *http.Request) {
	v, ok := rt.zipVersion(r.PathValue("name"))
	if !ok {
		problem(w, r, http.StatusBadRequest, "name must be <version>.zip")
		return
	}
	if rt.cat.Current().Default.Version == v {
		problem(w, r, http.StatusConflict, v+" is the default bundle; set another default first")
		return
	}
	err := rt.cat.DeleteBundle(r.Context(), v)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		problem(w, r, http.StatusNotFound, v+" is not on disk")
		return
	case err != nil:
		problem(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	c := from(r.Context())
	rt.log.Info("bundle deleted", "version", v, "by", c.who, "remote", c.ip)
	rt.cat.Record(domain.Event{Kind: "delete", By: c.who, Bundle: v})
	w.WriteHeader(http.StatusNoContent)
}

var errServing = errors.New("serves as the default")

// SetBlocked keeps the blocked list out of the editable config, so no save or revert undoes it.
func (rt *Router) SetBlocked(ctx context.Context, req *connect.Request[gatewayv1.SetBlockedRequest]) (*connect.Response[gatewayv1.SetBlockedResponse], error) {
	v, on, c := req.Msg.Version, req.Msg.Blocked, from(ctx)
	if on && !rt.cat.Current().Loaded(v) {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("%s is not loaded", v))
	}
	_, err := rt.cat.UpdateConfig(c.who, func(cur *configv1.Config) (*configv1.Config, error) {
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
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("%s serves as the default: set another default first", v))
	case err != nil:
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	rt.log.Info("bundle blocked", "version", v, "blocked", on, "by", c.who, "remote", c.ip)
	kind := "unblock"
	if on {
		kind = "block"
	}
	rt.cat.Record(domain.Event{Kind: kind, By: c.who, Bundle: v})
	return connect.NewResponse(&gatewayv1.SetBlockedResponse{}), nil
}
