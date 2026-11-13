package handler

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
	"github.com/zenkiet/bundle-pilot/internal/gen/gatewayv1"
)

var (
	errNoConfig = errors.New("config is required")
	errStale    = errors.New("config changed since it was read: reload it")
)

func (rt *Router) GetConfig(context.Context, *connect.Request[gatewayv1.GetConfigRequest]) (*connect.Response[gatewayv1.GetConfigResponse], error) {
	pb, err := rt.cat.Config()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&gatewayv1.GetConfigResponse{Config: domain.Redacted(pb), Etag: domain.ETag(pb)}), nil
}

func (rt *Router) CheckConfig(_ context.Context, req *connect.Request[gatewayv1.CheckConfigRequest]) (*connect.Response[gatewayv1.CheckConfigResponse], error) {
	if req.Msg.Config == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errNoConfig)
	}
	out := &gatewayv1.CheckConfigResponse{}
	if snap, err := rt.preview(req.Msg.Config); err != nil {
		out.Error = err.Error()
	} else {
		out.Issues = issues(snap.Issues)
	}
	return connect.NewResponse(out), nil
}

func (rt *Router) UpdateConfig(ctx context.Context, req *connect.Request[gatewayv1.UpdateConfigRequest]) (*connect.Response[gatewayv1.UpdateConfigResponse], error) {
	pb, etag := req.Msg.Config, req.Msg.Etag
	if pb == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errNoConfig)
	}
	c := from(ctx)
	rev, err := rt.cat.UpdateConfig(c.who, func(cur *configv1.Config) (*configv1.Config, error) {
		if etag != "" && etag != domain.ETag(cur) {
			return nil, errStale
		}
		domain.KeepStored(pb, cur)
		return pb, nil // UpdateConfig validates it
	})
	switch {
	case errors.Is(err, errStale):
		return nil, connect.NewError(connect.CodeAborted, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	rt.log.Info("config saved", "by", c.who, "remote", c.ip)
	rt.cat.Record(domain.Event{Kind: "config", By: c.who, Revision: rev})
	return connect.NewResponse(&gatewayv1.UpdateConfigResponse{Etag: domain.ETag(pb), Issues: issues(rt.cat.Current().Issues)}), nil
}

// preview is what saving pb would serve, under the loaded bundles.
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

// Decide answers like /data, but sets no cookie and counts nothing.
func (rt *Router) Decide(_ context.Context, req *connect.Request[gatewayv1.DecideRequest]) (*connect.Response[gatewayv1.DecideResponse], error) {
	raw, err := protojson.Marshal(req.Msg.GetFacts())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	facts, err := domain.ParseFacts(raw)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("facts: "+err.Error()))
	}
	snap := rt.cat.Current()
	if req.Msg.Config != nil {
		if snap, err = rt.preview(req.Msg.Config); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	b, via, why, match, err := snap.Explain(facts, time.Now())
	out := &gatewayv1.DecideResponse{Via: via}
	for _, s := range why {
		out.Why = append(out.Why, &gatewayv1.Skip{
			Rule:    s.Rule,
			Reason:  s.Reason,
			Fact:    s.Fact,
			Value:   s.Value,
			Bucket:  int32(s.Bucket),
			Percent: int32(s.Percent),
		})
	}
	for _, m := range match {
		out.Match = append(out.Match, &gatewayv1.Match{
			Fact:    m.Fact,
			Value:   m.Value,
			Bucket:  int32(m.Bucket),
			Percent: int32(m.Percent),
		})
	}
	if err != nil {
		out.Error = err.Error()
	} else {
		out.Bundle = b.Version
	}
	return connect.NewResponse(out), nil
}
