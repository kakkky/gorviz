package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestSchedulerTimeline_EnrichFromDump(t *testing.T) {
	t.Parallel()
	waitingUnknown := model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: model.UnknownWaitReason}
	tl := model.SchedulerTimeline{
		Entries: map[model.GoroutineID]string{1: "main.truncated", 3: "main.fromTrace"},
		Events: []model.SchedEvent{
			{Kind: model.SchedEventGoroutine, Goroutine: 1, Place: waitingUnknown},
			{Kind: model.SchedEventGoroutine, Goroutine: 2, Place: waitingUnknown},
			{Kind: model.SchedEventGoroutine, Goroutine: 3, Place: waitingUnknown},
		},
	}
	domain.EnrichFromDump(&tl, []model.Goroutine{
		{ID: 1, State: "chan receive", EntryFunction: "main.worker", CurrentFunction: "main.fetch"},
		{ID: 2, State: "running", EntryFunction: "main.busy", CurrentFunction: "main.busy"},
	})

	assert.Equal(t, map[model.GoroutineID]string{1: "main.worker", 2: "main.busy", 3: "main.fromTrace"}, tl.Entries)
	assert.Equal(t, []model.SchedEvent{
		{Kind: model.SchedEventGoroutine, Goroutine: 1, Place: model.GoroutinePlace{Kind: model.PlaceWaiting, Reason: "chan receive"}, Function: "main.fetch"},
		{Kind: model.SchedEventGoroutine, Goroutine: 2, Place: waitingUnknown},
		{Kind: model.SchedEventGoroutine, Goroutine: 3, Place: waitingUnknown},
	}, tl.Events)
}
