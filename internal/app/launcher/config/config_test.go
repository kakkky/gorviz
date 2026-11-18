package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		command []string
		want    string
	}{
		{name: "success: the package of go run", command: []string{"go", "run", "./example/todo", "-addr", ":8080"}, want: "todo"},
		{name: "success: flags of go run before the package are skipped", command: []string{"go", "run", "-race", "./cmd/api"}, want: "api"},
		{name: "success: a go file of go run", command: []string{"go", "run", "main.go"}, want: "main"},
		{name: "success: the binary", command: []string{"./bin/billing", "-port", "9000"}, want: "billing"},
		{name: "success: no command means no service", command: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, serviceOf(tt.command))
		})
	}
}
