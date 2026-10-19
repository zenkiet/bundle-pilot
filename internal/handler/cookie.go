package handler

import (
	"net/http"
	"strconv"
	"time"
)

const (
	cookieName  = "bundle"       // the pinned bundle version, also the ?bundle= query name
	revCookie   = "bundle_rev"   // changes on every switch, for ngsw.json
	factsCookie = "bundle_facts" // the facts the SDK last posted
	cookieAge   = 365 * 24 * 3600
	decisionAge = 7 * 24 * 3600 // a pin made by a decision other than the default
)

func cookieValue(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return c.Value
	}
	return ""
}

// setBundle pins version v in the cookie; maxAge -1 clears the pin. changed also moves
// bundle_rev, so the service worker treats the switch as an update.
func setBundle(w http.ResponseWriter, r *http.Request, v string, maxAge int, changed bool) {
	setCookie(w, r, cookieName, v, maxAge)
	if changed {
		setCookie(w, r, revCookie, strconv.FormatInt(time.Now().UnixMilli(), 10), cookieAge)
	}
}

func setCookie(w http.ResponseWriter, r *http.Request, name, v string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    v,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}
