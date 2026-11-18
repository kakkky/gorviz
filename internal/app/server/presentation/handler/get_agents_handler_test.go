package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBackAfterSelect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		back string
		want string
	}{
		{name: "success: stays on the same page", back: "/logs?range=1h&q=GET", want: "/logs?q=GET&range=1h"},
		{name: "success: drops the agent chosen in the URL", back: "/issues?agent=a&range=1d", want: "/issues?range=1d"},
		{name: "success: a goroutine of another agent means nothing, so back to the overview", back: "/goroutines/12?at=1", want: "/"},
		{name: "error: outside this server", back: "//evil.example/x", want: "/"},
		{name: "error: an absolute URL", back: "https://evil.example/", want: "/"},
		{name: "error: empty", back: "", want: "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, backAfterSelect(tt.back))
		})
	}
}
