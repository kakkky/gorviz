import type { JSX } from "preact"
import { useEffect, useMemo, useState } from "preact/hooks"
import type { GMPReplayProps, ReplayLeak, ReplayStateBefore, ReplayWindow } from "../gen/types"
import { Replay, segmentsOf, type GoroutineState, type SchedView, type WaitingGroup } from "./engine"
import { clock, duration, short } from "./format"
import { Signal, useSignal } from "./signal"

export interface GmpReplayIslandProps extends GMPReplayProps {
  windows: Signal<ReplayWindow[]>
  onSelect: (goroutine: number, from: string, to: string) => void
  onNavigate: (url: string) => void
}

// 再生は出来事 (G や P の状態が変わった瞬間) を 1 つずつ進める。時間で流すと 1 秒に何十もの出来事が過ぎて追えないため
const PLAY_INTERVAL_MS = 700
const LRQ_MAX = 8
const GRQ_MAX = 24
const WAIT_CHIPS_MAX = 40

// 止まっている G が待っている相手の種類と、runtime の中でぶら下がっている場所。trace の待機理由で見分ける
const WAIT_TARGETS = [
  { key: "channel", label: "channel", where: "recvq / sendq" },
  { key: "sync", label: "lock / sync", where: "semaphore" },
  { key: "network", label: "network", where: "netpoller" },
  { key: "timer", label: "timer", where: "sleep" },
  { key: "runtime", label: "runtime / unknown", where: "GC, system goroutines" },
] as const

function waitTargetOf(reason: string): string {
  if (["chan receive", "chan send", "select", "forever"].includes(reason)) return "channel"
  if (reason.startsWith("sync")) return "sync"
  if (reason === "network" || reason === "IO wait") return "network"
  if (reason === "sleep") return "timer"
  return "runtime"
}

