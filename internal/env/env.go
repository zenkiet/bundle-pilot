// Package env reads settings from environment variables; routing lives in config.pb.
package env

import (
	"cmp"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

type Config struct {
	Addr      string            // ":" + PORT, default :8080
	Dist      string            // DIST, default ./dist
	LogLevel  slog.Level        // LOG_LEVEL, default info
	BundleKey ed25519.PublicKey // BUNDLE_PUBKEY; when set, only signed bundles load
	Origins   []string          // CORS_ORIGINS, the app's by default
}

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
	// The default lets the Bundle Pilot app in: its iOS WebView is capacitor://localhost, its
	// Android one https://localhost.
	for o := range strings.SplitSeq(env("CORS_ORIGINS", "capacitor://localhost,https://localhost"), ",") {
		if o = strings.TrimSpace(o); o == "" {
			continue
		}
		u, err := url.Parse(o)
		if err != nil || u.Scheme == "" || u.Host == "" || strings.Trim(u.Path, "/") != "" {
			return cfg, fmt.Errorf("CORS_ORIGINS: %q is not an origin like https://portal.example.com", o)
		}
		cfg.Origins = append(cfg.Origins, u.Scheme+"://"+strings.ToLower(u.Host)) // as browsers send it
	}
	return cfg, nil
}
