import { MAIN_CAUSES, PLACE_KINDS, type MainCause, type PlaceKind, type ReplayEvent, type ReplayWindow } from "../gen/types"

export interface GoroutineState {
  g: number
  place: PlaceKind
  proc: number
  thread: number
  reason: string
  fn: string
  cause: MainCause | ""
  wokenBy: number
  seq: number
  // since は今の置き場所に入った時刻。trace は置き場所が変わらなくても書き直すので、同じ置き場所なら前の時刻を保つ
  since: number
}

export interface ProcView {
  proc: number
  running: boolean
  thread: number
  current?: GoroutineState
  inSyscall: boolean
  localQueue: GoroutineState[]
}

export interface WaitingGroup {
  entry: string
  reason: string
  goroutines: GoroutineState[]
}

export interface SchedView {
  at: number
  procs: ProcView[]
  globalQueue: GoroutineState[]
  waiting: WaitingGroup[]
  handedOff: GoroutineState[]
  stw?: string
  gcMark: boolean
  live: number
}

// Replay は 1 つの窓の状態の変化を順に当て、ある時刻の GMP の全体像を出す。前に進むときは続きから当て、戻るときは最初から当て直す
export class Replay {
  private goroutines = new Map<number, GoroutineState>()
  private procs = new Map<number, { running: boolean; thread: number }>()
  private nextEvent = 0
  private nextProc = 0
  private at = -1
  private agents: Set<number>

  constructor(readonly window: ReplayWindow) {
    this.agents = new Set(window.agents ?? [])
    this.reset()
  }

  // isAgent は gorviz の agent の G か。隠さず、目立たない色で見せる
  isAgent(g: number): boolean {
    return this.agents.has(g)
  }

  get duration(): number {
    return this.window.durationNs
  }

  entryOf(g: number): string {
    const i = this.window.entries[String(g)]
    return i === undefined ? "" : this.window.functions[i] ?? ""
  }

  private reset() {
    this.goroutines.clear()
    this.procs.clear()
    for (const p of this.window.procs) this.procs.set(p, { running: false, thread: -1 })
    this.nextEvent = 0
    this.nextProc = 0
    this.at = -1
  }

  private apply(ev: ReplayEvent, seq: number) {
    const w = this.window
    const prev = this.goroutines.get(ev.g)
    const place = PLACE_KINDS[ev.k] ?? "waiting"
    const same = prev !== undefined && prev.place === place && prev.proc === ev.p
    this.goroutines.set(ev.g, {
      g: ev.g,
      place,
      proc: ev.p,
      thread: ev.m,
      reason: ev.r >= 0 ? w.reasons[ev.r] ?? "" : "",
      fn: ev.f >= 0 ? w.functions[ev.f] ?? "" : prev?.fn ?? "",
      cause: ev.c >= 0 ? MAIN_CAUSES[ev.c] ?? "" : "",
      wokenBy: ev.w ?? 0,
      seq,
      since: same ? prev.since : ev.t,
    })
  }

  seek(t: number): SchedView {
    if (t < this.at) this.reset()
    const w = this.window
    while (this.nextEvent < w.events.length && w.events[this.nextEvent].t <= t) {
      this.apply(w.events[this.nextEvent], this.nextEvent)
      this.nextEvent++
    }
    while (this.nextProc < w.procEvents.length && w.procEvents[this.nextProc].t <= t) {
      const p = w.procEvents[this.nextProc]
      this.procs.set(p.p, { running: p.run, thread: p.m })
      this.nextProc++
    }
    this.at = t
    return this.view(t)
  }

