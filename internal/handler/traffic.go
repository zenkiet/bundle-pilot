package handler

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/zenkiet/bundle-pilot/internal/gen/gatewayv1"
)

// traffic counts decisions per minute for the last hour, minute m in slot m%60. Both maps share
// one minute, so they cover the same hour. Router.mu guards it.
type traffic struct {
	minute   int64 // in Unix minutes
	byBundle map[string]*[60]uint64
	byVia    map[string]*[60]uint64
}

// add counts one decision; bundle is "" for a refused one.
func (t *traffic) add(now time.Time, via, bundle string) {
	t.roll(now)
	slot := t.minute % 60
	ring(t.byVia, via)[slot]++
	if bundle != "" {
		ring(t.byBundle, bundle)[slot]++
	}
}

func ring(rings map[string]*[60]uint64, key string) *[60]uint64 {
	c := rings[key]
	if c == nil {
		c = new([60]uint64)
		rings[key] = c
	}
	return c
}

// roll clears the minutes since the last call. A clock that went back keeps counting in the
// current minute.
func (t *traffic) roll(now time.Time) {
	m := now.Unix() / 60
	if m <= t.minute {
		return
	}
	for _, rings := range []map[string]*[60]uint64{t.byBundle, t.byVia} {
		for key, c := range rings {
			for i := t.minute + 1; i <= min(m, t.minute+60); i++ {
				c[i%60] = 0
			}
			if *c == [60]uint64{} {
				delete(rings, key)
			}
		}
	}
	t.minute = m
}

func (t *traffic) series(now time.Time) *gatewayv1.Traffic {
	t.roll(now)
	return &gatewayv1.Traffic{
		Start:    timestamppb.New(time.Unix((t.minute-59)*60, 0)),
		ByBundle: t.perMinute(t.byBundle),
		ByVia:    t.perMinute(t.byVia),
	}
}

func (t *traffic) perMinute(rings map[string]*[60]uint64) map[string]*gatewayv1.Counts {
	out := make(map[string]*gatewayv1.Counts, len(rings))
	for key, c := range rings {
		perMinute := make([]uint64, 60)
		for i := range perMinute {
			perMinute[i] = c[(t.minute+1+int64(i))%60] // minute t.minute-59+i
		}
		out[key] = &gatewayv1.Counts{PerMinute: perMinute}
	}
	return out
}