export function GmpReplay(props: GmpReplayIslandProps) {
  const windows = useSignal(props.windows)
  const [index, setIndex] = useState(0)
  const [t, setT] = useState(0)
  const [playing, setPlaying] = useState(props.autoplay)
  const [livePaused, setLivePaused] = useState(false)

  const current = windows[Math.min(index, Math.max(windows.length - 1, 0))]
  const replay = useMemo(() => (current ? new Replay(current) : undefined), [current])
  const leaks = useMemo(() => new Map((current?.leaks ?? []).map((l) => [l.g, l])), [current])
  const following = props.live && !livePaused

  // 最新を追っているあいだは、新しく届いた窓に替え、録れている中でいちばん新しい時点を見せる
  useEffect(() => {
    if (!following || playing || windows.length === 0) return
    const last = windows.length - 1
    if (last !== index || t === 0) {
      setIndex(last)
      setT(windows[last].durationNs)
    }
  }, [windows.length, following, playing])

  // 最初の窓は、追っている G があればその G が最初に動く時点、なければ最初に G が走る時点から見せる。
  // 窓の外から移ってきたときは、移った先の時刻から見せる (unix ms は丸まっているので、その G の近くの出来事に合わせる)
  useEffect(() => {
    if (!replay || following) return
    const focus = props.focusGoroutine
    const at = takeStartAt(replay.window)
    const first = at !== undefined
      ? replay.window.events.find((e) => e.g === focus && e.t >= at - 1000)
      : replay.window.events.find((e) => (focus ? e.g === focus : e.k === 0))
    setT(first ? first.t : at ?? 0)
  }, [replay])

  useEffect(() => {
    if (!playing || !replay) return
    const id = setTimeout(() => {
      const next = replay.nextChangeAfter(t)
      if (next !== undefined) {
        setT(next)
        return
      }
      if (index < windows.length - 1) {
        setIndex(index + 1)
        setT(0)
        return
      }
      if (props.live) return
      setPlaying(false)
      if (props.nextUrl) props.onNavigate(props.nextUrl)
    }, PLAY_INTERVAL_MS)
    return () => clearTimeout(id)
  }, [playing, t, replay, index, windows.length])

  if (!current || !replay) return <p class="muted">waiting for the first trace window...</p>
  const view = replay.seek(Math.min(t, replay.duration))
  const pause = () => {
    setPlaying(false)
    setLivePaused(true)
  }
  const seekTo = (v: number) => {
    pause()
    setT(Math.max(0, Math.min(v, replay.duration)))
  }
  const select = (g: number) => props.onSelect(g, current.from, current.to)

  return (
    <>
      <div class="replay-head">
        <h2>
          GMP replay
          {current.fromPreserved && <span class="badge inference" title="the full execution trace of this time is past its retention; this replays the trace kept around an issue occurrence or an error log">from a kept trace</span>}
        </h2>
        <FocusSince window={current} g={props.focusGoroutine} t={t} before={index === 0 ? (props.focusBefore ?? undefined) : undefined} onSeek={seekTo} onNavigate={props.onNavigate} />
      </div>
      <div class="replay-controls">
        <div class="transport" role="group" aria-label="replay">
          <button type="button" title="step back to the previous event" aria-label="previous event" onClick={() => {
            const prev = replay.prevChangeBefore(t)
            if (prev !== undefined) seekTo(prev)
            else if (props.prevUrl) props.onNavigate(props.prevUrl)
          }}>⏮</button>
          <button type="button" class="primary play" title={playing || following ? "pause" : "play"} onClick={() => {
            if (playing || following) {
              pause()
              return
            }
            if (t >= replay.duration && index >= windows.length - 1) setT(0)
            setPlaying(true)
          }}>{playing || following ? "❚❚" : "▶"}</button>
          <button type="button" title="step forward to the next event" aria-label="next event" onClick={() => {
            const next = replay.nextChangeAfter(t)
            if (next !== undefined) seekTo(next)
            else if (props.nextUrl) props.onNavigate(props.nextUrl)
          }}>⏭</button>
        </div>
        <input type="range" class="seek" min={0} max={replay.duration} step={1000} value={t} onInput={(e) => seekTo(Number((e.target as HTMLInputElement).value))} />
        <span class="clock">{(t / 1e6).toFixed(3)}ms / {duration(replay.duration)}</span>
      </div>
      <div class="replay-legend">
        <span><span class="gdot cause-bg-RUNNING"></span> running</span>
        <span><span class="gdot cause-bg-RUNNABLE"></span> runnable (waiting for a P)</span>
        <span><span class="gdot cause-bg-IO_WAIT"></span> syscall / I/O wait</span>
        <span><span class="gdot cause-bg-LOCK_WAIT"></span> lock wait</span>
        <span><span class="gdot cause-bg-SYNC_WAIT"></span> sync wait (channel, WaitGroup)</span>
        <span><span class="gdot cause-bg-SLEEP"></span> sleep</span>
        <span><span class="gdot cause-bg-GC"></span> GC</span>
        <span title="goroutines parked inside the runtime, or with no wait reason in the trace"><span class="gdot other"></span> runtime / unknown</span>
        <span title="the ring marks a leaked goroutine; the fill still shows its state"><span class="gdot is-leaked"></span> leaked (ring)</span>
      </div>
      <Lanes window={current} replay={replay} t={t} focus={props.focusGoroutine} onSeek={seekTo} />
      <div class="replay-stage">
        <Stage view={view} replay={replay} t={t} focus={props.focusGoroutine} leaks={leaks} onSelect={select} />
      </div>
      <div class="replay-note">
        {current.truncated && <span class="error">events were truncated: the replay stops before the end of the window</span>}
      </div>
    </>
  )
}

