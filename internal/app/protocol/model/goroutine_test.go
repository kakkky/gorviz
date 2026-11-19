package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

func TestGoroutine_Origin_wrapper(t *testing.T) {
	t.Parallel()
	g := model.Goroutine{EntryFunction: "main.fetchUser.func1", CreatedBy: "github.com/kakkky/scope.(*Scope).Go", CreatedAt: "/mod/scope/scope.go:75"}
	assert.Equal(t, model.GoroutineOrigin("main.fetchUser.func1"), g.Origin(), "goroutines started by a wrapper are grouped by the user's function, not the wrapper's go statement")
	assert.Equal(t, "main.fetchUser.func1", model.EntryFunction([]string{"main.fetchUser.func1", "golang.org/x/sync/errgroup.(*Group).Go.func1"}))
	assert.Equal(t, "main.requests.func1", model.EntryFunction([]string{"main.handleRequest", "main.requests.func1.1", "runtime/pprof.Do", "main.requests.func1"}), "a goroutine started with pprof.Do still has its own go statement's function as the entry")
}
