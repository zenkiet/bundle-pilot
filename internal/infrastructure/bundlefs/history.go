package bundlefs

import (
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/zenkiet/bundle-pilot/internal/domain"
	"github.com/zenkiet/bundle-pilot/internal/gen/configv1"
)

const (
	historyDir    = "history"
	keepRevisions = 100
	revisionID    = "20060102T150405.000000000Z" // a save's time, which file names sort by
)

// record keeps pb in DIST/history as clients see it, so secrets and tokens never land there.
// A save that changes nothing clients see (a token, a block, a password) adds no revision.
// The first record also keeps old, so a revert can reach the config from before history began.
func (s *Source) record(old *configv1.Config, oldAt time.Time, pb *configv1.Config, by string) error {
	if old != nil && domain.ETag(old) == domain.ETag(pb) {
		return nil
	}
	if err := s.root.MkdirAll(historyDir, 0o700); err != nil {
		return err
	}
	ids, err := s.revisions()
	if err == nil && len(ids) == 0 && old != nil {
		if err := s.writeRevision(oldAt, "", old); err != nil {
			return err
		}
	}
	if err := s.writeRevision(time.Now(), by, pb); err != nil {
		return err
	}
	ids, err = s.revisions()
	for _, id := range ids[:max(0, len(ids)-keepRevisions)] { // oldest first
		_ = s.root.Remove(path.Join(historyDir, id+".pb"))
	}
	return err
}

func (s *Source) writeRevision(at time.Time, by string, pb *configv1.Config) error {
	cfg, err := domain.EncodeConfig(domain.Redacted(pb))
	if err != nil {
		return err
	}
	b, err := proto.Marshal(&configv1.Revision{By: by, Config: cfg})
	if err != nil {
		return err
	}
	return s.root.WriteFile(path.Join(historyDir, at.UTC().Format(revisionID)+".pb"), b, 0o600)
}

// revisions lists the ids in DIST/history, oldest first.
func (s *Source) revisions() ([]string, error) {
	entries, err := fs.ReadDir(s.root.FS(), historyDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var ids []string
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".pb"); ok && validRevision(id) {
			ids = append(ids, id)
		}
	}
	return ids, err
}

// History lists the saved configs, newest first.
func (s *Source) History() ([]domain.Revision, error) {
	ids, err := s.revisions()
	out := make([]domain.Revision, 0, len(ids))
	for _, id := range slices.Backward(ids) {
		if rev, err := s.readRevision(id); err == nil {
			at, _ := time.Parse(revisionID, id)
			out = append(out, domain.Revision{ID: id, At: at, By: rev.By})
		}
	}
	return out, err
}

// Revision returns one saved config, as clients see it; fs.ErrNotExist for an unknown id.
func (s *Source) Revision(id string) (*configv1.Config, error) {
	rev, err := s.readRevision(id)
	if err != nil {
		return nil, err
	}
	return domain.DecodeConfig(rev.Config, false)
}

func (s *Source) readRevision(id string) (*configv1.Revision, error) {
	if !validRevision(id) {
		return nil, fs.ErrNotExist
	}
	b, err := s.root.ReadFile(path.Join(historyDir, id+".pb"))
	if err != nil {
		return nil, err
	}
	rev := &configv1.Revision{}
	return rev, proto.Unmarshal(b, rev)
}

func validRevision(id string) bool {
	_, err := time.Parse(revisionID, id)
	return err == nil
}
