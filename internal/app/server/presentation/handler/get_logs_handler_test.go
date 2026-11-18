package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
)

func TestLogDetail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		line       model.LogLine
		wantFields []components.LogFieldViewModel
		wantRaw    string
	}{
		{
			name: "success: a JSON line becomes keys and values in the written order, nested objects as dotted keys",
			line: model.LogLine{Message: "request done", Raw: `{"time":"2026-11-19T02:38:19+09:00","msg":"request done","http":{"status":303,"path":"/todos"},"tags":["a","b"],"ok":true}`},
			wantFields: []components.LogFieldViewModel{
				{Key: "time", Value: "2026-11-19T02:38:19+09:00"},
				{Key: "msg", Value: "request done"},
				{Key: "http.status", Value: "303"},
				{Key: "http.path", Value: "/todos"},
				{Key: "tags", Value: `["a","b"]`},
				{Key: "ok", Value: "true"},
			},
		},
		{name: "success: a plain text line is shown as it was logged", line: model.LogLine{Message: "done", Raw: "2026/11/19 done id=3"}, wantRaw: "2026/11/19 done id=3"},
		{name: "success: nothing to show when the line is the message itself", line: model.LogLine{Message: "done", Raw: "done"}},
		{name: "success: a JSON array is not split into keys", line: model.LogLine{Message: "x", Raw: `[1,2]`}, wantRaw: "[1,2]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fields, raw := logDetail(tt.line)
			assert.Equal(t, tt.wantFields, fields)
			assert.Equal(t, tt.wantRaw, raw)
		})
	}
}
