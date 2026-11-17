package handler

import (
	"context"
	"encoding/json/v2"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"runtime/debug"
	"runtime/metrics"
	"slices"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/gatewayv1"
)

const maxFactsBody = 4 << 10

func (rt *Router) data(w http.ResponseWriter, r *http.Request) {
	snap := rt.cat.Current()
	if len(snap.Bundles()) == 0 {
		problem(w, r, http.StatusServiceUnavailable, "no bundle deployed yet")
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		problem(w, r, http.StatusUnsupportedMediaType, "send Content-Type: application/json")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxFactsBody))
	if err != nil {
		problem(w, r, http.StatusRequestEntityTooLarge, "facts body over 4 KiB")
		return
	}
	facts, err := domain.ParseFacts(body)
	if err != nil {
		problem(w, r, http.StatusBadRequest, err.Error())
		return
	}
	b, via, err := snap.Decide(facts, time.Now())
	if err != nil {
		rt.count("refused", "") // fail-closed answers stop OTA updates: keep them visible
		problem(w, r, http.StatusBadRequest, err.Error())
		return
	}
	rt.apply(w, r, snap, b, via)
	writeJSON(w, http.StatusOK, struct {
		Bundle string `json:"bundle"`
		Via    string `json:"via"`
	}{b.Version, via})
}

// apply pins a decision in the cookie; a default one clears it, so later default changes apply.
func (rt *Router) apply(w http.ResponseWriter, r *http.Request, snap *domain.Snapshot, b *domain.Bundle, via string) {
	cookie := cookieValue(r, cookieName)
	_, cur := snap.Pick("", cookie)
	switch {
	case via != "default":
		setBundle(w, r, b.Version, decisionAge, cur.Version != b.Version)
	case cookie != "":
		setBundle(w, r, "", -1, cur.Version != b.Version)
	}
	rt.count(via, b.Version)
	if cur.Version != b.Version {
		rt.log.Debug("bundle switch", "from", cur.Version, "to", b.Version, "via", via)
	}
}

func (rt *Router) count(via, bundle string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.byVia[via]++
	if bundle != "" {
		rt.byBundle[bundle]++
	}
	rt.traffic.add(time.Now(), via, bundle)
}

func cookieFacts(r *http.Request) domain.Facts {
	c, err := r.Cookie(factsCookie)
	if err != nil {
		return nil
	}
	s, err := url.PathUnescape(c.Value)
	if err != nil || len(s) > maxFactsBody {
		return nil
	}
	f, err := domain.ParseFacts([]byte(s))
	if err != nil {
		return nil
	}
	return f
}

var (
	started = time.Now()
	build   string // set at release: -ldflags "-X github.com/zenkiet/bundle-pilot/internal/handler.build=1.2.3"
)

// Builds made by hand get the version Go stamps from git.
func init() {
	if bi, ok := debug.ReadBuildInfo(); ok && build == "" {
		build = bi.Main.Version
	}
}

// features lets an app hide what an older gateway lacks.
var features = []string{"check_source", "notes", "issue_events", "token_expiry", "release", "setup_qr"}

func (rt *Router) GetInfo(context.Context, *connect.Request[gatewayv1.GetInfoRequest]) (*connect.Response[gatewayv1.GetInfoResponse], error) {
	snap := rt.cat.Current()
	return connect.NewResponse(&gatewayv1.GetInfoResponse{
		Version:       build,
		SetupRequired: snap.Setup != "",
		PasswordSet:   snap.Config.Auth.Hash != "",
		ProjectName:   snap.Config.Project,
		Environment:   snap.Config.Environment,
		Features:      features,
	}), nil
}

func (rt *Router) GetStatus(context.Context, *connect.Request[gatewayv1.GetStatusRequest]) (*connect.Response[gatewayv1.GetStatusResponse], error) {
	return connect.NewResponse(rt.status()), nil
}

