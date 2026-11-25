# todo-app: a tutorial for gorviz

A small todo web app with goroutine bugs that real web apps often have. Run it under gorviz and follow each bug from
the issue down to the line that causes it.

The app is split into the same layers as gorviz itself (`config/`, `driver/`, `presentation/`, `worker/`, `usecase/`,
`domain/`, `adapter/`, assembled in `main.go`). It listens on `localhost:8090` (`TODO_ADDR`).

## Start

From the root of the repository:

```sh
make build
./bin/gorviz run ./example/todo-app
./bin/gorviz diagnose example/todo-app/scenario.yaml
```

- `gorviz run` builds the app with the agent and returns once it runs. Open the GUI at http://127.0.0.1:7070.
- [scenario.yaml](scenario.yaml) seeds 1,000 todos, then sends five flows: add a todo with a reminder and complete it,
  list, show the statistics, export CSV and back up.
- The diagnosis takes about a minute here, because some of the requests are very slow (that is the point).
  The summary lists the issues and the phase each started in, then the coverage.

You can also open http://localhost:8090 and click around while the session runs: gorviz judges issues all the time,
not only during `gorviz diagnose`.

## 1. Goroutines pile up (GOROUTINE_LEAK)

```sh
./bin/gorviz issues --kind GOROUTINE_LEAK
./bin/gorviz issue <id>
```

- The target is `(*NotifierClient).RegisterReminder`, the function with the go statement. The certainty is `FACT`:
  the runtime's goroutineleak profile found goroutines that nothing can ever wake.
- `leaks` says "N of the N goroutines waiting at the same place leaked", blocked at `notifier_client.go:27` on `chan send`.
- The goroutine's `mainCause` is `SYNC_WAIT` and it has been stopped there for most of its life.
- The count grows from phase to phase, and cancellation adds more: a client that gives up makes the receiver return early.

**Answer**: `adapter/client/notifier_client.go`. The goroutine sends its result to an unbuffered channel, but when the
timeout fires first, nobody receives any more and the sender waits forever. `make(chan error, 1)` fixes it.

## 2. Requests wait on a lock (LOCK_CONTENTION)

```sh
./bin/gorviz issues --kind LOCK_CONTENTION
./bin/gorviz issue <id>
```

- `lockSites` has one row: `(*MemStore).Do` at `driver/memstore/memstore.go:35` takes almost all of the wait.
  The count is small and the mean and the max are long, so the lock is not taken too often — someone holds it for long.
- The goroutines affected the most are HTTP handlers (`net/http.(*conn).serve`) with `mainCause: LOCK_WAIT`.
- Follow `heldBy` (or `wokenBy`) with the `next` command to see what the holder was doing while it held the lock.

**Answer**: `usecase/backup_todos_usecase.go`. It calls the slow backup write (`BackupFileAccessor.Write`, 1.5 seconds)
inside `WithAllLocked`, holding the store's lock. Encode under the lock and write outside of it.

## 3. Requests wait for the CPU (CPU_CONTENTION)

```sh
./bin/gorviz issues --kind CPU_CONTENTION
./bin/gorviz issue <id>
```

- The goroutines that stayed `RUNNABLE` the longest are `domain.ComputeStats.func1`, thousands of them.
- Open the scheduler URL of the issue: every P is busy and the run queues are full of the same goroutines.
- The handler that asked for the statistics is in `SYNC_WAIT` on `sync.(*WaitGroup).Wait`: it only waits for its
  children; the reason it is slow is on their side.

**Answer**: `domain/stats.go`. `ComputeStats` starts a goroutine per todo for a CPU-heavy hash, so every request makes
thousands of runnable goroutines and the other requests wait for a P too. Limit the workers to the number of Ps, or cache the result.

## 4. GC takes the CPU (GC_PRESSURE)

This one shows up when the export is called with many todos (it can depend on your machine).

- The goroutines with the longest mark assist are the CSV export requests.
- On the Overview, the heap jumps up and comes back: lots of short-lived allocations, not a growing heap.

**Answer**: `domain/csv.go`. `ExportCSV` rebuilds the whole string for each line, allocating the square of the number of
todos. Use `strings.Builder` or `encoding/csv`.

## 5. Stop

```sh
./bin/gorviz stop
```

gorviz sends SIGTERM, keeps tracing until the app exits, and reports the goroutines that were left — here, the leaked
reminder senders again, now in the `shutdown` phase.

## Let an LLM agent do it

```text
Diagnose ./example/todo-app with gorviz. Read `gorviz docs getting-started` and `gorviz docs scenario` first,
write your own scenario from presentation/web/routes.go, then explain each issue with the file:line that causes it
and the GUI URL I can check.
```
