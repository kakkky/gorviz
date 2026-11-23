package domain

import (
	"crypto/sha256"
	"regexp"
	"strings"
	"sync"
)

type Stats struct {
	Total      int
	Done       int
	Duplicates int
}

// ComputeStats は完了数と、同じ内容の todo (重複) の数を数える。重複は題を正規化した指紋で見分ける。
// 不具合 (CPU_CONTENTION): todo ごとに goroutine を立てて重いハッシュを計算するので、件数が多いと P が埋まり、ほかのリクエストが実行を待たされる
func ComputeStats(todos []Todo) Stats {
	s := Stats{Total: len(todos)}
	var wg sync.WaitGroup
	prints := make([][32]byte, len(todos))
	for i, t := range todos {
		if t.Done {
			s.Done++
		}
		wg.Go(func() { prints[i] = fingerprint(t.Title) })
	}
	wg.Wait()
	seen := map[[32]byte]bool{}
	for _, p := range prints {
		if seen[p] {
			s.Duplicates++
		}
		seen[p] = true
	}
	return s
}

var titleSuffix = regexp.MustCompile(`\s*(#\d+|\(visitor \d+\))$`)

func fingerprint(title Title) [32]byte {
	sum := sha256.Sum256([]byte(strings.ToLower(titleSuffix.ReplaceAllString(string(title), ""))))
	for range 30000 {
		sum = sha256.Sum256(sum[:])
	}
	return sum
}
