package funcname_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/pkg/funcname"
)

func TestOuter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fn   string
		want string
	}{
		{name: "keeps a named function", fn: "main.(*Server).handle", want: "main.(*Server).handle"},
		{name: "folds a closure into the outer function", fn: "main.(*Server).handle.func1", want: "main.(*Server).handle"},
		{name: "folds a nested closure into the outer function", fn: "example.com/a.run.func2.1", want: "example.com/a.run"},
		{name: "folds a go statement wrapper into the outer function", fn: "main.serve.gowrap1", want: "main.serve"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, funcname.Outer(tt.fn))
		})
	}
}

func TestFoldTypeParams(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fn   string
		want string
	}{
		{name: "keeps a function without type parameters", fn: "main.run", want: "main.run"},
		{name: "folds the shape of type parameters", fn: "main.Map[go.shape.int,go.shape.string]", want: "main.Map[...]"},
		{name: "folds the type parameters of a method", fn: "main.(*List[...]).Push", want: "main.(*List[...]).Push"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, funcname.FoldTypeParams(tt.fn))
		})
	}
}

func TestIsStdlib(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fn   string
		want bool
	}{
		{name: "treats sync as the standard library", fn: "sync.(*Mutex).Lock", want: true},
		{name: "treats net/http as the standard library", fn: "net/http.(*conn).serve", want: true},
		{name: "treats main as the app", fn: "main.handle", want: false},
		{name: "treats a domain path as the app", fn: "github.com/a/b.Run", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, funcname.IsStdlib(tt.fn))
		})
	}
}
