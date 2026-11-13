package domain

import (
	"crypto/hkdf"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"aidanwoods.dev/go-paseto"
)

const (
	pbkdf2Rounds = 600_000
	sessionTTL   = 12 * time.Hour
	sessionMax   = 30 * 24 * time.Hour // how long after a login Refresh keeps renewing it
)

var errSession = errors.New("login expired or ended: sign in again")

type Auth struct {
	Username string
	Hash     string
}

// HashPassword returns pbkdf2-sha256$<rounds>$<salt>$<key>.
func HashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key, _ := pbkdf2.Key(sha256.New, password, salt, pbkdf2Rounds, 32)
	enc := base64.RawStdEncoding.EncodeToString
	return "pbkdf2-sha256$" + strconv.Itoa(pbkdf2Rounds) + "$" + enc(salt) + "$" + enc(key)
}

// VerifyPassword reads the rounds from the hash, so older hashes survive a pbkdf2Rounds change.
func VerifyPassword(hash, password string) bool {
	f := strings.Split(hash, "$")
	if len(f) != 4 || f[0] != "pbkdf2-sha256" {
		return false
	}
	rounds, err := strconv.Atoi(f[1])
	salt, err2 := base64.RawStdEncoding.DecodeString(f[2])
	want, err3 := base64.RawStdEncoding.DecodeString(f[3])
	if err != nil || err2 != nil || err3 != nil || rounds < 1 {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, rounds, len(want))
	return err == nil && subtle.ConstantTimeCompare(key, want) == 1
}

func (a Auth) Session(now time.Time) (token string, exp time.Time) {
	return a.issue(now, now)
}

// Refresh lets an app keep a session without storing the password.
func (a Auth) Refresh(token string, now time.Time) (string, time.Time, error) {
	t, err := a.parse(token)
	if err != nil {
		return "", time.Time{}, errSession
	}
	login, err := t.GetTime("auth_time")
	if err != nil || !now.Before(login.Add(sessionMax)) {
		return "", time.Time{}, errors.New("login is 30 days old: sign in again")
	}
	fresh, exp := a.issue(login, now)
	return fresh, exp, nil
}

func (a Auth) SessionExpiry(token string) (time.Time, bool) {
	t, err := a.parse(token)
	if err != nil {
		return time.Time{}, false
	}
	exp, err := t.GetExpiration()
	return exp, err == nil
}

func (a Auth) issue(login, now time.Time) (string, time.Time) {
	exp := now.Add(sessionTTL)
	limit := login.Add(sessionMax)
	if exp.After(limit) {
		exp = limit
	}
	t := paseto.NewToken()
	t.SetSubject(a.Username)
	t.SetIssuedAt(now)
	t.SetTime("auth_time", login) // when the password was checked; refreshes keep it
	t.SetExpiration(exp)
	return t.V4Encrypt(a.sessionKey(), nil), exp
}

func (a Auth) parse(token string) (*paseto.Token, error) {
	if a.Hash == "" {
		return nil, errSession
	}
	return paseto.NewParser().ParseV4Local(a.sessionKey(), token, nil)
}

// sessionKey derives from the hash and username: changing either ends every session, while a
// restart keeps them.
func (a Auth) sessionKey() paseto.V4SymmetricKey {
	k, _ := hkdf.Key(sha256.New, []byte(a.Hash), []byte(a.Username), "bundle-pilot session", 32)
	key, _ := paseto.V4SymmetricKeyFromBytes(k)
	return key
}
