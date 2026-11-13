package handler

import (
	"encoding/json/v2"
	"net/http"
)

// problem answers an RFC 9457 problem of type about:blank.
func problem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	writeProblem(w, r, status, detail, "")
}

// writeProblem adds reason, why a zip was refused, as an extension member.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail, reason string) {
	h := w.Header()
	h.Del("Content-Length") // it may be for the content this replaces
	h.Set("Content-Type", "application/problem+json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.MarshalWrite(w, struct {
		Type     string `json:"type"`
		Title    string `json:"title"`
		Status   int    `json:"status"`
		Detail   string `json:"detail,omitempty"`
		Instance string `json:"instance"`
		Reason   string `json:"reason,omitempty"`
	}{"about:blank", http.StatusText(status), status, detail, r.URL.Path, reason})
}
