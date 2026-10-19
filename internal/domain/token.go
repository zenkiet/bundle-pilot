package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
)

// scopes, weakest first: read sees status, config and history, upload also adds bundles,
// admin does everything.
var scopes = []string{"read", "upload", "admin"}

// Token lets a script call the admin API: "Authorization: Bearer <secret>".
type Token struct {
	Name    string    `json:"name"`
	Scope   string    `json:"scope"`
	Created time.Time `json:"created,omitzero"`
	hash    [sha256.Size]byte
}

// NewToken returns a fresh secret, to show once, and the hash stored in its place.
func NewToken() (secret, hash string) {
	secret = "bp_" + rand.Text()
	sum := sha256.Sum256([]byte(secret))
	return secret, hex.EncodeToString(sum[:])
}

// Token finds the token a secret belongs to. It checks every hash in constant time, so timing
// does not reveal which tokens exist.
func (c Config) Token(secret string) (found Token, ok bool) {
	sum := sha256.Sum256([]byte(secret))
	for _, t := range c.Tokens {
		if subtle.ConstantTimeCompare(sum[:], t.hash[:]) == 1 {
			found, ok = t, true
		}
	}
	return found, ok
}

// Allows reports whether a token with scope have may do what scope need allows.
func Allows(have, need string) bool {
	return slices.Index(scopes, have) >= slices.Index(scopes, need)
}

func parseTokens(ts []*configv1.Token) ([]Token, error) {
	out := make([]Token, 0, len(ts))
	for _, t := range ts {
		sum, err := hex.DecodeString(t.Sha256)
		taken := slices.ContainsFunc(out, func(o Token) bool { return o.Name == t.Name })
		switch {
		case !validName(t.Name) || taken:
			return nil, fmt.Errorf("token %q: names are unique, 1-64 of A-Z a-z 0-9 . _ -", t.Name)
		case !slices.Contains(scopes, t.Scope):
			return nil, fmt.Errorf("token %s: scope %q, want read, upload or admin", t.Name, t.Scope)
		case err != nil || len(sum) != sha256.Size:
			return nil, fmt.Errorf("token %s: sha256 must be 64 hex digits", t.Name)
		}
		created, _ := time.Parse(time.RFC3339, t.Created)
		out = append(out, Token{Name: t.Name, Scope: t.Scope, Created: created, hash: [sha256.Size]byte(sum)})
	}
	return out, nil
}
