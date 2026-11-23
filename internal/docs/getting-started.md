# Read this before investigating with gorviz: what it tells and in what order to use the operations

gorviz diagnoses what the goroutines of a Go app were stopped on, from the Go runtime's execution trace.

- Noticing slow requests or more errors is the job of your APM. gorviz starts from the logs, time ranges and Issues you found there, and looks into the goroutines and the runtime
- gorviz handles the apps it observes as "services" (the instances of the same app together) and "instances" (one process)
  - Per service
    - the overview, Issues and logs (`get-overview`, `list-issues`, `search-logs`, ...)
  - Per instance
    - goroutine diagnoses, state changes and summaries (`diagnose-goroutine`, ...)
- A goroutine is identified by its instance and its goroutine ID

## Order of use

1. Learn the services and instances with `list-services`
2. Narrow it down with `get-overview`, `list-issues` and `count-logs`
3. Call `diagnose-goroutine` with an Issue occurrence or a log line as the entry point. The server decides the period to look at from the entry point
4. Follow the IDs to look at next that come back (the goroutine that woke it, the goroutines an Issue hit the most, ...)
5. Read your code at the functions and file:line that come back. To let a person confirm it, hand them the GUI URL

Read next: `main-causes`, `issue-kinds`, `certainty`, `investigation`, `data-retention`
