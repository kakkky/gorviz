package domain

import (
	"cmp"
	"github.com/kakkky/gorviz/internal/app/api/model"
	"slices"
)

// ScopeLabel は structured concurrency のスコープ (kakkky/scope など) の名前を載せる pprof label のキー。
// スコープへの所属は外からは分からないので、スコープ側がこの label を付けた G だけをまとめる
const ScopeLabel = "scope"

// ScopeSummary は 1 つのスコープに属する G の様子。スコープを抜けたあとも残っている G があれば、寿命の保証が破れている疑いがある
type ScopeSummary struct {
	Name       string
	Goroutines int
	Waiting    int
	Leaked     int
}

func SummarizeScopes(gs []model.Goroutine) []ScopeSummary {
	byName := map[string]*ScopeSummary{}
	for _, g := range gs {
		name, ok := g.Labels[ScopeLabel]
		if !ok {
			continue
		}
		s, ok := byName[name]
		if !ok {
			s = &ScopeSummary{Name: name}
			byName[name] = s
		}
		s.Goroutines++
		if g.StateCategory() == model.GoroutineWaiting {
			s.Waiting++
		}
		if g.Leaked {
			s.Leaked++
		}
	}
	out := make([]ScopeSummary, 0, len(byName))
	for _, s := range byName {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b ScopeSummary) int {
		return cmp.Or(cmp.Compare(b.Goroutines, a.Goroutines), cmp.Compare(a.Name, b.Name))
	})
	return out
}
