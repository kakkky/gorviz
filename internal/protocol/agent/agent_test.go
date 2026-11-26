package agent_test

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/protocol/agent"
)

func TestReadWrite(t *testing.T) {
	t.Parallel()
	reply, err := agent.Encode(agent.TypeResponse, agent.Response{ID: 7, Coverage: []agent.CoverageFile{{Name: "covmeta.x", Data: []byte{1, 2}}}})
	require.NoError(t, err)
	tests := []struct {
		name string
		msgs []agent.Message
	}{
		{name: "reads a raw trace chunk as it was written", msgs: []agent.Message{{Type: agent.TypeTrace, Payload: []byte("go 1.27 trace")}}},
		{name: "reads an empty message", msgs: []agent.Message{{Type: agent.TypeBye, Payload: []byte{}}}},
		{name: "reads messages back to back in order", msgs: []agent.Message{reply, {Type: agent.TypeTrace, Payload: []byte("a")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			for _, m := range tt.msgs {
				require.NoError(t, agent.Write(&buf, m))
			}
			r := bufio.NewReader(&buf)
			for _, want := range tt.msgs {
				got, err := agent.Read(r)
				require.NoError(t, err)
				assert.Equal(t, want, got)
			}
		})
	}
}

func TestDecode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   agent.Request
	}{
		{name: "decodes an instruction with an event", in: agent.Request{ID: 3, Method: agent.MethodWriteTraceEvent, Event: "phase smoke"}},
		{name: "decodes an instruction without an event", in: agent.Request{ID: 4, Method: agent.MethodFinish}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, err := agent.Encode(agent.TypeRequest, tt.in)
			require.NoError(t, err)
			got, err := agent.Decode[agent.Request](m)
			require.NoError(t, err)
			assert.Equal(t, tt.in, got)
		})
	}
}
