import { useEffect, useMemo, useRef, useState } from "preact/hooks"
import type { GMPReplayProps, ReplayLeak, ReplayWindow } from "../gen/types"
import { Replay, type GoroutineState, type SchedView } from "./engine"
import { clock, duration, short } from "./format"
import { Signal, useSignal } from "./signal"

export interface GmpReplayIslandProps extends GMPReplayProps {
  windows: Signal<ReplayWindow[]>
  onSelect: (goroutine: number, from: string, to: string) => void
  onNavigate: (url: string) => void
}

// 再生の速さは trace の 1 秒をブラウザで何秒かけて見せるか。スケジューラの動きは µs の単位なので、既定は 100 倍に引き延ばす
const SPEEDS = [1000, 100, 10, 1] as const
const TICK_MS = 50
const MAX_CHIPS = 48

export function GmpReplay(props: GmpReplayIslandProps) {
  const windows = useSignal(props.windows)
  const [index, setIndex] = useState(0)
  const [t, setT] = useState(0)
  const [playing, setPlaying] = useState(props.autoplay || props.live)
  const [slowdown, setSlowdown] = useState<number>(100)

  const current = windows[Math.min(index, windows.length - 1)]
  const replay = useMemo(() => (current ? new Replay(current) : undefined), [current])
  const leaks = useMemo(() => new Map((current?.leaks ?? []).map((l) => [l.g, l])), [current])

  // 最新を追っているあいだは、新しい窓が届いたら、見ている窓の端まで再生し終えたところでつなぐ
  useEffect(() => {
    if (props.live && windows.length > 0 && index < windows.length - 1 && replay && t >= replay.duration) {
      setIndex(index + 1)
      setT(0)
    }
  }, [windows.length, index, t, replay, props.live])

  useEffect(() => {
    if (!playing || !replay) return
    const id = setInterval(() => {
      setT((prev) => {
        const next = prev + (TICK_MS * 1e6) / slowdown
        if (next < replay.duration) return next
        if (index < windows.length - 1) return replay.duration
        if (!props.live) {
          setPlaying(false)
          if (props.nextUrl && props.autoplay) props.onNavigate(props.nextUrl)
        }
        return replay.duration
      })
    }, TICK_MS)
    return () => clearInterval(id)
  }, [playing, replay, slowdown, index, windows.length])

  if (!current || !replay) return <p class="faint">no execution trace to replay</p>
  const view = replay.seek(Math.min(t, replay.duration))
  const select = (g: number) => props.onSelect(g, current.from, current.to)

  return (
    <div class="gmp">
      <Controls
        t={t}
        duration={replay.duration}
        startedAtMs={current.startedAtMs}
        playing={playing}
        slowdown={slowdown}
        live={props.live}
        onPlay={() => {
          if (t >= replay.duration) setT(0)
          setPlaying(!playing)
        }}
        onSeek={(v) => {
          setPlaying(false)
          setT(v)
        }}
        onStep={(dir) => {
          setPlaying(false)
          const v = dir > 0 ? replay.nextChangeAfter(t) : replay.prevChangeBefore(t)
          if (v !== undefined) setT(v)
        }}
        onSpeed={setSlowdown}
        prevUrl={props.prevUrl}
        nextUrl={props.nextUrl}
        onNavigate={props.onNavigate}
      />
      {current.truncated && <p class="hint-box small">This window has too many state changes; the replay shows only the first part.</p>}
      <Diagram view={view} replay={replay} focus={props.focusGoroutine} leaks={leaks} onSelect={select} />
      <Timeline replay={replay} t={t} focus={props.focusGoroutine} onSeek={(v) => { setPlaying(false); setT(v) }} />
      <RecentChanges replay={replay} t={t} startedAtMs={current.startedAtMs} onSelect={select} />
    </div>
  )
}

interface ControlsProps {
  t: number
  duration: number
  startedAtMs: number
  playing: boolean
  slowdown: number
  live: boolean
  prevUrl: string
  nextUrl: string
  onPlay: () => void
  onSeek: (t: number) => void
  onStep: (dir: number) => void
  onSpeed: (s: number) => void
  onNavigate: (url: string) => void
}

