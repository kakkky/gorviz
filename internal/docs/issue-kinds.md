# The kinds of Issues, the lines they are judged by, and where to look next

An Issue is a problem in the runtime layer. It has no resolved state: it is ongoing while an occurrence goes on in some instance. It is counted per function (closures are folded into the outer function).

- `GOROUTINE_LEAK`
  - Goroutines that should end keep piling up, waiting for something
  - FACT
    - the runtime goroutineleak profile found them
  - INFERENCE
    - the number of waiting goroutines per creator keeps growing without going down
  - Next
    - look at the go statement that created them and where they stop, and check that each goroutine has a way to end (context cancellation, closing the channel, a timeout)
- `CPU_CONTENTION`
  - `RUNNABLE` goroutines wait for a P
  - INFERENCE
    - the p99 from `RUNNABLE` to `RUNNING` is 1ms or more, or far above the usual
  - Next
    - review the goroutines that used the CPU long at the same time, or the CPU given to the app
- `GC_PRESSURE`
  - The GC takes the CPU away from the app
  - FACT
    - the GC used 50% of the CPU
  - INFERENCE
    - mark assist used over 5% of the CPU, or the GC share is far above the usual
  - Next
    - reduce the allocations of the goroutines with long mark assist, or review GOGC and GOMEMLIMIT
- `LOCK_CONTENTION`
  - Goroutines spend more time waiting for locks than working
  - INFERENCE
    - the time waited for locks is longer than the running time, or far above the usual (judged for the whole instance)
  - Next
    - look at the functions that called `Lock` with the most waiting. Tell whether the lock is taken too often or held too long from the count and the time per wait, and follow the goroutine that held it
- `LONG_STW`
  - A stop-the-world stops the whole app for long
  - INFERENCE
    - one stop-the-world took 1ms or more
  - Next
    - look at the reason of the stop-the-world, and review the calls that cause it (`runtime.ReadMemStats`, `runtime.GC`, taking profiles, ...)
- `THREAD_EXPLOSION`
  - Too many OS threads
  - INFERENCE
    - more than half of the limit (10,000 by default), or far above the usual
  - Next
    - look at where the syscalls and cgo calls are made, and limit how many run at the same time

"Far above the usual" means the modified z-score against the median and the MAD of the 5-minute values around the same time on the same weekday (up to the last 3 weeks) is over 3.5.
