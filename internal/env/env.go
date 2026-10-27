// Package env reads the gateway's settings from environment variables. Routing rules live in
// config.pb instead, see domain.Config.
package env

import (
	"cmp"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
)

type Config struct {
	Addr      string            // ":" + PORT, default :8080
	Dist      string            // DIST, default ./dist
	LogLevel  slog.Level        // LOG_LEVEL, default info
	BundleKey ed25519.PublicKey // BUNDLE_PUBKEY; when set, only signed bundles load
}

// Load reads the settings through getenv, normally os.Getenv.
func Load(getenv func(string) string) (Config, error) {
	env := func(key, fallback string) string { return cmp.Or(strings.TrimSpace(getenv(key)), fallback) }
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