// FocusSince は追っている G が今の時点でいつから今の状態かと、その状態に入った瞬間へ移るボタン。
// 窓の中で入っていれば再生の中で移り、最初の窓の頭より前からなら、サーバがさかのぼって見つけた時刻の画面へ移る
function FocusSince({ window: w, g, t, before, onSeek, onNavigate }: {
  window: ReplayWindow
  g: number
  t: number
  before?: ReplayStateBefore
  onSeek: (t: number) => void
  onNavigate: (url: string) => void
}) {
  const segs = useMemo(() => (g ? segmentsOf(w, g) : []), [w, g])
  if (!g) return null
  const seg = segs.find((s) => s.start <= t && t < s.end)
  const label = seg && `Go to when ${seg.cause} began`
  return (
    <span class="state-since">
      <span class="following-label">following</span>
      <span class="gchip is-focus small"><span class={`gdot cause-bg-${seg?.cause ?? "UNRECORDED"}`}></span>G{g}</span>
      {seg ? (
        <>
          <span class="cause-name">{seg.cause}</span>
          {seg.start > 0 ? (
            <>
              <span class="mono">since {clock(w.startedAtMs, seg.start)}</span>
              {t > seg.start && <button type="button" class="btn small" onClick={() => onSeek(seg.start)}>{label}</button>}
            </>
          ) : (
            <SinceBefore before={before?.cause === seg.cause ? before : undefined} label={label!} onNavigate={onNavigate} />
          )}
        </>
      ) : (
        <span class="muted">not seen at this moment</span>
      )}
    </span>
  )
}

function SinceBefore({ before, label, onNavigate }: { before?: ReplayStateBefore; label: string; onNavigate: (url: string) => void }) {
  if (!before) return <span class="mono">since before this window</span>
  const since = before.sinceMs > 0 ? clock(before.sinceMs, 0) : `before ${clock(before.recordedFromMs, 0)}`
  return (
    <>
      <span class="mono">since {since}</span>
      {before.url ? (
        <button type="button" class="btn small" onClick={() => { keepStartAt(before.sinceMs); onNavigate(before.url) }}>{before.sinceMs > 0 ? label : `Go back to ${clock(before.recordedFromMs, 0)}`}</button>
      ) : (
        <span class="faint">no earlier trace is recorded</span>
      )}
    </>
  )
}

// 窓の外の時刻へ移るとき、移った先の画面の再生をその時刻から始める。移った先は窓の頭を少し前にずらしてあり、頭からだと手前の出来事を見せてしまうため
const START_AT_KEY = "gorviz:replay-start-at"

function keepStartAt(ms: number) {
  try {
    if (ms > 0) sessionStorage.setItem(START_AT_KEY, String(ms))
  } catch {
    // 残せなければ、いつものとおり G が最初に動く時点から見せる
  }
}

function takeStartAt(w: ReplayWindow): number | undefined {
  let ms: number
  try {
    ms = Number(sessionStorage.getItem(START_AT_KEY))
    sessionStorage.removeItem(START_AT_KEY)
  } catch {
    return undefined
  }
  const t = Math.round((ms - w.startedAtMs) * 1e6)
  return ms > 0 && t >= 0 && t <= w.durationNs ? t : undefined
}

function pct(v: number, total: number): string {
  return `${((v / Math.max(total, 1)) * 100).toFixed(3)}%`
}

// axisTicks は 0 から w までを 4〜5 つほどに分ける、切りのよい時刻 (1・2・5 × 10^n ナノ秒の倍数)
function axisTicks(w: number): number[] {
  if (!(w > 0)) return [0]
  const raw = w / 5
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? raw
  const ticks: number[] = []
  for (let v = 0; v <= w + 1; v += step) ticks.push(v)
  return ticks
}