func (rt *Router) status() *gatewayv1.GetStatusResponse {
	snap := rt.cat.Current()
	reloads, lastErr := rt.cat.Stats()
	rt.mu.Lock()
	byVia, byBundle := maps.Clone(rt.byVia), maps.Clone(rt.byBundle)
	traffic := rt.traffic.series(time.Now())
	rt.mu.Unlock()
	out := &gatewayv1.GetStatusResponse{
		Version:           build,
		StartedAt:         timestamppb.New(started),
		ProjectName:       snap.Config.Project,
		Environment:       snap.Config.Environment,
		SetupRequired:     snap.Setup != "",
		PasswordSet:       snap.Config.Auth.Hash != "",
		DefaultBundle:     snap.Default.Version,
		DefaultFrom:       snap.DefaultFrom,
		BasePath:          snap.BasePath,
		Source:            snap.Config.Source.String(),
		Signing:           snap.Signed,
		LoadedAt:          timestamppb.New(snap.LoadedAt),
		Reloads:           reloads,
		LastError:         lastErr,
		Errors:            issues(snap.Errors),
		ResidentBytes:     resident(),
		Decisions:         byVia,
		DecisionsByBundle: byBundle,
		Issues:            issues(snap.Issues),
		Traffic:           traffic,
	}
	if at, objects, syncErr := rt.cat.SyncState(); !at.IsZero() {
		out.Sync = &gatewayv1.Sync{At: timestamppb.New(at), Objects: int64(objects), Error: syncErr}
	}
	uploaders := rt.uploaders()
	for _, b := range snap.Bundles() {
		out.Bundles = append(out.Bundles, &gatewayv1.Bundle{
			Version:    b.Version,
			Files:      int64(b.Len()),
			ZipBytes:   b.ZipBytes,
			ModTime:    timestamppb.New(b.ModTime),
			Native:     b.Native,
			Blocked:    slices.Contains(snap.Config.Blocked, b.Version),
			Served:     snap.Bundle(b.Version) != nil,
			Sha256:     b.SHA256,
			UploadedBy: uploaders[b.Version],
			Release:    release(b.Release),
		})
	}
	return out
}

func release(r domain.Release) *gatewayv1.Release {
	if r == (domain.Release{}) {
		return nil
	}
	out := &gatewayv1.Release{Commit: r.Commit, Branch: r.Branch, Url: r.URL, BuiltBy: r.BuiltBy, Notes: r.Notes}
	if !r.BuiltAt.IsZero() {
		out.BuiltAt = timestamppb.New(r.BuiltAt)
	}
	return out
}

// uploaders is who last uploaded each zip, as far back as the activity feed goes.
func (rt *Router) uploaders() map[string]string {
	by := map[string]string{}
	for _, e := range rt.cat.Events(0, maxEvents) { // oldest first, so the latest upload wins
		if e.Kind == "upload" || e.Kind == "fetch" {
			by[e.Bundle] = e.By
		}
	}
	return by
}

func issues(in []domain.Issue) []*gatewayv1.Issue {
	out := make([]*gatewayv1.Issue, 0, len(in))
	for _, i := range in {
		out = append(out, &gatewayv1.Issue{Code: i.Code, Text: i.Text, Rule: i.Rule, Bundle: i.Bundle, Reason: i.Reason, Token: i.Token})
	}
	return out
}

func (rt *Router) Reload(ctx context.Context, _ *connect.Request[gatewayv1.ReloadRequest]) (*connect.Response[gatewayv1.ReloadResponse], error) {
	start := time.Now()
	if err := rt.cat.Reload(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	rt.cat.Record(domain.Event{Kind: "reload", By: from(ctx).who})
	return connect.NewResponse(&gatewayv1.ReloadResponse{ReloadMs: time.Since(start).Milliseconds()}), nil
}

// resident leaves out the zips, which live in the page cache.
func resident() uint64 {
	s := []metrics.Sample{{Name: "/memory/classes/total:bytes"}, {Name: "/memory/classes/heap/released:bytes"}}
	metrics.Read(s)
	return s[0].Value.Uint64() - s[1].Value.Uint64()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.MarshalWrite(w, v)
}
