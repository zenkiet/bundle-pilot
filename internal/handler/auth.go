package handler

import (
	"crypto/subtle"
	"net/http"

	"github.com/zenkiet/edge-gateway/internal/domain"
)

type session struct{ hash, header string }

// auth requires HTTP Basic credentials once config sets them. The last header
// that passed is remembered per hash, so status polls skip the PBKDF2 work.
func (rt *Router) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := rt.cat.Current().Config.Auth
		if a.Hash == "" {
			next(w, r)
			return
		}
		h := r.Header.Get("Authorization")
		if s := rt.session.Load(); s != nil && s.hash == a.Hash && subtle.ConstantTimeCompare([]byte(s.header), []byte(h)) == 1 {
			next(w, r)
			return
		}
		user, pw, ok := r.BasicAuth()
		if ok && subtle.ConstantTimeCompare([]byte(user), []byte(a.Username)) == 1 && domain.VerifyPassword(a.Hash, pw) {
			rt.session.Store(&session{a.Hash, h})
			next(w, r)
			return
		}
		http.Error(w, "sign in required", http.StatusUnauthorized)
	}
}
