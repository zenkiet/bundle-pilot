package handler

import (
	"context"
	"crypto/subtle"
	"maps"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/domain"
)

// session is the last Basic header that passed, and the password hash it was checked against.
type session struct{ hash, header string }

// caller is who sent a request and what they may do, kept in its context.
type caller struct{ who, scope string }

type callerKey struct{}

// verifying caps concurrent PBKDF2 checks so failed sign-ins cannot starve serving.
var verifying = make(chan struct{}, 2)

// Sign-in backoff: the first failFree wrong passwords in a row are free. After that an address
// waits 1 s before its next try is even checked, doubling each time up to failMax. Failures
// older than failMax are forgotten.
const (
	failFree = 5
	failMax  = 15 * time.Minute
)

type failure struct {
	n    int
	last time.Time
}

type failures struct {
	mu sync.Mutex
	m  map[netip.Prefix]failure
}

// wait is how long key must still wait before its next sign-in is checked.
func (f *failures) wait(key netip.Prefix, now time.Time) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.m[key]
	if e.n < failFree {
		return 0
	}
	doublings := min(e.n-failFree, 10) // 2^10 s is already past failMax; the cap keeps the shift small
	delay := min(time.Second<<doublings, failMax)
	return e.last.Add(delay).Sub(now)
}

// add counts a wrong password from key.
func (f *failures) add(key netip.Prefix, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.m) >= 10_000 {
		maps.DeleteFunc(f.m, func(_ netip.Prefix, e failure) bool { return now.Sub(e.last) > failMax })
		if len(f.m) >= 10_000 {
			clear(f.m) // ponytail: a flood of addresses resets every count; a cap per subnet if that ever matters
		}
	}
	e := f.m[key]
	if now.Sub(e.last) > failMax {
		e.n = 0
	}
	f.m[key] = failure{n: e.n + 1, last: now}
}

func (f *failures) forget(key netip.Prefix) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.m, key)
}

// auth runs next only for a request allowed to do what scope need requires:
//   - Until an admin password is set, every request is admin. During first-run setup (no
//     config.pb yet), writes also need the setup token printed to the log.
//   - After that, a request needs a Bearer token with enough scope, or the admin's HTTP Basic
//     credentials. The last Basic header that passed is remembered, so UI polls skip PBKDF2.
//
// Writes sent from another site's page are refused, whatever credentials the browser adds.
func (rt *Router) auth(need string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := checkOrigin(r); err != nil {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		snap := rt.cat.Current()
		a := snap.Config.Auth

		if a.Hash == "" {
			who := "anonymous"
			if snap.Setup != "" {
				dryRun := r.Pattern == "PUT /__gateway/config" && r.Header.Get("X-Dry-Run") != ""
				tokenOK := subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Setup-Token")), []byte(snap.Setup)) == 1
				if r.Method != http.MethodGet && !dryRun && !tokenOK {
					http.Error(w, "setup token required: the gateway printed it to its log", http.StatusForbidden)
					return
				}
				who = "setup"
			}
			next(w, as(r, who, "admin"))
			return
		}

		h := r.Header.Get("Authorization")
		if secret, ok := strings.CutPrefix(h, "Bearer "); ok {
			t, found := snap.Config.Token(secret)
			switch {
			case !found:
				rt.log.Warn("unknown token", "remote", clientIP(r))
				http.Error(w, "unknown token", http.StatusUnauthorized)
			case !domain.Allows(t.Scope, need):
				http.Error(w, "token "+t.Name+" has scope "+t.Scope+", this needs "+need, http.StatusForbidden)
			default:
				next(w, as(r, "token:"+t.Name, t.Scope))
			}
			return
		}

		if rt.remembered(a.Hash, h) {
			next(w, as(r, a.Username, "admin"))
			return
		}
		user, pw, ok := r.BasicAuth()
		if !ok {
			http.Error(w, "sign in required", http.StatusUnauthorized)
			return
		}
		key := clientKey(clientIP(r))
		if d := rt.failures.wait(key, time.Now()); d > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(d.Seconds())+1))
			http.Error(w, "too many wrong passwords from this address: retry later", http.StatusTooManyRequests)
			return
		}
		select {
		case verifying <- struct{}{}:
		default:
			http.Error(w, "too many sign-in attempts, retry shortly", http.StatusTooManyRequests)
			return
		}
		good := domain.VerifyPassword(a.Hash, pw) // checked even for a wrong username, so timing does not reveal it
		<-verifying
		if good && subtle.ConstantTimeCompare([]byte(user), []byte(a.Username)) == 1 {
			rt.failures.forget(key)
			rt.session.Store(&session{hash: a.Hash, header: h})
			next(w, as(r, a.Username, "admin"))
			return
		}
		rt.failures.add(key, time.Now())
		rt.log.Warn("sign-in failed", "remote", key)
		http.Error(w, "sign in required", http.StatusUnauthorized)
	}
}

// remembered reports whether header is the Basic header that last passed for this password hash.
func (rt *Router) remembered(hash, header string) bool {
	s := rt.session.Load()
	return s != nil && s.hash == hash && subtle.ConstantTimeCompare([]byte(s.header), []byte(header)) == 1
}

// as records who sent r and their scope.
func as(r *http.Request, who, scope string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), callerKey{}, caller{who, scope}))
}

// actor is who sent r, for audit logs: the admin's username, "token:<name>", "setup" during
// first run, or "anonymous" while no password is set.
func actor(r *http.Request) string {
	c, _ := r.Context().Value(callerKey{}).(caller)
	return c.who
}

// scope is what the sender of r may do: read, upload or admin.
func scope(r *http.Request) string {
	c, _ := r.Context().Value(callerKey{}).(caller)
	return c.scope
}

// clientKey is what sign-in failures are counted by: one IPv4 address, or one IPv6 /64, which
// is what a single client usually gets.
func clientKey(ip netip.Addr) netip.Prefix {
	bits := 32
	if ip.Is6() {
		bits = 64
	}
	p, _ := ip.Prefix(bits)
	return p
}

// clientIP is the visitor's address. Behind a proxy on a private network, such as Traefik in
// Docker, that is the last X-Forwarded-For entry, the one the proxy added; otherwise it is the
// peer address.
func clientIP(r *http.Request) netip.Addr {
	peer, _ := netip.ParseAddrPort(r.RemoteAddr)
	ip := peer.Addr().Unmap()
	fwd := r.Header.Values("X-Forwarded-For")
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
