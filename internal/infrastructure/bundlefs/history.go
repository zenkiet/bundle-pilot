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

// record keeps pb as clients see it, so secrets never land in history, and skips a save they
// cannot see. The first record also keeps old, so a revert can reach the config before history.
func (s *Source) record(old *configv1.Config, oldAt time.Time, pb *configv1.Config, by, note string) (string, error) {
	if old != nil && domain.ETag(old) == domain.ETag(pb) {
		return "", nil
	}
	if err := s.root.MkdirAll(historyDir, 0o700); err != nil {
		return "", err
	}
	ids, err := s.revisions()
	if err == nil && len(ids) == 0 && old != nil {
		if _, err := s.writeRevision(oldAt, "", "", old); err != nil {
			return "", err
		}
	}
	rev, err := s.writeRevision(time.Now(), by, note, pb)
	if err != nil {
		return "", err
	}
	ids, err = s.revisions()
	for _, id := range ids[:max(0, len(ids)-keepRevisions)] { // oldest first
		_ = s.root.Remove(path.Join(historyDir, id+".pb"))
	}
	return rev, err
}

func (s *Source) writeRevision(at time.Time, by, note string, pb *configv1.Config) (string, error) {
	cfg, err := domain.EncodeConfig(domain.Redacted(pb))
	if err != nil {
		return "", err
	}
	b, err := proto.Marshal(&configv1.Revision{By: by, Note: note, Config: cfg})
	if err != nil {
		return "", err
	}
	id := at.UTC().Format(revisionID)
	return id, s.root.WriteFile(path.Join(historyDir, id+".pb"), b, 0o600)
}

// revisions are oldest first.
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

func (s *Source) History() ([]domain.Revision, error) {
	ids, err := s.revisions()
	out := make([]domain.Revision, 0, len(ids))
	for _, id := range slices.Backward(ids) {
		rev, err := s.readRevision(id)
		if err != nil {
			continue
		}
		cfg, err := domain.DecodeConfig(rev.Config, false)
		if err != nil {
			continue
		}
		at, _ := time.Parse(revisionID, id)
		out = append(out, domain.Revision{ID: id, At: at, By: rev.By, Note: rev.Note, Config: cfg})
	}
	return out, err
}

// Revision returns fs.ErrNotExist for an unknown id.
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
