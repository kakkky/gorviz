package handler

import (
	"testing"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/stretchr/testify/assert"
)

func TestNewGMPReplayJSON_LimitsEvents(t *testing.T) {
	t.Parallel()
	events := func(n int) []model.SchedEvent {
		out := make([]model.SchedEvent, n)
		for i := range out {
			out[i] = model.SchedEvent{At: time.Duration(i), Kind: model.SchedEventProc, Proc: 1, ProcStatus: model.ProcIdle}
		}
		return out
	}

	under := newGMPReplayJSON(model.SchedulerTimeline{Window: time.Second, Events: events(maxReplayEvents)}, nil, time.Time{})
	assert.Len(t, under.Events, maxReplayEvents)
	assert.False(t, under.Truncated)

	over := newGMPReplayJSON(model.SchedulerTimeline{Window: time.Second, Events: events(maxReplayEvents + 1)}, nil, time.Time{})
	assert.Len(t, over.Events, maxReplayEvents)
	assert.True(t, over.Truncated)
}
