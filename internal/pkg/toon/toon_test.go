package toon_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/pkg/toon"
)

func TestMarshal(t *testing.T) {
	t.Parallel()
	type row struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	tests := []struct {
		name string
		v    any
		want string
	}{
		{
			name: "writes fields in the order of the struct",
			v:    struct{ B, A string }{B: "x", A: "y"},
			want: "B: x\nA: y\n",
		},
		{
			name: "writes uniform objects as a table",
			v:    map[string]any{"rows": []row{{1, "a"}, {2, "b,c"}}},
			want: "rows[2]{id,name}:\n  1,a\n  2,\"b,c\"\n",
		},
		{
			name: "writes primitives inline and quotes ambiguous strings",
			v:    map[string]any{"tags": []string{"x", "1", ""}},
			want: "tags[3]: x,\"1\",\"\"\n",
		},
		{
			name: "writes nested objects with indentation",
			v:    map[string]any{"a": map[string]any{"b": true}},
			want: "a:\n  b: true\n",
		},
		{
			name: "writes mixed arrays as a list",
			v:    map[string]any{"items": []any{map[string]any{"k": 1, "n": []int{1}}, 2}},
			want: "items[2]:\n  - k: 1\n    n[1]: 1\n  - 2\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := toon.Marshal(tt.v)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}