  // eventTimes は前後の変化へ 1 つずつ進めるための時刻
  nextChangeAfter(t: number): number | undefined {
    const evs = this.window.events
    let lo = 0
    let hi = evs.length
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (evs[mid].t <= t) lo = mid + 1
      else hi = mid
    }
    return lo < evs.length ? evs[lo].t : undefined
  }

  prevChangeBefore(t: number): number | undefined {
    const evs = this.window.events
    let lo = 0
    let hi = evs.length
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (evs[mid].t < t) lo = mid + 1
      else hi = mid
    }
    return lo > 0 ? evs[lo - 1].t : undefined
  }

  // recentChanges は t までの直近の変化。古い順
  recentChanges(t: number, n: number): ReplayEvent[] {
    const evs = this.window.events
    let end = 0
    while (end < evs.length && evs[end].t <= t) end++
    return evs.slice(Math.max(0, end - n), end)
  }

  private view(t: number): SchedView {
    const bySeq = (a: GoroutineState, b: GoroutineState) => a.seq - b.seq
    const procs = new Map<number, ProcView>()
    for (const [p, s] of this.procs) procs.set(p, { proc: p, running: s.running, thread: s.thread, inSyscall: false, localQueue: [] })
    const procOf = (p: number) => {
      let v = procs.get(p)
      if (!v) {
        v = { proc: p, running: false, thread: -1, inSyscall: false, localQueue: [] }
        procs.set(p, v)
      }
      return v
    }
    const global: GoroutineState[] = []
    const handedOff: GoroutineState[] = []
    const waiting = new Map<string, WaitingGroup>()
    const syscallOnP: GoroutineState[] = []
    let live = 0
    for (const g of this.goroutines.values()) {
      if (g.place !== "dead") live++
      switch (g.place) {
        case "running":
          if (g.proc >= 0) procOf(g.proc).current = g
          break
        case "local_run_queue":
          if (g.proc >= 0) procOf(g.proc).localQueue.push(g)
          else global.push(g)
          break
        case "global_run_queue":
          global.push(g)
          break
        case "waiting": {
          const entry = this.entryOf(g.g) || g.fn || "entry unknown"
          const key = `${entry}\u0000${g.reason}`
          let grp = waiting.get(key)
          if (!grp) {
            grp = { entry, reason: g.reason, goroutines: [] }
            waiting.set(key, grp)
          }
          grp.goroutines.push(g)
          break
        }
        case "syscall":
          if (g.proc >= 0) syscallOnP.push(g)
          else handedOff.push(g)
          break
      }
    }
    // P を持ったまま syscall に入った G は、その P がまだ走っていて別の G を走らせていなければ P の上に置く。そうでなければ P を手放した M の上にいる
    for (const g of syscallOnP) {
      const p = procOf(g.proc)
      if (p.running && !p.current) {
        p.current = g
        p.inSyscall = true
      } else {
        handedOff.push(g)
      }
    }
    for (const p of procs.values()) p.localQueue.sort(bySeq)
    global.sort(bySeq)
    handedOff.sort((a, b) => a.g - b.g)
    const groups = [...waiting.values()]
    for (const grp of groups) grp.goroutines.sort((a, b) => a.g - b.g)
    groups.sort((a, b) => b.goroutines.length - a.goroutines.length || a.entry.localeCompare(b.entry))
    const stw = this.window.stw.find((r) => r.s <= t && t <= r.e)
    return {
      at: t,
      procs: [...procs.values()].sort((a, b) => a.proc - b.proc),
      globalQueue: global,
      waiting: groups,
      handedOff,
      stw: stw ? (stw.r >= 0 ? this.window.reasons[stw.r] ?? "stop-the-world" : "stop-the-world") : undefined,
      gcMark: this.window.gcMark.some((r) => r.s <= t && t <= r.e),
      live,
    }
  }
}

// Segment は 1 つの G が同じ主因でいた区間
export interface Segment {
  start: number
  end: number
  cause: string
  reason: string
}

// segmentsOf は窓の中の G の状態の推移を区間に分ける。同じ主因・同じ理由が隙間なく続くところはつなげる
export function segmentsOf(w: ReplayWindow, g: number): Segment[] {
  const out: Segment[] = []
  let open: Segment | null = null
  for (const ev of w.events) {
    if (ev.g !== g) continue
    const cause = PLACE_KINDS[ev.k] === "dead" ? "EXITED" : ev.c >= 0 ? MAIN_CAUSES[ev.c] ?? "" : ""
    const reason = PLACE_KINDS[ev.k] === "waiting" && ev.r >= 0 ? w.reasons[ev.r] ?? "" : ""
    if (open && open.cause === cause && open.reason === reason) continue
    if (open) {
      open.end = ev.t
      if (open.end > open.start) out.push(open)
    }
    open = cause ? { start: ev.t, end: w.durationNs, cause, reason } : null
  }
  if (open && open.end > open.start) out.push(open)
  return out
}
