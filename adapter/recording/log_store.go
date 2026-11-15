package recording

import (
	"slices"
	"sync"

	"github.com/kakkky/gorviz/config"
	"github.com/kakkky/gorviz/domain"
)

// LogStore は受け取ったログを古い順に持ち、上限を超えたら古いものから捨てる。
type LogStore struct {
	mu     sync.RWMutex
	lines  []domain.LogLine
	nextID domain.LogLineID
	max    int
}

func NewLogStore(cfg *config.Config) *LogStore {
	return &LogStore{max: cfg.MaxLogLines}
}

func (s *LogStore) Append(line domain.LogLine) domain.LogLine {
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

func (s *LogStore) Snapshot() []domain.LogLine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.lines)
}

func (s *LogStore) Get(id domain.LogLineID) (domain.LogLine, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i, ok := slices.BinarySearchFunc(s.lines, id, func(l domain.LogLine, id domain.LogLineID) int { return int(l.ID - id) })
	if !ok {
		return domain.LogLine{}, false
	}
	return s.lines[i], true
}
