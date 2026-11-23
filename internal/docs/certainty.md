# How to read the certainty (FACT / INFERENCE) and what a result was computed from

- `FACT`
  - read directly from the runtime's judgement or the recorded trace
- `INFERENCE`
  - inferred by gorviz from trends or comparison. Do not treat it as a fact; confirm it with the GUI replay or another operation

Certainty in a goroutine diagnosis:

- Main cause
  - `FACT` when decided from the state changes in the execution trace
  - `INFERENCE` when decided only from the wait reason inferred from the stack in a goroutine snapshot
- The goroutine that wrote a log line
  - `FACT` when matched by the number the logger adapter wrote
  - `INFERENCE` when it was the only goroutine running at that time
- A goroutine's leak
  - `FACT` when there were exactly as many goroutines with the same stack as the leak count
  - `INFERENCE` when gorviz picked as many goroutines with the same stack as the leak count, the longest waiting first

A diagnosis says what it was computed from, finest first.

- `execution-trace`
  - the period can be cut out as is
- `goroutine-state-breakdown`
  - totals per minute. The period is aligned to whole minutes
- `goroutine-snapshot`
  - only the state at that moment
