package handler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
	"github.com/zenkiet/bundle-pilot/internal/gen/gatewayv1"
)

var errNoToken = errors.New("no such token")

func (rt *Router) ListTokens(context.Context, *connect.Request[gatewayv1.ListTokensRequest]) (*connect.Response[gatewayv1.ListTokensResponse], error) {
	out := &gatewayv1.ListTokensResponse{}
	for _, t := range rt.cat.Current().Config.Tokens {
		tok := &gatewayv1.Token{Name: t.Name, Scope: t.Scope}
		if !t.Created.IsZero() { // a token written into config.pb by hand may have no date
			tok.Created = timestamppb.New(t.Created)
		}
		out.Tokens = append(out.Tokens, tok)
	}
	return connect.NewResponse(out), nil
}

// CreateToken needs a password first: a token saved during setup would end setup with the API
// still open.
func (rt *Router) CreateToken(ctx context.Context, req *connect.Request[gatewayv1.CreateTokenRequest]) (*connect.Response[gatewayv1.CreateTokenResponse], error) {
	if rt.cat.Current().Config.Auth.Hash == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("set an admin password first: until then the admin API is open to anyone"))
	}
	c := from(ctx)
	secret, hash := domain.NewToken()
	token := &configv1.Token{
		Name:    req.Msg.Name,
		Scope:   req.Msg.Scope,
		Sha256:  hash,
		Created: time.Now().UTC().Format(time.RFC3339),
	}
	_, err := rt.cat.UpdateConfig(c.who, func(cur *configv1.Config) (*configv1.Config, error) {
		cur.Tokens = append(cur.Tokens, token)
		return cur, nil
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	rt.log.Info("token created", "name", token.Name, "scope", token.Scope, "by", c.who, "remote", c.ip)
	rt.cat.Record(domain.Event{Kind: "token_create", By: c.who, Token: token.Name})
	return connect.NewResponse(&gatewayv1.CreateTokenResponse{Token: secret}), nil
}

func (rt *Router) DeleteToken(ctx context.Context, req *connect.Request[gatewayv1.DeleteTokenRequest]) (*connect.Response[gatewayv1.DeleteTokenResponse], error) {
	name, c := req.Msg.Name, from(ctx)
	_, err := rt.cat.UpdateConfig(c.who, func(cur *configv1.Config) (*configv1.Config, error) {
		before := len(cur.Tokens)
		cur.Tokens = slices.DeleteFunc(cur.Tokens, func(t *configv1.Token) bool { return t.Name == name })
		if len(cur.Tokens) == before {
			return nil, errNoToken
		}
		return cur, nil
	})
	switch {
	case errors.Is(err, errNoToken):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no token %q", name))
	case err != nil:
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	rt.log.Info("token revoked", "name", name, "by", c.who, "remote", c.ip)
	rt.cat.Record(domain.Event{Kind: "token_delete", By: c.who, Token: name})
	return connect.NewResponse(&gatewayv1.DeleteTokenResponse{}), nil
}