// Lanes は P ごとに、窓の中で G が走った区間の帯。押すとその時刻へ移る
function Lanes({ window: w, replay, t, focus, onSeek }: { window: ReplayWindow; replay: Replay; t: number; focus: number; onSeek: (t: number) => void }) {
  const total = w.durationNs
  const ticks = axisTicks(total)
  const tickLines = ticks.filter((v) => v > 0 && v < total).map((v) => <span class="tick" style={{ left: pct(v, total) }}></span>)
  const stw = w.stw.map((r) => (
    <span class={`stw${r.gc ? "" : " other"}`} style={{ left: pct(r.s, total), width: pct(r.e - r.s, total) }} title={`stop-the-world (${r.r >= 0 ? w.reasons[r.r] : "unknown"}) ${duration(r.e - r.s)}`}></span>
  ))
  const seekAt = (e: MouseEvent) => {
    const el = e.currentTarget as HTMLElement
    const rect = el.getBoundingClientRect()
    onSeek(Math.round(((e.clientX - rect.left) / rect.width) * total))
  }
  return (
    <div class="gmp-lanes">
      <div class="caption">
        <strong>CPU slots (P) over time</strong>
        <span class="lane-keys">
          <span><span class="lane-key seg"></span> a goroutine running</span>
          {focus > 0 && <span><span class="lane-key seg is-focus"></span> G{focus} running</span>}
          <span><span class="lane-key gc-mark"></span> GC concurrent mark</span>
          <span><span class="lane-key stw"></span> stop-the-world by GC</span>
          <span title="hover a band to see the reason"><span class="lane-key stw other"></span> stop-the-world by others (starting a trace, ...)</span>
        </span>
      </div>
      <div class="lanes-grid">
        <span class="pname gc">GC</span>
        <div class="lane-track gc-track" onClick={seekAt}>
          {tickLines}
          {w.gcMark.map((r) => <span class="gc-mark" style={{ left: pct(r.s, total), width: pct(r.e - r.s, total) }} title={`GC concurrent mark ${duration(r.e - r.s)}`}></span>)}
          {stw}
          <span class="cursor" style={{ left: pct(t, total) }}></span>
        </div>
        <span class="util"></span>
        {w.procs.map((p, i) => {
          const spans = w.lanes[i] ?? []
          const busy = spans.reduce((a, s) => a + (s.e - s.s), 0)
          return (
            <>
              <span class="pname">P{p}</span>
              <div class="lane-track" onClick={seekAt}>
                {tickLines}
                {spans.map((s) => (
                  <span class={`seg${s.g === focus ? " is-focus" : ""}`} style={{ left: pct(s.s, total), width: pct(s.e - s.s, total) }} title={`G${s.g} ${short(replay.entryOf(s.g)) || "entry unknown"}\n${duration(s.e - s.s)}`}></span>
                ))}
                {stw}
                <span class="cursor" style={{ left: pct(t, total) }}></span>
              </div>
              <span class="util" title="share of the window this P was running a goroutine">{total ? Math.round((busy / total) * 100) : 0}%</span>
            </>
          )
        })}
        <span></span>
        <div class="lanes-axis">
          {ticks.map((v) => <span class={v > total * 0.9 ? "end" : ""} style={{ left: pct(v, total) }}>{v === 0 ? "0" : duration(v)}</span>)}
        </div>
        <span class="util">busy</span>
      </div>
    </div>
  )
}

interface StageProps {
  view: SchedView
  replay: Replay
  t: number
  focus: number
  leaks: Map<number, ReplayLeak>
  onSelect: (g: number) => void
}

