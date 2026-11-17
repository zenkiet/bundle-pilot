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

var scopes = []string{"read", "upload", "admin"} // weakest first

const expiryWarning = 7 * 24 * time.Hour

type Token struct {
	Name    string
	Scope   string
	Created time.Time
	Expires time.Time // zero: never
	hash    [sha256.Size]byte
}

func (t Token) Expired(now time.Time) bool {
	return !t.Expires.IsZero() && !now.Before(t.Expires)
}

func tokenIssues(tokens []Token, now time.Time) []Issue {
	var out []Issue
	for _, t := range tokens {
		switch {
		case t.Expires.IsZero():
		case t.Expired(now):
			out = append(out, Issue{
				Code:  "token_expired",
				Text:  fmt.Sprintf("token %s expired on %s: delete it", t.Name, t.Expires.Format(time.DateOnly)),
				Token: t.Name,
			})
		case now.Add(expiryWarning).After(t.Expires):
			out = append(out, Issue{
				Code:  "token_expiring",
				Text:  fmt.Sprintf("token %s expires on %s", t.Name, t.Expires.Format(time.DateOnly)),
				Token: t.Name,
			})
		}
	}
	return out
}

func NewToken() (secret, hash string) {
	secret = "bp_" + rand.Text()
	sum := sha256.Sum256([]byte(secret))
	return secret, hex.EncodeToString(sum[:])
}

// Token checks every hash in constant time, so timing does not reveal which tokens exist.
func (c Config) Token(secret string) (found Token, ok bool) {
	sum := sha256.Sum256([]byte(secret))
	for _, t := range c.Tokens {
		if subtle.ConstantTimeCompare(sum[:], t.hash[:]) == 1 {
			found, ok = t, true
		}
	}
	return found, ok
}

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
		token := Token{Name: t.Name, Scope: t.Scope, Created: created, hash: [sha256.Size]byte(sum)}
		if t.Expires != "" {
			token.Expires, err = time.Parse(time.RFC3339, t.Expires)
			if err != nil { // ignoring a typo would make the token last forever
				return nil, fmt.Errorf("token %s: expires %q is not an RFC 3339 time", t.Name, t.Expires)
			}
		}
		out = append(out, token)
	}
	return out, nil
}
