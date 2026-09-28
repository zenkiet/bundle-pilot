package handler

import (
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/zenkiet/bundle-pilot/internal/domain"
)

const (
	maxConfig    = 1 << 20
	protobufType = "application/x-protobuf"
	maskedSecret = "***"
)

// getConfig serves the stored config.pb, binary for protobuf clients and JSON
// otherwise, with the S3 secret masked.
func (rt *Router) getConfig(w http.ResponseWriter, r *http.Request) {
	pb, err := rt.cat.Config()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pb.Source != nil && pb.Source.SecretAccessKey != "" {
		pb.Source.SecretAccessKey = maskedSecret
	}
	if pb.Auth != nil && pb.Auth.PasswordHash != "" {
		pb.Auth.PasswordHash = maskedSecret
	}
	w.Header().Set("Cache-Control", "no-store")
	if strings.Contains(r.Header.Get("Accept"), protobufType) {
		b, _ := domain.EncodeConfig(pb)
		w.Header().Set("Content-Type", protobufType)
		_, _ = w.Write(b)
		return
	}
	b, _ := protojson.MarshalOptions{Multiline: true}.Marshal(pb)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// putConfig validates the posted config (protobuf or JSON) against the loaded
// bundles and, unless X-Dry-Run is set, stores it and reloads. A masked secret
// keeps the stored one.
func (rt *Router) putConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxConfig))
	if err != nil {
		http.Error(w, "config over 1 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	pb, err := domain.DecodeConfig(body, mt != protobufType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cur, _ := rt.cat.Config()
	if pb.Source != nil && pb.Source.SecretAccessKey == maskedSecret && cur != nil && cur.Source != nil {
		pb.Source.SecretAccessKey = cur.Source.SecretAccessKey
	}
	if pb.Auth != nil && pb.Auth.PasswordHash == maskedSecret && cur != nil && cur.Auth != nil {
		pb.Auth.PasswordHash = cur.Auth.PasswordHash
	}
	out := struct {
		Saved  bool     `json:"saved"`
		Errors []string `json:"errors"`
		Issues []string `json:"issues"`
	}{}
	if c, err := domain.ParseConfig(pb); err != nil {
		out.Errors = append(out.Errors, err.Error())
	} else if dry, err := domain.NewSnapshot(domain.Inventory{Bundles: rt.cat.Current().Bundles(), Config: c}, time.Now()); err != nil {
		out.Errors = append(out.Errors, err.Error())
	} else {
		out.Issues = dry.Issues
	}
	if len(out.Errors) == 0 && r.Header.Get("X-Dry-Run") == "" {
		if err := rt.cat.SaveConfig(pb); err != nil {
			out.Errors = append(out.Errors, err.Error())
		} else {
			out.Saved = true
		}
	}
	status := http.StatusOK
	if len(out.Errors) > 0 {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, out)
}
