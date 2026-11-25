package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestParseScenario(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		yaml    string
		wantErr []string
		check   func(t *testing.T, s domain.Scenario)
	}{
		{
			name: "reads flows with captures and defaults",
			yaml: `
target: http://127.0.0.1:8080
flows:
  - name: create
    steps:
      - method: post
        path: /todos
        json: {title: "t{{seq}}"}
        capture: {id: json:id}
      - path: /todos/{{id}}
`,
			check: func(t *testing.T, s domain.Scenario) {
				assert.Equal(t, "GET", s.Ready.Method)
				require.Len(t, s.Flows, 1)
				assert.Equal(t, 1, s.Flows[0].Weight)
				assert.Equal(t, "POST", s.Flows[0].Steps[0].Method)
			},
		},
		{
			name:    "rejects a missing target and empty flows",
			yaml:    "flows: []\n",
			wantErr: []string{"target is missing", "flows is empty"},
		},
		{
			name: "rejects a value used before it is captured",
			yaml: `
target: http://127.0.0.1:8080
flows:
  - steps:
      - path: /todos/{{id}}
`,
			wantErr: []string{"{{id}} is not captured"},
		},
		{
			name: "rejects unknown keys",
			yaml: `
target: http://127.0.0.1:8080
concurrency: 10
flows: [{steps: [{path: /}]}]
`,
			wantErr: []string{"unknown keys are rejected"},
		},
		{
			name: "rejects two bodies in one step and a path without a slash",
			yaml: `
target: http://127.0.0.1:8080
flows:
  - steps:
      - {method: POST, path: todos, body: x, form: {a: b}}
`,
			wantErr: []string{"must start with /", "only one of body, json and form"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, err := domain.ParseScenario([]byte(tt.yaml))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, domain.ErrInvalidArgument)
				for _, w := range tt.wantErr {
					assert.Contains(t, err.Error(), w)
				}
				return
			}
			require.NoError(t, err)
			tt.check(t, s)
		})
	}
}

func TestScenarioRequest(t *testing.T) {
	t.Parallel()
	s, err := domain.ParseScenario([]byte(`
target: http://127.0.0.1:8080/base
flows:
  - steps:
      - method: POST
        path: /todos/{{vu}}
        query: {q: "{{seq}}"}
        form: {title: "t{{seq}}"}
        capture: {id: json:items.0.id, loc: header:location}
`))
	require.NoError(t, err)
	st := s.Flows[0].Steps[0]
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "expands built-in values into the URL and the body",
			check: func(t *testing.T) {
				r := s.Request(st, domain.Vars{"vu": "3", "seq": "9"})
				assert.Equal(t, "http://127.0.0.1:8080/base/todos/3?q=9", r.URL)
				assert.Equal(t, "title=t9", string(r.Body))
				assert.Equal(t, "application/x-www-form-urlencoded", r.Headers["Content-Type"])
			},
		},
		{
			name: "captures values from the JSON body and a header",
			check: func(t *testing.T) {
				vars := domain.Vars{}
				st.CaptureInto(domain.Response{Status: 200, Body: []byte(`{"items":[{"id":42}]}`), Header: map[string][]string{"Location": {"/todos/42"}}}, vars)
				assert.Equal(t, "42", vars["id"])
				assert.Equal(t, "/todos/42", vars["loc"])
			},
		},
		{
			name: "passes statuses below 400 when nothing is expected",
			check: func(t *testing.T) {
				assert.True(t, st.Passed(domain.Response{Status: 303}))
				assert.False(t, st.Passed(domain.Response{Status: 500}))
				assert.False(t, st.Passed(domain.Response{Err: "refused"}))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t)
		})
	}
}
