# issues

The kinds of issues, how they are judged and how sure gorviz is about them.

An issue is a problem of the Go runtime found by gorviz in the session. Issues are counted per function
(closures are folded into the outer function, type parameters are folded). An occurrence is one stretch
of time in which the condition held. Occurrences during a diagnosis carry the phase they fell in.

## Kinds

- GOROUTINE_LEAK
  - Goroutines that should finish stay blocked on something and pile up.
  - FACT when the goroutineleak profile of the runtime says so (Go 1.26 and later). Per stack, gorviz shows
    "N of the M goroutines waiting at the same place leaked", because leaked goroutines and ones that can still be woken can wait at the same line.
  - Next: look at the go statement that created them and where they are blocked, and check they have a way to finish.
- CPU_CONTENTION
  - Goroutines that can run wait for a P. p99 of the wait from RUNNABLE to RUNNING reached 1ms (INFERENCE).
  - ramp-up raises the concurrency above the number of Ps on purpose, so this shows up at some level. The level it starts at tells how much concurrency the CPU can take.
    If LOCK_CONTENTION or another issue starts at a lower level, the app is stuck inside before the CPU runs out.
  - Next: review the work of the goroutines that used the CPU for long in the same period.
- GC_PRESSURE
  - GC takes the CPU away from the app. FACT when GC used 50% of the CPU, INFERENCE when mark assist used more than 5%.
  - Next: reduce the allocations of the goroutines with long mark assist, or review GOGC and GOMEMLIMIT.
- LOCK_CONTENTION
  - Goroutines spend more time waiting on sync.Mutex / sync.RWMutex Lock than running (INFERENCE). Judged for the whole app.
  - `lockSites` lists the functions that called Lock with the share of the wait, the count, the mean and the max.
    Many short waits mean the lock is taken too often; few long waits mean someone holds it for long. `heldBy` is the side that held it.
  - Next: review the granularity of the lock, how long it is held and how it is split.
- LONG_STW
  - One stop-the-world took 1ms or more (INFERENCE). The target is the reason of the STW.
  - Next: review the calls that cause it (runtime.ReadMemStats, runtime.GC, taking profiles and so on).
- THREAD_EXPLOSION
  - The number of OS threads went over half of the limit (10,000) (INFERENCE).
  - Next: look at where the syscalls and cgo calls are made and limit how many run at the same time.

## When they are judged

- All the time while the app runs, every second, for all kinds but GOROUTINE_LEAK. The goroutineleak profile is taken every 15 seconds.
- For each level of ramp-up and for cancellation, again over the whole phase.
- GOROUTINE_LEAK after cooldown, for what was left after the load. The profile only says what is left at that time,
  so it does not tell which phase leaked; the creator and the blocked place tell which path it was.
- At shutdown (`gorviz stop`) and when the app exits by itself, the goroutines left at that time.

## Certainty

- FACT: said directly by the runtime or recorded in the execution trace.
- INFERENCE: inferred by gorviz from a threshold, a trend or a stack. Do not treat it as a fact.

The goroutines of the gorviz agent are never counted in issues.