function Stage({ view, replay, t, focus, leaks, onSelect }: StageProps) {
  const stw = replay.window.stw.find((r) => r.s <= t && t < r.e)
  const stwReason = stw && stw.r >= 0 ? replay.window.reasons[stw.r] : "unknown"
  const chip = (g: GoroutineState, dot: string, wide: boolean, wait = "") => (
    <Chip g={g} dot={dot} wide={wide} wait={wait} focus={focus} leak={leaks.get(g.g)} replay={replay} onSelect={onSelect} />
  )
  const waitLabel = (g: GoroutineState) => duration(Math.max(0, t - g.since))
  return (
    <section class={`gmp-space${stw ? " is-stopped" : ""}`} aria-label="GMP">
      <div class="gmp-space-head">
        <strong>GMP</strong>
        <span class="faint">where each goroutine (G) is: on a CPU slot (P), in a run queue, on an OS thread (M) in a syscall, or parked</span>
        {stw ? (
          <span class={`gc-state stw${stw.gc ? "" : " other"}`} title="every goroutine is stopped">stop-the-world · {stwReason}</span>
        ) : (
          view.gcMark && <span class="gc-state mark" title="part of the Ps run GC work, and allocating goroutines help with mark assist">GC mark</span>
        )}
        <span class="mono gmp-space-at">{(t / 1e6).toFixed(3)}ms</span>
      </div>
      {stw && <div class="gmp-stopped" role="status">goroutines are stopped · stop-the-world ({stwReason}) · {duration(stw.e - stw.s)}</div>}
      <div class="gmp-board">
        <div class="gmp-main">
          <div class="gmp-procs">
            {view.procs.map((p) => (
              <div class={`proc ${p.running ? "running" : "idle"}${!p.current && p.localQueue.length === 0 ? " empty" : ""}`}>
                <div class="proc-head">
                  <span class="pname">P{p.proc}</span>
                  {p.running ? <span class="mname">M{p.thread}</span> : <span class="idle-label">idle</span>}
                </div>
                <div class="proc-run">{p.current && chip(p.current, p.inSyscall ? "cause-bg-IO_WAIT" : "cause-bg-RUNNING", true)}</div>
                <div class="proc-lrq">
                  {p.localQueue.slice(0, LRQ_MAX).map((g) => chip(g, "cause-bg-RUNNABLE", false, waitLabel(g)))}
                  {p.localQueue.length > LRQ_MAX && <span class="more">+{p.localQueue.length - LRQ_MAX}</span>}
                </div>
              </div>
            ))}
          </div>
          <div class="gmp-grq" title="inferred from the execution trace: a goroutine readied by a P goes to that P's local run queue, one descheduled by preemption, yield or a syscall without P goes to the global run queue. runnext is not distinguished">
            <h4>global run queue <span>{view.globalQueue.length}</span><small>shared by all P · head first</small></h4>
            <div class="grq-row">
              {view.globalQueue.slice(0, GRQ_MAX).map((g, i) => (
                <span class="slot-chip" title={`position ${i + 1} in the global run queue`}><span class="slot-label">{i + 1}</span>{chip(g, "cause-bg-RUNNABLE", true, waitLabel(g))}</span>
              ))}
              {view.globalQueue.length > GRQ_MAX && <span class="more">+{view.globalQueue.length - GRQ_MAX}</span>}
            </div>
          </div>
          <div class="gmp-row-area handed-off" title="not a queue: each goroutine stays on its M (OS thread) inside the kernel, and its P was handed to another M">
            <h4>M in syscall, P released <span>{view.handedOff.length}</span>{stw && <small class="keeps-running">still running in the kernel (or C for cgo) during stop-the-world · waits for a P when it returns</small>}</h4>
            <div class="m-rows">
              {view.handedOff.map((g) => (
                <span class="slot-chip"><span class="slot-label">{g.thread >= 0 ? `M${g.thread}` : "M?"}</span><span class="arrow">→</span>{chip(g, "cause-bg-IO_WAIT", true)}</span>
              ))}
            </div>
          </div>
        </div>
        <Parked waiting={view.waiting} chip={chip} focus={focus} leaks={leaks} />
      </div>
    </section>
  )
}

