package schedtrace_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/adapter/schedtrace"
	"github.com/kakkky/gorviz/domain"
)

func TestStore_Consume(t *testing.T) {
	t.Parallel()
	stderr := `app started
SCHED 1004ms: gomaxprocs=2 idleprocs=1 threads=8 spinningthreads=0 needspinning=0 idlethreads=5 runqueue=3 gcwaiting=false
  P0: status=1 schedtick=18 syscalltick=0 m=4 runqsize=2 gfreecnt=0 timerslen=2
  P1: status=0 schedtick=3 syscalltick=0 m=nil runqsize=0 gfreecnt=5 timerslen=0
  M4: p=0 curg=7 mallocing=0 throwing=0 preemptoff= locks=0 dying=0 spinning=false blocked=false lockedg=nil
  G1: status=4(IO wait) m=nil lockedm=nil
  G7: status=2() m=4 lockedm=nil
{"msg":"a log line written in the middle of the block"}
  G8: status=1() m=nil lockedm=nil
  G9: status=6() m=nil lockedm=nil
  G10: status=10(chan receive) m=nil lockedm=nil
  G11: status=4100(select) m=nil lockedm=nil
request handled
`
	store := schedtrace.NewStore()
	_, ok := store.Latest()
	require.False(t, ok)

	var passthrough bytes.Buffer
	require.NoError(t, store.Consume(strings.NewReader(stderr), &passthrough))

	got, ok := store.Latest()
	require.True(t, ok)
	assert.Equal(t, domain.SchedTraceReport{
		Uptime:         1004 * time.Millisecond,
		GOMAXPROCS:     2,
		IdleProcs:      1,
		Threads:        8,
		IdleThreads:    5,
		GlobalRunQueue: 3,
		Procs: []domain.SchedTraceProc{
			{Proc: 0, Status: "running", Thread: 4, HasThread: true, RunQueue: 2},
			{Proc: 1, Status: "idle", GFree: 5},
		},
		GoroutineStates: map[string]int{"waiting": 2, "running": 1, "runnable": 1, "dead": 1, "leaked": 1},
	}, got)
	assert.Equal(t, "app started\n{\"msg\":\"a log line written in the middle of the block\"}\nrequest handled\n", passthrough.String(), "detail lines after an interleaved log line still belong to the block")
}
