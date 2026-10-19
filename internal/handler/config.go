package handler

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
	"github.com/zenkiet/bundle-pilot/internal/usecase"
)

const (
	maxConfig    = 1 << 20
	protobufType = "application/x-protobuf"
)

var errStale = errors.New("config changed since it was read: reload it")

// getConfig serves config.pb as clients see it, with the ETag a PUT sends back as If-Match.
func (rt *Router) getConfig(w http.ResponseWriter, r *http.Request) {
	pb, err := rt.cat.Config()
	if errors.Is(err, fs.ErrNotExist) {
		pb, err = &configv1.Config{}, nil
	}
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

// draft is the loaded bundles under config c: what saving c would serve.
func (rt *Router) draft(c domain.Config) *domain.Snapshot {
	snap := rt.cat.Current()
	inv := domain.Inventory{Bundles: snap.Bundles(), Config: c}
	return domain.NewSnapshot(inv, time.Now(), snap.Default.Version)
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
	out := struct {
		Saved  bool     `json:"saved"`
		Errors []string `json:"errors"`
		Issues []string `json:"issues"`
	}{}
	// check validates pb on top of the stored config, and collects its warnings.
	check := func(cur *configv1.Config) error {
		domain.KeepStored(pb, cur)
		c, err := domain.ParseConfig(pb)
		if err == nil {
			out.Issues = rt.draft(c).Issues
		}
		return err
	}
	if r.Header.Get("X-Dry-Run") != "" {
		cur, err := rt.cat.Config()
		if errors.Is(err, fs.ErrNotExist) {
			cur, err = &configv1.Config{}, nil
		}
		if err == nil {
			err = check(cur)
		}
		if err != nil {
			out.Errors = append(out.Errors, err.Error())
		}
	} else {
		match := r.Header.Get("If-Match")
		err := rt.cat.UpdateConfig(actor(r), func(cur *configv1.Config) (*configv1.Config, error) {
			if match != "" && match != domain.ETag(cur) {
				return nil, errStale
			}
			return pb, check(cur)
		})
		if errors.Is(err, errStale) {
			http.Error(w, err.Error(), http.StatusPreconditionFailed)
			return
		}
		out.Saved = err == nil || errors.Is(err, usecase.ErrNotApplied)
		if out.Saved {
			w.Header().Set("ETag", domain.ETag(pb))
			rt.log.Info("config saved", "by", actor(r), "remote", clientIP(r))
		}
		if err != nil {
			out.Errors = append(out.Errors, err.Error())
		}
	}
	status := http.StatusOK
	if len(out.Errors) > 0 && !out.Saved {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, out)
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
		if cur, err := rt.cat.Config(); err == nil {
			domain.KeepStored(pb, cur) // the blocked bundles
		}
		c, err := domain.ParseConfig(pb)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		snap = rt.draft(c)
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