// Parked は止まっている G を、待っている相手の種類ごとの段に分け、その中を「関数 × 理由」でまとめる。
// 止まっている G はスケジューラのどのキューにもおらず、待っている相手の側からたどれるだけ
function Parked({ waiting, chip, focus, leaks }: { waiting: WaitingGroup[]; chip: (g: GoroutineState, dot: string, wide: boolean) => JSX.Element; focus: number; leaks: Map<number, ReplayLeak> }) {
  const sections = new Map<string, WaitingGroup[]>(WAIT_TARGETS.map((t) => [t.key, []]))
  for (const w of waiting) sections.get(waitTargetOf(w.reason))!.push(w)
  const counts = new Map(WAIT_TARGETS.map((t) => [t.key, sections.get(t.key)!.reduce((a, w) => a + w.goroutines.length, 0)]))
  const total = [...counts.values()].reduce((a, b) => a + b, 0)
  const jump = (e: MouseEvent, key: string) => {
    const side = (e.currentTarget as HTMLElement).closest(".gmp-side")
    side?.querySelector(`[data-parked-target="${key}"]`)?.scrollIntoView({ block: "nearest", behavior: "smooth" })
  }
  return (
    <div class="gmp-side" title="parked goroutines are not in any scheduler queue; each one hangs on what it waits for">
      <div class="parked-head">
        <h4>parked <span>{total}</span></h4>
        <div class="parked-share">
          {WAIT_TARGETS.map((t) => (counts.get(t.key) ? <span class={`share-${t.key}`} style={{ flex: counts.get(t.key) }} title={`${t.label}: ${counts.get(t.key)}`}></span> : null))}
        </div>
        <div class="parked-legend">
          {WAIT_TARGETS.map((t) =>
            counts.get(t.key) ? (
              <button type="button" class="parked-jump" onClick={(e) => jump(e, t.key)}><span class={`swatch share-${t.key}`}></span>{t.label}<span class="mono">{counts.get(t.key)}</span></button>
            ) : null,
          )}
        </div>
      </div>
      {WAIT_TARGETS.map((target) => {
        const groups = sections.get(target.key)!.slice().sort((a, b) => b.goroutines.length - a.goroutines.length)
        if (groups.length === 0) return null
        return (
          <div class="wtarget" data-parked-target={target.key}>
            <div class="wtarget-head"><span class={`swatch share-${target.key}`}></span><span>{target.label}</span><span class="faint">{target.where}</span><span class="count">{counts.get(target.key)}</span></div>
            {groups.map((w) => {
              const leaked = w.goroutines.filter((g) => leaks.has(g.g)).length
              const shown = w.goroutines.slice(0, WAIT_CHIPS_MAX)
              const f = w.goroutines.find((g) => g.g === focus)
              if (f && !shown.includes(f)) shown.push(f)
              return (
                <div class={`wgroup${f ? " has-focus" : ""}`} title={`${w.entry} waiting on ${w.reason || "unknown"}`}>
                  <div class="wgroup-head">
                    <span class="fn">{short(w.entry)}<span class="reason">{w.reason || "unknown"}</span></span>
                    {leaked > 0 && <span class="badge kind">{leaked} leaked</span>}
                    <span class="count">{w.goroutines.length}</span>
                  </div>
                  <div class="wgroup-chips">
                    {shown.map((g) => chip(g, target.key === "runtime" ? "other" : `cause-bg-${g.cause || "SYNC_WAIT"}`, false))}
                    {w.goroutines.length > shown.length && <span class="more">+{w.goroutines.length - shown.length}</span>}
                  </div>
                </div>
              )
            })}
          </div>
        )
      })}
      {total === 0 && <p class="faint">no parked goroutine at this moment</p>}
    </div>
  )
}

interface ChipProps {
  g: GoroutineState
  dot: string
  wide: boolean
  wait: string
  focus: number
  leak?: ReplayLeak
  replay: Replay
  onSelect: (g: number) => void
}

// Chip は G 1 つ。丸の色は主因、赤い輪はリーク。押すとその G の診断へ移る
function Chip({ g, dot, wide, wait, focus, leak, replay, onSelect }: ChipProps) {
  const entry = replay.entryOf(g.g)
  const tip = [
    `G${g.g}${leak ? ` (leaked · ${leak.certainty} · ${leak.count} of ${leak.of} at the same place)` : ""}`,
    `entry: ${entry || "?"}`,
    g.fn && `now at: ${g.fn}`,
    g.reason && `wait reason: ${g.reason}`,
    g.wokenBy ? `woken by G${g.wokenBy}` : "",
    "click to diagnose this goroutine",
  ].filter(Boolean).join("\n")
  return (
    <button type="button" class={`gchip${g.g === focus ? " is-focus" : ""}`} title={tip} onClick={() => onSelect(g.g)}>
      <span class={`gdot ${dot}${leak ? " is-leaked" : ""}`}></span>
      G{g.g}
      {wide && <small>{short(entry) || "entry unknown"}</small>}
      {wait && <small class="wait" title={`waiting in the run queue for ${wait}`}>{wait}</small>}
    </button>
  )
}
