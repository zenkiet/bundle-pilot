package handler

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/gatewayv1"
	"github.com/zenkiet/bundle-pilot/internal/gen/gatewayv1/gatewayv1connect"
)

type caller struct {
	who, scope string
	ip         netip.Addr
	exp        time.Time // zero: never expires
}

type callerKey struct{}

func withCaller(ctx context.Context, c caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

func from(ctx context.Context) caller {
	c, _ := ctx.Value(callerKey{}).(caller)
	return c
}

// needs is each call's scope, "" for none; a call missing here takes admin.
var needs = map[string]string{
	gatewayv1connect.GatewayServiceGetInfoProcedure:        "",
	gatewayv1connect.GatewayServiceLoginProcedure:          "",
	gatewayv1connect.GatewayServiceRefreshSessionProcedure: "", // it checks its token itself
	gatewayv1connect.GatewayServiceWhoAmIProcedure:         "read",
	gatewayv1connect.GatewayServiceGetStatusProcedure:      "read",
	gatewayv1connect.GatewayServiceWatchProcedure:          "read",
	gatewayv1connect.GatewayServiceListEventsProcedure:     "read",
	gatewayv1connect.GatewayServiceGetConfigProcedure:      "read",
	gatewayv1connect.GatewayServiceCheckConfigProcedure:    "read",
	gatewayv1connect.GatewayServiceDecideProcedure:         "read",
	gatewayv1connect.GatewayServiceListHistoryProcedure:    "read",
	gatewayv1connect.GatewayServiceGetRevisionProcedure:    "read",
}

// authorize keeps the API open until a password is set; during first-run setup, writes also need
// the setup token. Credentials are headers the client sets, never cookies, so another site's page
// cannot borrow them.
func (rt *Router) authorize(h http.Header, ip netip.Addr, need string) (caller, *connect.Error) {
	snap := rt.cat.Current()
	a := snap.Config.Auth
	if a.Hash == "" {
		if snap.Setup == "" {
			return caller{who: "anonymous", scope: "admin", ip: ip}, nil
		}
		tokenOK := subtle.ConstantTimeCompare([]byte(h.Get("X-Setup-Token")), []byte(snap.Setup)) == 1
		if need != "read" && !tokenOK {
			return caller{}, connect.NewError(connect.CodePermissionDenied,
				errors.New("setup token required: the gateway printed it to its log"))
		}
		scope := "read" // so WhoAmI tells a setup wizard whether its token is right
		if tokenOK {
			scope = "admin"
		}
		return caller{who: "setup", scope: scope, ip: ip}, nil
	}

	secret, ok := strings.CutPrefix(h.Get("Authorization"), "Bearer ")
	if !ok {
		return caller{}, connect.NewError(connect.CodeUnauthenticated, errors.New("sign in required"))
	}
	if strings.HasPrefix(secret, "v4.local.") {
		exp, ok := a.SessionExpiry(secret)
		if !ok {
			return caller{}, connect.NewError(connect.CodeUnauthenticated, errors.New("login expired or ended: sign in again"))
		}
		return caller{who: a.Username, scope: "admin", ip: ip, exp: exp}, nil
	}
	t, found := snap.Config.Token(secret)
	now := time.Now()
	switch {
	case !found:
		rt.log.Warn("unknown token", "remote", ip)
		return caller{}, connect.NewError(connect.CodeUnauthenticated, errors.New("unknown token"))
	case t.Expired(now):
		return caller{}, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("token %s expired", t.Name))
	case !domain.Allows(t.Scope, need):
		return caller{}, connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("token %s has scope %s, this needs %s", t.Name, t.Scope, need))
	}
	rt.cat.TokenUsed(t.Name, now)
	return caller{who: "token:" + t.Name, scope: t.Scope, ip: ip, exp: t.Expires}, nil
}

type authInterceptor struct{ rt *Router }

