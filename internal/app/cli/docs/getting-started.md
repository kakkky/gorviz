# getting-started

How to diagnose an app with gorviz from start to end. Read this first.

## 1. Start the app

    gorviz run <main package> [-- app arguments]

- Pass a main package the same way as `go run`, like `gorviz run ./cmd/api -- --port 8080`.
- gorviz builds the app with net/http/pprof opened on a private unix socket (your files are not changed) and starts it as a child process.
  The environment of `gorviz run` is passed to the build and to the app as it is.
- The command returns once the app runs, with the GUI URL. Build errors are returned here.
- The standard output and error of the app are not shown. gorviz only looks at the Go runtime.
- Only one session runs at a time. `gorviz status` shows it.

## 2. Diagnose

    gorviz diagnose scenario.yaml

- For an API server (HTTP), write a scenario first: `gorviz docs scenario`.
  Read the handlers and routes of the app to decide what to send.
- gorviz sends the load in fixed phases, so a diagnosis takes about 35 seconds every time:
  smoke (each flow once), ramp-up (4 levels of concurrency based on the number of Ps, 5 seconds each),
  cancellation (5 seconds, 30% of the requests are cut by the client) and cooldown (3 seconds).
- A line is printed when each phase (each level of ramp-up) ends, then the summary.
- When a flow does not pass smoke, the diagnosis stops there. Fix the scenario with the statuses in the summary.
- For a daemon, a batch or a CLI, run `gorviz diagnose` without a scenario. It sends no load and watches the app
  until it exits (or 30 seconds for an app that does not exit).
- The exit code is 0 when no issue was found, 1 when issues were found and 2 when the app could not be diagnosed.

## 3. Read the issues

    gorviz issues
    gorviz issue <id>
    gorviz goroutine <id> --from <time> --to <time>

- `gorviz issue <id>` returns the timeline of the occurrences, the goroutines affected the most and what each was blocked on.
- When a goroutine waited for another one (LOCK_WAIT, SYNC_WAIT), follow the goroutine that woke it with the `next` command.
- Each answer has the GUI URL of the same thing. Pass it to the developer so they can replay the scheduler there.
- Times are seconds since the session started, like `12.500s`.
- Meanings: `gorviz docs issues`, `gorviz docs goroutines`.

## 4. Stop

    gorviz stop

- gorviz sends SIGTERM to the app and keeps tracing until it exits, so a shutdown that does not finish shows up.
  An app that does not finish within 10 seconds is killed.
- The answer has the goroutines left at the shutdown, from the last goroutineleak profile taken before the app exited.
  All data of the session is gone after this.
- The trace is taken in chunks of 250ms, and the chunk being taken when the app exits never arrives,
  so the last 250ms or less before the exit is not in the trace.
