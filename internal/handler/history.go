package handler

import (
	"errors"
	"io/fs"
	"net/http"
)

// history lists the saved configs, newest first, with who saved each.
func (rt *Router) history(w http.ResponseWriter, _ *http.Request) {
	revs, err := rt.cat.History()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, revs)
}

// revision serves one saved config the way getConfig does, for a diff or a revert.
func (rt *Router) revision(w http.ResponseWriter, r *http.Request) {
	pb, err := rt.cat.Revision(r.PathValue("id"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		http.NotFound(w, r)
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	default:
		writeConfig(w, r, pb)
	}
}
