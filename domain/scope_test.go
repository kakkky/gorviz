package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/domain"
)

func TestSummarizeScopes(t *testing.T) {
	t.Parallel()
	gs := []domain.Goroutine{
		{ID: 1, State: "running", Labels: map[string]string{"scope": "checkout"}},
		{ID: 2, State: "chan receive", Leaked: true, Labels: map[string]string{"scope": "checkout"}},
		{ID: 3, State: "select", Labels: map[string]string{"scope": "search"}},
		{ID: 4, State: "select"},
	}

	assert.Equal(t, []domain.ScopeSummary{
		{Name: "checkout", Goroutines: 2, Waiting: 1, Leaked: 1},
		{Name: "search", Goroutines: 1, Waiting: 1},
	}, domain.SummarizeScopes(gs))
}

func TestGoroutine_Origin_wrapper(t *testing.T) {
	t.Parallel()
	g := domain.Goroutine{EntryFunction: "main.fetchUser.func1", CreatedBy: "github.com/kakkky/scope.(*Scope).Go", CreatedAt: "/mod/scope/scope.go:75"}
	assert.Equal(t, domain.GoroutineOrigin("main.fetchUser.func1"), g.Origin(), "goroutines started by a wrapper are grouped by the user's function, not the wrapper's go statement")
	assert.Equal(t, "main.fetchUser.func1", domain.EntryFunction([]string{"main.fetchUser.func1", "golang.org/x/sync/errgroup.(*Group).Go.func1"}))
}