func (i authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, err := i.rt.authorizeCall(ctx, req.Spec(), req.Peer(), req.Header())
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (i authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := i.rt.authorizeCall(ctx, conn.Spec(), conn.Peer(), conn.RequestHeader())
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

func (authInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (rt *Router) authorizeCall(ctx context.Context, spec connect.Spec, peer connect.Peer, h http.Header) (context.Context, error) {
	ip := clientIP(peer.Addr, h)
	need, listed := needs[spec.Procedure]
	if !listed {
		need = "admin"
	}
	if need == "" {
		return withCaller(ctx, caller{ip: ip}), nil
	}
	c, err := rt.authorize(h, ip, need)
	if err != nil {
		return ctx, err
	}
	return withCaller(ctx, c), nil
}

func (rt *Router) auth(need string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := rt.authorize(r.Header, clientIP(r.RemoteAddr, r.Header), need)
		if err != nil {
			status := http.StatusForbidden
			if err.Code() == connect.CodeUnauthenticated {
				status = http.StatusUnauthorized
			}
			problem(w, r, status, err.Message())
			return
		}
		next(w, r.WithContext(withCaller(r.Context(), c)))
	}
}

// verifying caps concurrent PBKDF2 checks so a flood of logins cannot starve serving.
var verifying = make(chan struct{}, 2)

func (rt *Router) Login(ctx context.Context, req *connect.Request[gatewayv1.LoginRequest]) (*connect.Response[gatewayv1.LoginResponse], error) {
	a := rt.cat.Current().Config.Auth
	if a.Hash == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("no admin password yet: finish setup first"))
	}
	ip := from(ctx).ip
	key := clientKey(ip)
	if d := rt.logins.wait(key, time.Now()); d > 0 {
		err := connect.NewError(connect.CodeResourceExhausted, errors.New("too many wrong passwords from this address: retry later"))
		err.Meta().Set("Retry-After", strconv.Itoa(int(d.Seconds())+1))
		return nil, err
	}
	select {
	case verifying <- struct{}{}:
	default:
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("too many logins at once, retry shortly"))
	}
	good := domain.VerifyPassword(a.Hash, req.Msg.Password) // checked even for a wrong username, so timing does not reveal it
	<-verifying
	if !good || subtle.ConstantTimeCompare([]byte(req.Msg.Username), []byte(a.Username)) != 1 {
		rt.logins.fail(key, time.Now())
		rt.log.Warn("login failed", "remote", ip)
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("wrong username or password"))
	}
	rt.logins.forget(key)
	token, exp := a.Session(time.Now())
	rt.log.Info("login", "user", a.Username, "remote", ip)
	return connect.NewResponse(&gatewayv1.LoginResponse{Token: token, ExpiresAt: timestamppb.New(exp)}), nil
}

func (rt *Router) RefreshSession(_ context.Context, req *connect.Request[gatewayv1.RefreshSessionRequest]) (*connect.Response[gatewayv1.RefreshSessionResponse], error) {
	secret, _ := strings.CutPrefix(req.Header().Get("Authorization"), "Bearer ")
	token, exp, err := rt.cat.Current().Config.Auth.Refresh(secret, time.Now())
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	return connect.NewResponse(&gatewayv1.RefreshSessionResponse{Token: token, ExpiresAt: timestamppb.New(exp)}), nil
}

func (rt *Router) WhoAmI(ctx context.Context, _ *connect.Request[gatewayv1.WhoAmIRequest]) (*connect.Response[gatewayv1.WhoAmIResponse], error) {
	c := from(ctx)
	out := &gatewayv1.WhoAmIResponse{By: c.who, Scope: c.scope}
	if !c.exp.IsZero() {
		out.ExpiresAt = timestamppb.New(c.exp)
	}
	return connect.NewResponse(out), nil
}

const (
	loginBurst = 5
	loginEvery = time.Minute
)

// logins holds a limiter per address with recent wrong passwords. One is added only after a
// PBKDF2 check, which verifying caps, so the map stays small however many addresses try.
type logins struct {
	mu sync.Mutex
	m  map[netip.Prefix]*rate.Limiter
}

func (l *logins) wait(key netip.Prefix, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim := l.m[key]
	if lim == nil {
		return 0
	}
	r := lim.ReserveN(now, 1)
	defer r.CancelAt(now) // only looking: the try is spent in fail
	return r.DelayFrom(now)
}

// fail spends one of key's tries. Full limiters are dropped first: a new one allows as much.
func (l *logins) fail(key netip.Prefix, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	maps.DeleteFunc(l.m, func(_ netip.Prefix, lim *rate.Limiter) bool { return lim.TokensAt(now) >= loginBurst })
	lim := l.m[key]
	if lim == nil {
		lim = rate.NewLimiter(rate.Every(loginEvery), loginBurst)
		l.m[key] = lim
	}
	lim.AllowN(now, 1)
}

func (l *logins) forget(key netip.Prefix) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}

// clientKey is an IPv4 address or an IPv6 /64, what one client usually gets.
func clientKey(ip netip.Addr) netip.Prefix {
	p, _ := ip.Prefix(min(ip.BitLen(), 64))
	return p
}

// clientIP trusts X-Forwarded-For only from a private or loopback peer, such as Traefik in
// Docker, and takes its last entry, the one that proxy added.
func clientIP(peer string, h http.Header) netip.Addr {
	addr, _ := netip.ParseAddrPort(peer)
	ip := addr.Addr().Unmap()
	fwd := h.Values("X-Forwarded-For")
	viaProxy := ip.IsLoopback() || ip.IsPrivate()
	if len(fwd) == 0 || !viaProxy {
		return ip
	}
	hops := strings.Split(fwd[len(fwd)-1], ",")
	if last, err := netip.ParseAddr(strings.TrimSpace(hops[len(hops)-1])); err == nil {
		return last.Unmap()
	}
	return ip
}
