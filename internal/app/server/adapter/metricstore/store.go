// Package metricstore は集計の時系列を、細かさの違う 2 段の区切りでメモリに持ち、ディスクに追記で保存する
package metricstore

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

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const dayLayout = "2006-01-02"

// tierSpecs は区切りの細かさと保持期間。細かい区切りは 1 日強 (1d の range を描ける分)、粗い区切りは 30 日強持つ
var tierSpecs = []struct {
	step      time.Duration
	retention time.Duration
}{
	{domain.MetricStep, 26 * time.Hour},
	{5 * time.Minute, 31 * 24 * time.Hour},
}

type tier struct {
	step      time.Duration
	retention time.Duration
	dir       string
	points    map[int64]*domain.MetricPoint
	// pending はまだディスクに書いていない差分。閉じた区切りの分だけを Flush で書く。
	// 書いたあとの区切りに遅れて届いたものは、もう一度差分として書き、読み直すときに Merge する
	pending map[int64]*domain.MetricPoint
}

// Store は dir/<区切りの長さ>/<日付 (UTC)>.jsonl に、区切りの差分を 1 行ずつ追記する。dir が空ならメモリにだけ持つ。
// 保存先を S3 などに替えるときは、この Store を domain.MetricStore の別の実装に差し替える
type Store struct {
	mu    sync.Mutex
	tiers []*tier
	// since は持っているいちばん古い集計の時刻。粗い区切りは時刻を切り捨てて持つので、区切りからではなく足した時刻から取る
	since time.Time
}

func NewStore(dir string, now time.Time) (*Store, error) {
	s := &Store{}
	for _, spec := range tierSpecs {
		t := &tier{step: spec.step, retention: spec.retention, points: map[int64]*domain.MetricPoint{}, pending: map[int64]*domain.MetricPoint{}}
		if dir != "" {
			t.dir = filepath.Join(dir, spec.step.String())
			if err := os.MkdirAll(t.dir, 0o755); err != nil {
				return nil, model.ErrInternal.Wrap(err, "create the metrics directory")
			}
			if err := t.load(now); err != nil {
				return nil, err
			}
		}
		s.tiers = append(s.tiers, t)
	}
	s.resetSince()
	return s, nil
}

func (t *tier) load(now time.Time) error {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return model.ErrInternal.Wrap(err, "read the metrics directory")
	}
	oldest := now.Add(-t.retention).UTC().Format(dayLayout)
	for _, e := range entries {
		day, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if !ok || day < oldest {
			continue
		}
		if err := t.loadFile(filepath.Join(t.dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (t *tier) loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return model.ErrInternal.Wrap(err, "open "+path)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		var p domain.MetricPoint
		// 書いている途中で止まった最後の行は読めないので飛ばす
		if err := json.Unmarshal(sc.Bytes(), &p); err != nil {
			continue
		}
		mergeInto(t.points, p)
	}
	if err := sc.Err(); err != nil {
		return model.ErrInternal.Wrap(err, "read "+path)
	}
	return nil
}

func mergeInto(m map[int64]*domain.MetricPoint, p domain.MetricPoint) {
	key := p.At.UnixNano()
	q, ok := m[key]
	if !ok {
		q = &domain.MetricPoint{At: p.At}
		m[key] = q
	}
	q.Merge(p)
}

func (s *Store) Add(points ...domain.MetricPoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range points {
		if s.since.IsZero() || p.At.Before(s.since) {
			s.since = p.At
		}
		for _, t := range s.tiers {
			p.At = p.At.Truncate(t.step)
			mergeInto(t.points, p)
			if t.dir != "" {
				mergeInto(t.pending, p)
			}
		}
	}
}

func (s *Store) Range(from, to time.Time, step time.Duration) []domain.MetricPoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tierFor(from, to, step)
	var out []domain.MetricPoint
	for _, p := range t.points {
		if p.At.Add(t.step).After(from) && !p.At.After(to) {
			out = append(out, p.Clone())
		}
	}
	slices.SortFunc(out, func(a, b domain.MetricPoint) int { return a.At.Compare(b.At) })
	return out
}

// tierFor は step で割り切れる区切りのうち、from までを保持しているいちばん細かいものを選ぶ。どれも保持していなければいちばん長く持つもの
func (s *Store) tierFor(from, to time.Time, step time.Duration) *tier {
	fallback := s.tiers[0]
	for _, t := range s.tiers {
		if step%t.step != 0 {
			continue
		}
		if !from.Before(to.Add(-t.retention)) {
			return t
		}
		fallback = t
	}
	return fallback
}

func (s *Store) Since() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.since, !s.since.IsZero()
}

// resetSince は読み直したときと古いものを捨てたときに since を決め直す。細かい区切りから順に見て、
// 粗い区切りは、細かい区切りより丸ごと前に集計があるときだけ使う (時刻を切り捨てて持つので、重なるなら始まりを早く見せてしまう)
func (s *Store) resetSince() {
	s.since = time.Time{}
	for _, t := range s.tiers {
		oldest, ok := t.oldest()
		if ok && (s.since.IsZero() || !oldest.Add(t.step).After(s.since)) {
			s.since = oldest
		}
	}
}

func (t *tier) oldest() (time.Time, bool) {
	var oldest time.Time
	for _, p := range t.points {
		if oldest.IsZero() || p.At.Before(oldest) {
			oldest = p.At
		}
	}
	return oldest, !oldest.IsZero()
}

func (s *Store) Flush(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	pruned := false
	for _, t := range s.tiers {
		n := len(t.points)
		errs = append(errs, t.flush(now), t.prune(now))
		pruned = pruned || len(t.points) < n
	}
	if pruned {
		s.resetSince()
	}
	return errors.Join(errs...)
}

func (t *tier) flush(now time.Time) error {
	byDay := map[string][]*domain.MetricPoint{}
	for key, p := range t.pending {
		if p.At.Add(t.step).After(now) {
			continue
		}
		day := p.At.UTC().Format(dayLayout)
		byDay[day] = append(byDay[day], p)
		delete(t.pending, key)
	}
	for day, points := range byDay {
		slices.SortFunc(points, func(a, b *domain.MetricPoint) int { return a.At.Compare(b.At) })
		if err := appendLines(filepath.Join(t.dir, day+".jsonl"), points); err != nil {
			return err
		}
	}
	return nil
}

func appendLines(path string, points []*domain.MetricPoint) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return model.ErrInternal.Wrap(err, "open "+path)
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, p := range points {
		if err := enc.Encode(p); err != nil {
			f.Close()
			return model.ErrInternal.Wrap(err, "encode a metric point")
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return model.ErrInternal.Wrap(err, "write "+path)
	}
	return f.Close()
}

func (t *tier) prune(now time.Time) error {
	oldest := now.Add(-t.retention)
	for key, p := range t.points {
		if p.At.Before(oldest) {
			delete(t.points, key)
		}
	}
	if t.dir == "" {
		return nil
	}
	oldestDay := oldest.UTC().Format(dayLayout)
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return model.ErrInternal.Wrap(err, "read the metrics directory")
	}
	for _, e := range entries {
		day, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if !ok || day >= oldestDay {
			continue
		}
		if err := os.Remove(filepath.Join(t.dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return model.ErrInternal.Wrap(err, "remove an expired metrics file")
		}
	}
	return nil
}
