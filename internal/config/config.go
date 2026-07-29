package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
)

type Config struct {
	Addr      string
	Dist      string
	LogLevel  slog.Level
	BundleKey ed25519.PublicKey
}

func Load(getenv func(string) string) (Config, error) {
	env := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	cfg := Config{Addr: ":" + env("PORT", "8080"), Dist: env("DIST", "./dist")}
	if err := cfg.LogLevel.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		return cfg, fmt.Errorf("LOG_LEVEL: want debug, info, warn or error")
	}
	if k := env("BUNDLE_PUBKEY", ""); k != "" {
		key, err := hex.DecodeString(k)
		if err != nil {
			key, err = base64.StdEncoding.DecodeString(k)
		}
		if err != nil || len(key) != ed25519.PublicKeySize {
			return cfg, fmt.Errorf("BUNDLE_PUBKEY: want a 32-byte Ed25519 public key as hex or base64")
		}
		cfg.BundleKey = key
	}
	return cfg, nil
}
