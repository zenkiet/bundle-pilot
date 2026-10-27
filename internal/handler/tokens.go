package handler

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
)

var errNoToken = errors.New("no such token")

// listTokens names the API tokens and their scopes; each secret was shown once, at creation.
func (rt *Router) listTokens(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, rt.cat.Current().Config.Tokens)
}

// createToken creates a token from {"name": "ci", "scope": "upload"} and answers 201 with its
// secret, shown only this once. It needs an admin password first: until one is set the API is
// open anyway, and a token saved during setup would end setup with the API still open.
func (rt *Router) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 4<<10), &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if rt.cat.Current().Config.Auth.Hash == "" {
		http.Error(w, "set an admin password first: until then the admin API is open to anyone", http.StatusConflict)
		return
	}
	secret, hash := domain.NewToken()
	token := &configv1.Token{
		Name:    in.Name,
		Scope:   in.Scope,
		Sha256:  hash,
		Created: time.Now().UTC().Format(time.RFC3339),
	}
	err := rt.cat.UpdateConfig(actor(r), func(cur *configv1.Config) (*configv1.Config, error) {
		cur.Tokens = append(cur.Tokens, token)
		return cur, nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	rt.log.Info("token created", "name", in.Name, "scope", in.Scope, "by", actor(r), "remote", clientIP(r))
	writeJSON(w, http.StatusCreated, struct {
		Token string `json:"token"`
	}{Token: secret})
}

// deleteToken revokes a token at once.
func (rt *Router) deleteToken(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	err := rt.cat.UpdateConfig(actor(r), func(cur *configv1.Config) (*configv1.Config, error) {
		before := len(cur.Tokens)
		cur.Tokens = slices.DeleteFunc(cur.Tokens, func(t *configv1.Token) bool { return t.Name == name })
		if len(cur.Tokens) == before {
			return nil, errNoToken
		}
		return cur, nil
	})
	switch {
	case errors.Is(err, errNoToken):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	rt.log.Info("token revoked", "name", name, "by", actor(r), "remote", clientIP(r))
	w.WriteHeader(http.StatusNoContent)
}
