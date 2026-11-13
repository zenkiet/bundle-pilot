package handler

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/gatewayv1"
)

const (
	maxEvents  = 500              // all the feed keeps
	watchEvery = 10 * time.Second // the longest a watch goes without a message
)

func (rt *Router) ListEvents(_ context.Context, req *connect.Request[gatewayv1.ListEventsRequest]) (*connect.Response[gatewayv1.ListEventsResponse], error) {
	limit := int(req.Msg.Limit)
	if limit <= 0 {
		limit = 100
	}
	out := &gatewayv1.ListEventsResponse{}
	for _, e := range rt.cat.Events(req.Msg.After, min(limit, maxEvents)) {
		out.Events = append(out.Events, event(e))
	}
	return connect.NewResponse(out), nil
}

// Watch checks memory once a second and authorizes again on each new config, so a password
// change or a revoked token ends it.
func (rt *Router) Watch(ctx context.Context, req *connect.Request[gatewayv1.WatchRequest], stream *connect.ServerStream[gatewayv1.WatchResponse]) error {
	c := from(ctx)
	after, limit := req.Msg.After, 100
	var seen *domain.Snapshot
	var sent time.Time
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if !c.exp.IsZero() && !time.Now().Before(c.exp) {
			return connect.NewError(connect.CodeUnauthenticated, errors.New("login expired: refresh it and watch again"))
		}
		snap := rt.cat.Current()
		if seen != nil && snap != seen {
			if _, err := rt.authorize(req.Header(), c.ip, "read"); err != nil {
				return err
			}
		}
		events := rt.cat.Events(after, limit)
		if len(events) > 0 || snap != seen || time.Since(sent) >= watchEvery {
			msg := &gatewayv1.WatchResponse{Status: rt.status()}
			for _, e := range events {
				msg.Events = append(msg.Events, event(e))
			}
			if err := stream.Send(msg); err != nil {
				return err
			}
			if len(events) > 0 {
				after = events[len(events)-1].ID
			}
			seen, sent, limit = snap, time.Now(), maxEvents
		}
		select {
		case <-ctx.Done():
			return nil
		case <-rt.stop:
			return nil
		case <-tick.C:
		}
	}
}

func event(e domain.Event) *gatewayv1.Event {
	return &gatewayv1.Event{
		Id:       e.ID,
		At:       timestamppb.New(e.At),
		Kind:     e.Kind,
		By:       e.By,
		Bundle:   e.Bundle,
		Token:    e.Token,
		Revision: e.Revision,
	}
}
