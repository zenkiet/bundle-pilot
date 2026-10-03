package domain

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"
)

const pbkdf2Rounds = 600_000

type Auth struct {
	Username string `json:"username"`
	Hash     string `json:"-"`
}

func HashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key, _ := pbkdf2.Key(sha256.New, password, salt, pbkdf2Rounds, 32)
	enc := base64.RawStdEncoding.EncodeToString
	return "pbkdf2-sha256$" + strconv.Itoa(pbkdf2Rounds) + "$" + enc(salt) + "$" + enc(key)
}

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
