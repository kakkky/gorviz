package recording

import (
	"encoding/json"
	"fmt"
	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// TraceParser は保存した生の trace を読み直す (domain.RuntimeParser の ParseTrace)
type TraceParser func(raw []byte, receivedAt time.Time) (model.TraceChunk, error)

// SavedStore は Issue の前後の録画をディスクに保存する。dir/<id>/meta.json と、区切りごとの生の trace (chunk-N.trace) を置く。
// 生の trace はそのまま `go tool trace` で開ける。起動時に dir にあるものを読み直す
type SavedStore struct {
	mu     sync.RWMutex
	dir    string
	parse  TraceParser
	meta   map[string]model.SavedRecording
	chunks map[string][]model.TraceChunk
}

func NewSavedStore(dir string, parse TraceParser) (*SavedStore, error) {
	s := &SavedStore{dir: dir, parse: parse, meta: map[string]model.SavedRecording{}, chunks: map[string][]model.TraceChunk{}}
	if dir == "" {
		return s, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, model.ErrInternal.Wrap(err, "create the save directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, model.ErrInternal.Wrap(err, "read the save directory")
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := s.load(e.Name()); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *SavedStore) load(id string) error {
	b, err := os.ReadFile(filepath.Join(s.dir, id, "meta.json"))
	if err != nil {
		return nil
	}
	var rec model.SavedRecording
	if err := json.Unmarshal(b, &rec); err != nil {
		return model.ErrInternal.Wrap(err, "read "+id+"/meta.json")
	}
	var chunks []model.TraceChunk
	for i := range rec.Chunks {
		raw, err := os.ReadFile(filepath.Join(s.dir, id, fmt.Sprintf("chunk-%d.trace", i)))
		if err != nil {
			return model.ErrInternal.Wrap(err, "read a saved trace")
		}
		c, err := s.parse(raw, rec.To)
		if err != nil {
			return err
		}
		c.Raw = raw
		chunks = append(chunks, c)
	}
	s.meta[id], s.chunks[id] = rec, chunks
	return nil
}

func (s *SavedStore) Save(rec model.SavedRecording, chunks []model.TraceChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir != "" {
		dir := filepath.Join(s.dir, rec.ID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return model.ErrInternal.Wrap(err, "create a recording directory")
		}
		for i, c := range chunks {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("chunk-%d.trace", i)), c.Raw, 0o644); err != nil {
				return model.ErrInternal.Wrap(err, "write a saved trace")
			}
		}
		b, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			return model.ErrInternal.Wrap(err, "encode the recording")
		}
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644); err != nil {
			return model.ErrInternal.Wrap(err, "write meta.json")
		}
	}
	s.meta[rec.ID], s.chunks[rec.ID] = rec, chunks
	return nil
}

func (s *SavedStore) Has(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.meta[id]
	return ok
}

func (s *SavedStore) List() []model.SavedRecording {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.SavedRecording, 0, len(s.meta))
	for _, r := range s.meta {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b model.SavedRecording) int { return b.At.Compare(a.At) })
	return out
}

func (s *SavedStore) Chunks(id string) ([]model.TraceChunk, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.chunks[id]
	return c, ok
}

// all は保存したすべての区切りを古い順に返す。同じ区切りが複数の録画に入っていれば 1 つにする
func (s *SavedStore) all() []model.TraceChunk {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[int64]bool{}
	var out []model.TraceChunk
	for _, cs := range s.chunks {
		for _, c := range cs {
			if seen[c.StartedAt.UnixNano()] {
				continue
			}
			seen[c.StartedAt.UnixNano()] = true
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b model.TraceChunk) int { return a.StartedAt.Compare(b.StartedAt) })
	return out
}

// CompositeTraceRecording は server の側の録画。まず agent のリングバッファを見て、なければ保存した録画を見る
type CompositeTraceRecording struct {
	primary domain.TraceRecording
	saved   *SavedStore
}

func NewCompositeTraceRecording(primary domain.TraceRecording, saved *SavedStore) *CompositeTraceRecording {
	return &CompositeTraceRecording{primary: primary, saved: saved}
}

func (c *CompositeTraceRecording) Append(model.TraceChunk) {}

func (c *CompositeTraceRecording) Latest() (model.TraceChunk, bool) {
	if ch, ok := c.primary.Latest(); ok {
		return ch, true
	}
	all := c.saved.all()
	if len(all) == 0 {
		return model.TraceChunk{}, false
	}
	return all[len(all)-1], true
}

func (c *CompositeTraceRecording) At(t time.Time) (model.TraceChunk, bool) {
	if ch, ok := c.primary.At(t); ok {
		return ch, true
	}
	for _, ch := range c.saved.all() {
		if ch.Contains(t) {
			return ch, true
		}
	}
	return model.TraceChunk{}, false
}

func (c *CompositeTraceRecording) Between(from, to time.Time) []model.TraceChunk {
	out := c.primary.Between(from, to)
	seen := map[int64]bool{}
	for _, ch := range out {
		seen[ch.StartedAt.UnixNano()] = true
	}
	for _, ch := range c.saved.all() {
		if seen[ch.StartedAt.UnixNano()] || ch.EndedAt().Before(from) || ch.StartedAt.After(to) {
			continue
		}
		out = append(out, ch)
	}
	slices.SortFunc(out, func(a, b model.TraceChunk) int { return a.StartedAt.Compare(b.StartedAt) })
	return out
}

func (c *CompositeTraceRecording) FindLogMark(seq uint64) (model.TraceChunk, model.TraceLogMark, bool) {
	if ch, m, ok := c.primary.FindLogMark(seq); ok {
		return ch, m, true
	}
	for _, ch := range c.saved.all() {
		for _, m := range ch.LogMarks {
			if m.Seq == seq {
				return ch, m, true
			}
		}
	}
	return model.TraceChunk{}, model.TraceLogMark{}, false
}

func (c *CompositeTraceRecording) Span() (model.RecordingSpan, bool) {
	return c.primary.Span()
}
