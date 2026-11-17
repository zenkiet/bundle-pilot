package bundlefs

import (
	"encoding/json/v2"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/zenkiet/bundle-pilot/internal/infrastructure/mirror"
)

const usedFile = "tokens-used.json"

// tokenUse is kept outside config.pb, so using a token never changes its etag or history.
type tokenUse struct {
	mu sync.Mutex
	at map[string]time.Time
}

func (s *Source) loadTokenUse() {
	s.used.at = map[string]time.Time{}
	data, err := s.root.ReadFile(usedFile)
	if err != nil {
		return
	}
	var at map[string]time.Time
	if json.Unmarshal(data, &at) == nil && at != nil {
		s.used.at = at
	}
}

func (s *Source) TokenUsed(name string, now time.Time) {
	hour := now.UTC().Truncate(time.Hour)
	s.used.mu.Lock()
	defer s.used.mu.Unlock()
	if s.used.at[name].Equal(hour) {
		return
	}
	s.used.at[name] = hour
	err := mirror.Replace(s.root, usedFile, 0o600, func(f *os.File) (time.Time, error) {
		return time.Time{}, json.MarshalWrite(f, s.used.at)
	})
	if err != nil {
		s.log.Warn("token use not saved in "+usedFile, "error", err)
	}
}

func (s *Source) TokensUsed() map[string]time.Time {
	s.used.mu.Lock()
	defer s.used.mu.Unlock()
	return maps.Clone(s.used.at)
}
