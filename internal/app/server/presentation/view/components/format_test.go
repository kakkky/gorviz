package components_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
)

func TestShortPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "success: origin with an absolute path", in: "main.leakForever @ /Users/me/gorviz/example/leaky/main.go:57", want: "main.leakForever @ leaky/main.go:57"},
		{name: "success: function name with a package path is kept", in: "net/http.(*Server).Serve", want: "net/http.(*Server).Serve"},
		{name: "success: function name in a module is shortened to its package", in: "github.com/me/app/internal/agent.(*Agent).Run.func1", want: "agent.(*Agent).Run.func1"},
		{name: "success: function name at the root of a module", in: "github.com/me/app.Start", want: "app.Start"},
		{name: "success: function name in a module with its file", in: "github.com/me/app/internal/agent.(*Agent).Run @ /src/app/internal/agent/agent.go:46", want: "agent.(*Agent).Run @ agent/agent.go:46"},
		{name: "success: file without a directory", in: "main.go:3", want: "main.go:3"},
		{name: "success: no path", in: "main.main", want: "main.main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, components.ShortPath(tt.in))
		})
	}
}
