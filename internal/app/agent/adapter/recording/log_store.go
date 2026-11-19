package recording

import (
	"slices"
	"sync"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

// LogStore は受け取ったログを古い順に持ち、上限を超えたら古いものから捨てる。
type LogStore struct {
	mu     sync.RWMutex
	lines  []model.LogLine
	nextID model.LogLineID
	max    int
}

func NewLogStore(maxLines int) *LogStore {
	return &LogStore{max: maxLines}
}

func (s *LogStore) Append(line model.LogLine) model.LogLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	line.ID = s.nextID
	s.lines = append(s.lines, line)
	if len(s.lines) > s.max {
		s.lines = slices.Delete(s.lines, 0, len(s.lines)-s.max)
	}
	return line
}

func (s *LogStore) Snapshot() []model.LogLine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.lines)
}

func (s *LogStore) Get(id model.LogLineID) (model.LogLine, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i, ok := slices.BinarySearchFunc(s.lines, id, func(l model.LogLine, id model.LogLineID) int { return int(l.ID - id) })
	if !ok {
		return model.LogLine{}, false
	}
	return s.lines[i], true
}
