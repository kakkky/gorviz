package domain_test

import (
	"testing"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCoverageReport(t *testing.T) {
	t.Parallel()
	profile := []byte(`mode: atomic
example.com/app/x/x.go:3.20,5.2 2 1
example.com/app/x/x.go:5.2,7.3 1 0
example.com/app/x/x.go:7.3,8.3 1 0
example.com/app/x/x.go:11.20,13.2 2 0
example.com/app/cmd/api/gorvizagent/agent.go:3.1,4.1 1 1
`)
	src := []byte("package x\n\nfunc A(v int) int {\n\tif v > 0 {\n\t\treturn 1\n\t}\n\treturn 2\n}\n\nfunc B() {\n\tprintln()\n\tprintln()\n}\n")
	tests := []struct {
		name  string
		check func(t *testing.T, r domain.CoverageReport)
	}{
		{
			name: "leaves out the agent",
			check: func(t *testing.T, r domain.CoverageReport) {
				assert.Equal(t, 6, r.Statements)
				assert.Equal(t, 2, r.Covered)
			},
		},
		{
			name: "groups the lines not reached by function as ranges",
			check: func(t *testing.T, r domain.CoverageReport) {
				require.Len(t, r.Partial, 1)
				assert.Equal(t, "example.com/app/x.A", r.Partial[0].Function)
				assert.Equal(t, []domain.LineRange{{From: 5, To: 8}}, r.Partial[0].Uncovered)
				assert.Equal(t, []string{"example.com/app/x.B"}, r.Unreached)
			},
		},
	}
	blocks, err := domain.ParseCoverageProfile(profile)
	require.NoError(t, err)
	r := domain.BuildCoverageReport(blocks, map[string][]byte{"example.com/app/x/x.go": src})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, r)
		})
	}
}
