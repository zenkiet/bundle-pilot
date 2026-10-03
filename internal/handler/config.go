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

// getConfig serves config.pb as protobuf or JSON, with secrets masked.
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

// putConfig validates the posted config against the loaded bundles and, unless
// X-Dry-Run is set, stores it. A masked secret keeps the stored one, or is
// rejected when none is stored.
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
	if pb.GetSource().GetSecretAccessKey() == maskedSecret {
		pb.Source.SecretAccessKey = cur.GetSource().GetSecretAccessKey()
	}
	if pb.GetAuth().GetPasswordHash() == maskedSecret {
		pb.Auth.PasswordHash = cur.GetAuth().GetPasswordHash()
	}
	out := struct {
		Saved  bool     `json:"saved"`
		Errors []string `json:"errors"`
		Issues []string `json:"issues"`
	}{}
	if c, err := domain.ParseConfig(pb); err != nil {
		out.Errors = append(out.Errors, err.Error())
	} else {
		snap := rt.cat.Current()
		out.Issues = domain.NewSnapshot(domain.Inventory{Bundles: snap.Bundles(), Config: c}, time.Now(), snap.Default.Version).Issues
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
