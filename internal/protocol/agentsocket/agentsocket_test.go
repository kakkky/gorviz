package agentsocket_test

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/protocol/agentsocket"
)

func TestReadWrite(t *testing.T) {
	t.Parallel()
	reply, err := agentsocket.Encode(agentsocket.KindReply, agentsocket.Reply{ID: 7, Coverage: []agentsocket.CoverageFile{{Name: "covmeta.x", Data: []byte{1, 2}}}})
	require.NoError(t, err)
	tests := []struct {
		name string
		msgs []agentsocket.Message
	}{
		{name: "reads a raw trace chunk as it was written", msgs: []agentsocket.Message{{Kind: agentsocket.KindTrace, Payload: []byte("go 1.27 trace")}}},
		{name: "reads an empty message", msgs: []agentsocket.Message{{Kind: agentsocket.KindBye, Payload: []byte{}}}},
		{name: "reads messages back to back in order", msgs: []agentsocket.Message{reply, {Kind: agentsocket.KindTrace, Payload: []byte("a")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			for _, m := range tt.msgs {
				require.NoError(t, agentsocket.Write(&buf, m))
			}
			r := bufio.NewReader(&buf)
			for _, want := range tt.msgs {
				got, err := agentsocket.Read(r)
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
		in   agentsocket.Instruction
	}{
		{name: "decodes an instruction with an event", in: agentsocket.Instruction{ID: 3, Op: agentsocket.OpWriteTraceEvent, Event: "phase smoke"}},
		{name: "decodes an instruction without an event", in: agentsocket.Instruction{ID: 4, Op: agentsocket.OpFinish}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, err := agentsocket.Encode(agentsocket.KindInstruction, tt.in)
			require.NoError(t, err)
			got, err := agentsocket.Decode[agentsocket.Instruction](m)
			require.NoError(t, err)
			assert.Equal(t, tt.in, got)
		})
	}
}