function Controls(p: ControlsProps) {
  return (
    <div class="gmp-controls">
      {p.prevUrl && <button class="btn ghost" title="the previous window" onClick={() => p.onNavigate(p.prevUrl)}>⏮</button>}
      <button class="btn ghost" title="the previous state change" onClick={() => p.onStep(-1)}>◀</button>
      <button class="btn primary" onClick={p.onPlay}>{p.playing ? "❚❚ pause" : "▶ play"}</button>
      <button class="btn ghost" title="the next state change" onClick={() => p.onStep(1)}>▶</button>
      {p.nextUrl && <button class="btn ghost" title="the next window" onClick={() => p.onNavigate(p.nextUrl)}>⏭</button>}
      <input class="grow" type="range" min={0} max={p.duration} step={Math.max(1, Math.floor(p.duration / 2000))} value={p.t}
        onInput={(e) => p.onSeek(Number((e.target as HTMLInputElement).value))} />
      <span class="mono">{clock(p.startedAtMs, p.t)}</span>
      <span class="mono faint">+{duration(p.t)}</span>
      <select value={p.slowdown} title="how much the replay is slowed down" onChange={(e) => p.onSpeed(Number((e.target as HTMLSelectElement).value))}>
        {SPEEDS.map((s) => <option value={s}>{s === 1 ? "real time" : `1/${s}`}</option>)}
      </select>
      {p.live && <span class="live-dot" title="following the latest">live</span>}
    </div>
  )
}

interface DiagramProps {
  view: SchedView
  replay: Replay
  focus: number
  leaks: Map<number, ReplayLeak>
  onSelect: (g: number) => void
}

function Diagram({ view, replay, focus, leaks, onSelect }: DiagramProps) {
  const chip = (g: GoroutineState) => <G key={g.g} g={g} replay={replay} focus={focus} leak={leaks.get(g.g)} onSelect={onSelect} />
  // 数千の G が並ぶと描き直しが追いつかないので、まとまりごとに先頭だけを描き、残りは数で出す
  const chips = (gs: GoroutineState[]) => (
    <>
      {gs.slice(0, MAX_CHIPS).map(chip)}
      {gs.length > MAX_CHIPS && <span class="faint small">+{gs.length - MAX_CHIPS}</span>}
    </>
  )
  return (
    <div class="gmp-diagram">
      {view.stw && <div class="gmp-stw">stop-the-world: {view.stw}</div>}
      <div class="gmp-row">
        <div class="gmp-global">
          <h4>global run queue <span class="faint">{view.globalQueue.length}</span></h4>
          <div class="gmp-chips">{chips(view.globalQueue)}</div>
        </div>
        <div class="gmp-meta faint small">
          <div>{view.live} goroutines</div>
          {view.gcMark && <div class="gmp-gc">GC mark</div>}
        </div>
      </div>
      <div class="gmp-procs">
        {view.procs.map((p) => (
          <div class={`gmp-proc ${p.running ? "running" : "idle"}`} key={p.proc}>
            <div class="gmp-proc-head">
              <strong>P{p.proc}</strong>
              <span class="mono faint">{p.running ? `M${p.thread}` : "idle"}</span>
            </div>
            <div class="gmp-current">
              {p.current ? chip(p.current) : <span class="faint small">—</span>}
              {p.inSyscall && <span class="badge note small">syscall</span>}
            </div>
            <div class="gmp-local">
              <span class="faint small">local run queue {p.localQueue.length}</span>
              <div class="gmp-chips">{chips(p.localQueue)}</div>
            </div>
          </div>
        ))}
      </div>
      {view.handedOff.length > 0 && (
        <div class="gmp-handedoff">
          <h4>in a syscall on an M without a P</h4>
          <div class="gmp-chips">{chips(view.handedOff)}</div>
        </div>
      )}
      <div class="gmp-waiting">
        <h4>waiting</h4>
        {view.waiting.length === 0 && <span class="faint small">none</span>}
        {view.waiting.slice(0, 40).map((w) => (
          <div class="gmp-wait-group" key={`${w.entry}/${w.reason}`}>
            <div class="small"><span class="mono">{short(w.entry) || "(unknown entry)"}</span> <span class="reason">{w.reason || "unknown"}</span> <span class="faint">{w.goroutines.length}</span></div>
            <div class="gmp-chips">{chips(w.goroutines)}</div>
          </div>
        ))}
      </div>
    </div>
  )
}

