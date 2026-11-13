package bundlefs

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/infrastructure/mirror"
)

const (
	eventsFile = "events.jsonl"
	keepEvents = 500
)

type events struct {
	mu   sync.Mutex
	list []domain.Event // oldest first
}

func (s *Source) loadEvents() {
	data, err := s.root.ReadFile(eventsFile)
	if err != nil {
		return
	}
	for line := range bytes.Lines(data) {
		var e domain.Event
		if json.Unmarshal(line, &e) == nil {
			s.events.list = append(s.events.list, e)
		}
	}
	s.events.list = s.events.list[max(0, len(s.events.list)-keepEvents):]
}

// Record's ids are Unix milliseconds raised past the previous id, so they keep growing across
// restarts.
func (s *Source) Record(e domain.Event) {
	s.events.mu.Lock()
	defer s.events.mu.Unlock()
	e.At = time.Now().UTC()
	e.ID = e.At.UnixMilli()
	last := len(s.events.list) - 1
	if last >= 0 {
		e.ID = max(e.ID, s.events.list[last].ID+1)
	}
	s.events.list = append(s.events.list, e)
	s.events.list = s.events.list[max(0, len(s.events.list)-keepEvents):]
	// The whole file is rewritten: a few hundred lines, on an admin action or a bucket sync.
	err := mirror.Replace(s.root, eventsFile, 0o600, func(f *os.File) (time.Time, error) {
		w := bufio.NewWriter(f)
		for _, e := range s.events.list {
			if err := json.MarshalWrite(w, e); err != nil {
				return time.Time{}, err
			}
			_ = w.WriteByte('\n')
		}
		return time.Time{}, w.Flush()
	})
	if err != nil {
		s.log.Warn("event not saved in "+eventsFile, "error", err)
	}
}

func (s *Source) Events(after int64, limit int) []domain.Event {
	s.events.mu.Lock()
	defer s.events.mu.Unlock()
	list := s.events.list
	if after == 0 {
		return slices.Clone(list[max(0, len(list)-limit):])
	}
	first := len(list)
	for first > 0 && list[first-1].ID > after {
		first--
	}
	return slices.Clone(list[first:min(len(list), first+limit)])
}

func (s *Source) synced(kind, zip string) {
	s.Record(domain.Event{Kind: kind, By: "s3", Bundle: strings.TrimSuffix(zip, ".zip")})
}
