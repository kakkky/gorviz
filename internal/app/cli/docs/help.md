gorviz builds a Go app with an agent in it, runs it on your machine and diagnoses the health of its Go runtime
(goroutine leaks, lock contention, waiting for the CPU, GC, stop-the-world and too many OS threads).

A session goes like this:

  gorviz run ./cmd/api -- --port 8080   start the app and return once it runs
  gorviz diagnose scenario.yaml         load it and report issues (about 35 seconds)
  gorviz issue I1                       open an issue down to the goroutines behind it
  gorviz goroutine 42                   follow the goroutine that made another one wait
  gorviz stop                           watch the shutdown and end the session

Every command except run talks to the gorviz server over a unix socket on this machine (a fixed path, or --socket).
The GUI listens on http://127.0.0.1:24678 (GORVIZ_GUI_ADDR to change it).
Output is TOON by default and JSON with --format json.

Read the documents first: gorviz docs, then gorviz docs getting-started.