interface GProps {
  g: GoroutineState
  replay: Replay
  focus: number
  leak?: ReplayLeak
  onSelect: (g: number) => void
}

function G({ g, replay, focus, leak, onSelect }: GProps) {
  const title = [
    `G${g.g} ${short(replay.entryOf(g.g))}`,
    g.cause && `cause: ${g.cause}`,
    g.reason && `wait reason: ${g.reason}`,
    g.fn && `at: ${g.fn}`,
    g.wokenBy ? `woken by G${g.wokenBy}` : "",
    leak && `leak (${leak.certainty}): ${leak.count} of ${leak.of} at the same place`,
  ].filter(Boolean).join("\n")
  return (
    <button type="button" class={`gmp-g cause-bg-${g.cause || "NONE"}${g.g === focus ? " focus" : ""}${leak ? " leak" : ""}`} title={title} onClick={() => onSelect(g.g)}>
      G{g.g}
    </button>
  )
}

function Timeline({ replay, t, focus, onSeek }: { replay: Replay; t: number; focus: number; onSeek: (t: number) => void }) {
  const ref = useRef<HTMLDivElement>(null)
  const w = replay.window
  const pct = (ns: number) => `${(ns / Math.max(1, w.durationNs)) * 100}%`
  return (
    <div class="gmp-timeline" ref={ref} onClick={(e) => {
      const r = ref.current?.getBoundingClientRect()
      if (r) onSeek(((e.clientX - r.left) / r.width) * w.durationNs)
    }}>
      {w.procs.map((p, i) => (
        <div class="gmp-lane" key={p}>
          <span class="gmp-lane-label mono small">P{p}</span>
          <div class="gmp-lane-bar">
            {(w.lanes[i] ?? []).map((s) => (
              <span class={`gmp-span${s.g === focus ? " focus" : ""}`} style={{ left: pct(s.s), width: pct(Math.max(s.e - s.s, w.durationNs / 2000)) }} title={`G${s.g} ${duration(s.e - s.s)}`} />
            ))}
            {w.stw.map((r) => <span class="gmp-span stw" style={{ left: pct(r.s), width: pct(Math.max(r.e - r.s, w.durationNs / 1000)) }} />)}
          </div>
        </div>
      ))}
      <span class="gmp-cursor" style={{ left: `calc(2.5rem + (100% - 2.5rem) * ${t / Math.max(1, w.durationNs)})` }} />
    </div>
  )
}

function RecentChanges({ replay, t, startedAtMs, onSelect }: { replay: Replay; t: number; startedAtMs: number; onSelect: (g: number) => void }) {
  const w = replay.window
  const changes = replay.recentChanges(t, 8).reverse()
  return (
    <ol class="gmp-changes small">
      {changes.map((ev, i) => (
        <li key={`${ev.t}-${ev.g}-${i}`}>
          <span class="mono faint">{clock(startedAtMs, ev.t)}</span>{" "}
          <a href="#" onClick={(e) => { e.preventDefault(); onSelect(ev.g) }}>G{ev.g}</a>{" → "}
          <span>{["running", "local run queue", "global run queue", "waiting", "syscall", "dead"][ev.k]}</span>
          {ev.r >= 0 && <span class="reason"> {w.reasons[ev.r]}</span>}
          {ev.f >= 0 && <span class="mono faint"> {short(w.functions[ev.f])}</span>}
          {ev.w ? <span class="faint"> (woken by G{ev.w})</span> : null}
        </li>
      ))}
    </ol>
  )
}
