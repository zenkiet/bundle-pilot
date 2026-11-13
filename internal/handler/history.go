package handler

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zenkiet/bundle-pilot/internal/gen/gatewayv1"
)

func (rt *Router) ListHistory(_ context.Context, req *connect.Request[gatewayv1.ListHistoryRequest]) (*connect.Response[gatewayv1.ListHistoryResponse], error) {
	revs, err := rt.cat.History()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	limit := int(req.Msg.Limit)
	if limit > 0 && limit < len(revs) {
		revs = revs[:limit] // newest first
	}
	out := &gatewayv1.ListHistoryResponse{}
	for _, r := range revs {
		rev := &gatewayv1.Revision{Id: r.ID, At: timestamppb.New(r.At), By: r.By}
		if req.Msg.Configs {
			rev.Config = r.Config
		}
		out.Revisions = append(out.Revisions, rev)
	}
	return connect.NewResponse(out), nil
}

func (rt *Router) GetRevision(_ context.Context, req *connect.Request[gatewayv1.GetRevisionRequest]) (*connect.Response[gatewayv1.GetRevisionResponse], error) {
	pb, err := rt.cat.Revision(req.Msg.Id)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no revision %q", req.Msg.Id))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&gatewayv1.GetRevisionResponse{Config: pb}), nil
}
