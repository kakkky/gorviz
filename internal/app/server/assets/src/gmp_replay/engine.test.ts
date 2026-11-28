import test from "node:test"
import assert from "node:assert/strict"
import { Replay } from "./engine"
import type { ReplayWindow } from "../gen/types"

const base: ReplayWindow = {
  from: "", to: "", startedAtMs: 0, durationNs: 100, procs: [0, 1], functions: ["main.worker", "sync.(*Mutex).Lock"], reasons: ["sync"],
  entries: { "1": 0, "2": 0, "3": 0 }, events: [], procEvents: [], lanes: [[], []], stw: [], gcMark: [], leaks: [], gorviz: [], truncated: false, ended: false,
}

test("places goroutines on Ps, run queues and waiting groups at a time", () => {
  const r = new Replay({
    ...base,
    procEvents: [{ t: 0, p: 0, run: true, m: 3 }],
    events: [
      { t: 0, g: 1, k: 0, p: 0, m: 3, r: -1, f: -1, c: 0 },
      { t: 0, g: 2, k: 1, p: 0, m: 0, r: -1, f: -1, c: 1 },
      { t: 10, g: 3, k: 3, p: -1, m: 0, r: 0, f: 1, c: 2 },
    ],
  })
  const v = r.seek(20)
  assert.equal(v.procs[0].current?.g, 1)
  assert.deepEqual(v.procs[0].localQueue.map((g) => g.g), [2])
  assert.equal(v.waiting[0].entry, "main.worker")
  assert.equal(v.waiting[0].reason, "sync")
  assert.equal(v.waiting[0].goroutines[0].cause, "LOCK_WAIT")
})

test("replays from the start when seeking backwards", () => {
  const r = new Replay({ ...base, events: [{ t: 0, g: 1, k: 1, p: 0, m: 0, r: -1, f: -1, c: 1 }, { t: 50, g: 1, k: 3, p: -1, m: 0, r: 0, f: -1, c: 3 }] })
  assert.equal(r.seek(60).waiting.length, 1)
  assert.equal(r.seek(10).procs[0].localQueue[0].g, 1)
})

test("keeps a goroutine in a syscall on its P only while the P has no other goroutine", () => {
  const r = new Replay({
    ...base,
    procEvents: [{ t: 0, p: 0, run: true, m: 1 }],
    events: [{ t: 0, g: 1, k: 4, p: 0, m: 1, r: -1, f: -1, c: 4 }, { t: 5, g: 2, k: 0, p: 0, m: 2, r: -1, f: -1, c: 0 }],
  })
  assert.equal(r.seek(1).procs[0].inSyscall, true)
  const later = r.seek(10)
  assert.equal(later.procs[0].current?.g, 2)
  assert.deepEqual(later.handedOff.map((g) => g.g), [1])
})

test("tells the goroutines of gorviz apart", () => {
  const r = new Replay({ ...base, gorviz: [7] })
  assert.equal(r.isGorviz(7), true)
  assert.equal(r.isGorviz(1), false)
})
