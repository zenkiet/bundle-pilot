package handler

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
)

const (
	maxConfig    = 1 << 20
	protobufType = "application/x-protobuf"
)

var errStale = errors.New("config changed since it was read: reload it")

// getConfig serves config.pb as clients see it, with the ETag a PUT sends back as If-Match.
func (rt *Router) getConfig(w http.ResponseWriter, r *http.Request) {
	pb, err := rt.cat.Config()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("ETag", domain.ETag(pb))
	writeConfig(w, r, domain.Redacted(pb))
}

// writeConfig encodes pb as protobuf for clients that accept it, else as JSON.
func writeConfig(w http.ResponseWriter, r *http.Request, pb *configv1.Config) {
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

// preview is what saving pb would serve: pb on top of the stored config, under the loaded bundles.
func (rt *Router) preview(pb *configv1.Config) (*domain.Snapshot, error) {
	cur, err := rt.cat.Config()
	if err != nil {
		return nil, err
	}
	domain.KeepStored(pb, cur)
	c, err := domain.ParseConfig(pb)
	if err != nil {
		return nil, err
	}
	snap := rt.cat.Current()
	return domain.NewSnapshot(domain.Inventory{Bundles: snap.Bundles(), Config: c}, time.Now(), snap.Default.Version), nil
}

// putConfig validates the posted config against the loaded bundles and saves it, unless
// X-Dry-Run is set. A stale If-Match gets 412. Masked secrets and a missing auth keep the
// stored values.
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
	var out struct {
		Saved  bool     `json:"saved"`
		Errors []string `json:"errors"`
		Issues []string `json:"issues"`
	}
	var snap *domain.Snapshot // what serves, or would, under pb
	if r.Header.Get("X-Dry-Run") != "" {
		snap, err = rt.preview(pb)
	} else {
		match := r.Header.Get("If-Match")
		err = rt.cat.UpdateConfig(actor(r), func(cur *configv1.Config) (*configv1.Config, error) {
			if match != "" && match != domain.ETag(cur) {
				return nil, errStale
			}
			domain.KeepStored(pb, cur)
			return pb, nil // UpdateConfig validates it
		})
		if errors.Is(err, errStale) {
			http.Error(w, err.Error(), http.StatusPreconditionFailed)
			return
		}
		if err == nil {
			out.Saved, snap = true, rt.cat.Current()
			w.Header().Set("ETag", domain.ETag(pb))
			rt.log.Info("config saved", "by", actor(r), "remote", clientIP(r))
		}
	}
	if err != nil {
		out.Errors = []string{err.Error()}
		writeJSON(w, http.StatusUnprocessableEntity, out)
		return
	}
	out.Issues = snap.Issues
	writeJSON(w, http.StatusOK, out)
}

// decide backs the admin's decision tester. It answers what /data would for the posted facts,
// under the posted draft config when there is one, and says why each earlier rule was passed
// over. It sets no cookie and counts nothing.
func (rt *Router) decide(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Facts  jsontext.Value `json:"facts"`
		Config jsontext.Value `json:"config"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxConfig), &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	facts, err := domain.ParseFacts(in.Facts)
	if err != nil {
		http.Error(w, "facts: "+err.Error(), http.StatusBadRequest)
		return
	}
	snap := rt.cat.Current()
	if len(in.Config) > 0 {
		pb, err := domain.DecodeConfig(in.Config, true)
		if err != nil {
			http.Error(w, "config: "+err.Error(), http.StatusBadRequest)
			return
		}
		snap, err = rt.preview(pb)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}
	b, via, why, err := snap.Explain(facts, time.Now())
	out := struct {
		Bundle string   `json:"bundle"`
		Via    string   `json:"via"`
		Why    []string `json:"why"`
		Error  string   `json:"error,omitempty"`
	}{Via: via, Why: why}
	if err != nil {
		out.Error = err.Error()
	} else {
		out.Bundle = b.Version
	}
	writeJSON(w, http.StatusOK, out)
}
