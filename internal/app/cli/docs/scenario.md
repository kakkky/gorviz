# scenario

How to write a scenario: what gorviz sends to an HTTP API server during `gorviz diagnose`.

A scenario is a YAML file. It only says what to send. How much load, how long and in what order is decided by gorviz.

```yaml
target: http://127.0.0.1:8080      # the base URL the app listens on (required)
ready:                             # sent until it passes, to wait for the app (default: GET /)
  method: GET
  path: /healthz
setup:                             # sent once before the load, to make the data the load uses
  - method: POST
    path: /todos
    json: {title: "seed"}
    capture:
      firstID: json:id             # values captured here can be used by every flow
flows:                             # the requests sent under load (at least one)
  - name: create-and-complete
    weight: 3                      # how often this flow is picked relative to the others (default 1)
    steps:
      - method: POST
        path: /todos
        json: {title: "todo {{seq}}"}
        capture:
          id: json:id              # json:<dot path> (like items.0.id) or header:<name>
      - method: POST
        path: /todos/{{id}}/complete
        expect: [200, 204]         # passed statuses (default: below 400)
  - name: list
    steps:
      - method: GET
        path: /todos
        query: {limit: "20"}
```

## Steps

- `method` is one of GET, HEAD, POST, PUT, PATCH, DELETE and OPTIONS (default GET).
- `path` starts with `/` and is joined to `target`.
- Use one of `json` (sent as application/json), `form` (sent as application/x-www-form-urlencoded) and `body` (sent as it is).
- `headers` and `query` are maps of strings.
- `{{name}}` is replaced by a value captured by an earlier step of the same flow (or of setup),
  or by a built-in value: `vu` (the number of the virtual user) and `seq` (a number unique to each request).
- Redirects are not followed, so a 303 after a form post passes as it is.
- When a step does not pass, the rest of the flow is skipped.

## How it is sent

- Each virtual user (VU) keeps its own cookies and repeats flows picked by weight.
- smoke sends each flow once in order. If one does not pass, the diagnosis stops and returns the statuses.
- ramp-up runs 4 levels with P, 2P, 4P and 8P VUs (P is GOMAXPROCS of the app), 5 seconds each.
- cancellation runs 2P VUs for 5 seconds and cuts 30% of the requests from the client side,
  after 0 to 2 times the median latency of ramp-up. Cancellation paths are where goroutines often leak.
- cooldown stops the load, waits 3 seconds and takes the goroutineleak profile and the coverage.

## Errors

The scenario is checked before any load is sent. Each problem says where it is and how to fix it.
Unknown keys are rejected.
