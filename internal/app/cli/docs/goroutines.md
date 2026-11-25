# goroutines

What the details of a goroutine mean: the main cause, the place it was blocked and the goroutine that woke it.

`gorviz goroutine <id>` and each goroutine in `gorviz issue <id>` say what one goroutine was blocked on in a range.
Without --from/--to, the range is the life of the goroutine (up to the last minute).

## Fields

- timeByState: how long it was in each state in the range.
- mainCause: the longest of them.
- stoppedAt: the function and file:line it was blocked at the longest, with the wait reason of the runtime.
- wokenBy: for LOCK_WAIT and SYNC_WAIT, the goroutines that ended the wait and where. Run its `next` command to open it
  over the range it made this one wait. For a lock this is the side that held the lock.
- createdBy: the function with the go statement and its file:line.
- leak: whether the runtime judged it leaked, with the certainty.
- agent: true for the goroutines of the gorviz agent. They are shown but never counted in issues.
- traceTruncated: the app ended on the spot (os.Exit, log.Fatal, a panic outside main) and the end of the trace is missing.

## Main causes

- RUNNING: it used the CPU itself.
- RUNNABLE: it could run but no P was free.
- LOCK_WAIT: it waited on Lock of sync.Mutex or sync.RWMutex.
- SYNC_WAIT: it waited for another goroutine on a channel, select, sync.WaitGroup or sync.Cond.
  The reason it was slow is on the other side, so follow wokenBy.
- IO_WAIT: it waited for the network, the disk or a syscall.
- CGO: it ran C code through cgo.
- SLEEP: it was in a sleep or a timer.
- GC: it was stopped by GC or made to do mark assist.

## Certainty

- FACT: decided from the state changes in the execution trace.
- INFERENCE: the goroutine was already waiting before the agent started, so the wait reason was guessed from its stack.
  For leaks, more goroutines waited at the same place than leaked, so the ones waiting the longest were picked.

## Limits

- The run queue a goroutine waited in (which P, or the global one) is not in the trace, so the GUI guesses it.
  How long it waited and which P and M ran it are as recorded.
