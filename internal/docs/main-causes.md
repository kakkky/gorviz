# The main causes (what a goroutine was stopped on) and the next step for each

The main cause is the longest state in the time-by-state breakdown of the period.

- `RUNNING`
  - It used the CPU itself. Review the work at the place it stopped
- `RUNNABLE`
  - It was ready to run but waited for a P. Look for `CPU_CONTENTION` in the same time
- `LOCK_WAIT`
  - It waited in `Lock` of `sync.Mutex` or `sync.RWMutex`. Follow the goroutine that woke it (the one that held the lock)
- `SYNC_WAIT`
  - It waited for another goroutine on a channel, `select`, `sync.WaitGroup` or `sync.Cond`. The reason it was slow is on the side it waited for, so follow the goroutine that woke it
- `IO_WAIT`
  - It waited for the network, the disk or a system call. Look at the external dependencies in your APM
- `CGO`
  - It ran C code through cgo
- `SLEEP`
  - It was stopped by a sleep or a timer
- `GC`
  - The GC stopped it or made it do mark assist. Look for `GC_PRESSURE` and `LONG_STW` in the same time

`LOCK_WAIT` and the `SYNC_WAIT` of `sync.WaitGroup` both have the wait reason "sync" in the trace, so gorviz tells them apart by the stack when the wait began.

The goroutine that woke it is known only while the execution trace is kept.
