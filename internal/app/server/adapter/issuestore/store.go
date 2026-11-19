// Package issuestore は見つかった Issue をメモリに持ち、ディスクに追記で保存する
package issuestore

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const dayLayout = "2006-01-02"

// Store は dir/<日付 (UTC)>.jsonl に、見つかった Issue を 1 行ずつ追記する。dir が空ならメモリにだけ持つ。
// 保存先を S3 などに替えるときは、この Store を domain.IssueStore の別の実装に差し替える
type Store struct {
	mu  sync.Mutex
	dir string
	// byFingerprint は Issue ごとの、見つかった順の一覧
	byFingerprint map[string][]domain.IssueOccurrence
	pending       []domain.IssueOccurrence
}

func NewStore(dir string, now time.Time) (*Store, error) {
	s := &Store{dir: dir, byFingerprint: map[string][]domain.IssueOccurrence{}}
	if dir == "" {
		return s, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, model.ErrInternal.Wrap(err, "create the issues directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, model.ErrInternal.Wrap(err, "read the issues directory")
	}
	oldest := now.Add(-domain.IssueRetention).UTC().Format(dayLayout)
	for _, e := range entries {
		day, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if !ok || day < oldest {
			continue
		}
		if err := s.loadFile(filepath.Join(dir, e.Name())); err != nil {
			return nil, err
		}
	}
	for fp, occs := range s.byFingerprint {
		slices.SortFunc(occs, func(a, b domain.IssueOccurrence) int { return a.SeenAt.Compare(b.SeenAt) })
		s.byFingerprint[fp] = occs
	}
	return s, nil
}

func (s *Store) loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return model.ErrInternal.Wrap(err, "open "+path)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		var o domain.IssueOccurrence
		// 書いている途中で止まった最後の行は読めないので飛ばす
		if err := json.Unmarshal(sc.Bytes(), &o); err != nil {
			continue
		}
		s.byFingerprint[o.Fingerprint] = append(s.byFingerprint[o.Fingerprint], o)
	}
	if err := sc.Err(); err != nil {
		return model.ErrInternal.Wrap(err, "read "+path)
	}
	return nil
}

func (s *Store) Add(occurrences ...domain.IssueOccurrence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range occurrences {
		s.byFingerprint[o.Fingerprint] = append(s.byFingerprint[o.Fingerprint], o)
		if s.dir != "" {
			s.pending = append(s.pending, o)
		}
	}
}

func (s *Store) Range(from, to time.Time) []domain.IssueOccurrence {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.IssueOccurrence
	for _, occs := range s.byFingerprint {
		for _, o := range occs {
			if !o.SeenAt.Before(from) && !o.SeenAt.After(to) {
				out = append(out, o)
			}
		}
	}
	slices.SortFunc(out, func(a, b domain.IssueOccurrence) int { return a.SeenAt.Compare(b.SeenAt) })
	return out
}

func (s *Store) Last(fingerprint string) (domain.IssueOccurrence, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	occs := s.byFingerprint[fingerprint]
	if len(occs) == 0 {
		return domain.IssueOccurrence{}, false
	}
	return occs[len(occs)-1], true
}

func (s *Store) FirstSeen(fingerprint string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	occs := s.byFingerprint[fingerprint]
	if len(occs) == 0 {
		return time.Time{}, false
	}
	return occs[0].SeenAt, true
}

func (s *Store) Flush(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return errors.Join(s.flush(), s.prune(now))
}

func (s *Store) flush() error {
	byDay := map[string][]domain.IssueOccurrence{}
	for _, o := range s.pending {
		day := o.SeenAt.UTC().Format(dayLayout)
		byDay[day] = append(byDay[day], o)
	}
	s.pending = nil
	for day, occs := range byDay {
		if err := appendLines(filepath.Join(s.dir, day+".jsonl"), occs); err != nil {
			return err
		}
	}
	return nil
}

func appendLines(path string, occs []domain.IssueOccurrence) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return model.ErrInternal.Wrap(err, "open "+path)
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, o := range occs {
		if err := enc.Encode(o); err != nil {
			f.Close()
			return model.ErrInternal.Wrap(err, "encode an issue")
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return model.ErrInternal.Wrap(err, "write "+path)
	}
	return f.Close()
}

func (s *Store) prune(now time.Time) error {
	oldest := now.Add(-domain.IssueRetention)
	for fp, occs := range s.byFingerprint {
		i := 0
		for i < len(occs) && occs[i].SeenAt.Before(oldest) {
			i++
		}
		if i == len(occs) {
			delete(s.byFingerprint, fp)
		} else if i > 0 {
			s.byFingerprint[fp] = occs[i:]
		}
	}
	if s.dir == "" {
		return nil
	}
	oldestDay := oldest.UTC().Format(dayLayout)
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return model.ErrInternal.Wrap(err, "read the issues directory")
	}
	for _, e := range entries {
		day, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if !ok || day >= oldestDay {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return model.ErrInternal.Wrap(err, "remove an expired issues file")
		}
	}
	return nil
}
