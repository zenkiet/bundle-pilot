package handler

import (
	"net/http"
	"strconv"
	"time"
)

const (
	cookieName  = "bundle"
	revCookie   = "bundle_rev"
	factsCookie = "bundle_facts"
	cookieAge   = 365 * 24 * 3600
	decisionAge = 7 * 24 * 3600
)

func cookieValue(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return c.Value
	}
	return ""
}

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
