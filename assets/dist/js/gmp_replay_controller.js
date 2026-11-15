import { Controller } from "@hotwired/stimulus"

// サーバ (handler の gmpReplayJSON) と揃えたコード値
const PLACE_RUNNING = 0
const PLACE_LOCAL_RUN_QUEUE = 1
const PLACE_GLOBAL_RUN_QUEUE = 2
const PLACE_WAITING = 3
const PLACE_SYSCALL = 4
const PLACE_DEAD = 5
const EVENT_G = 0
const PROC_RUNNING = 1

const R = 14
const STEP = 31
const LEFT_W = 330
const COL_W = 132
const LEGEND_W = 170
const LRQ_MAX = 6
const ROW_MAX = 7
const LOG_SIZE = 8

const COLORS = {
  running: { fill: "#f2a08a", stroke: "#2e7d32", width: 3 },
  runnable: { fill: "#e8590c", stroke: "#a63f08", width: 1.5 },
  waiting: { fill: "#4a4a4a", stroke: "#1f1f1f", width: 1.5 },
  syscall: { fill: "#d63a1f", stroke: "#7d1a0b", width: 1.5 },
  dead: { fill: "#ffffff", stroke: "#222222", width: 1.5 },
}
const PROC_COLORS = {
  idle: { fill: "#f6d0c8", stroke: "#2b7a78" },
  running: { fill: "#f2a08a", stroke: "#2e7d32" },
}

// 観測窓 1 つ分の trace を時間軸で再生し、GMP の概念図 (global / local run queue / P / M / 待機 / free G list) として描く。
// 新しい窓が SSE で届いても再生中は割り込まず、今の窓を最後まで再生してから切り替える。
export default class extends Controller {
  static targets = ["window", "stage", "seek", "clock", "play", "speed", "live", "pending", "log", "note"]

  connect() {
    this.playing = true
    this.current = null
    this.pending = null
    this.lastFrame = null
    this.tick = this.tick.bind(this)
    this.frame = requestAnimationFrame(this.tick)
  }

  disconnect() {
    cancelAnimationFrame(this.frame)
  }

  windowTargetConnected(el) {
    const data = JSON.parse(el.textContent)
    if (!this.current || (this.following() && this.ended)) {
      this.load(data)
    } else {
      this.pending = data
    }
    if (this.hasPendingTarget) this.pendingTarget.hidden = !this.pending
  }

  following() {
    return this.hasLiveTarget && this.liveTarget.checked
  }

  load(data) {
    this.current = data
    this.appLogs = [...data.logs].sort((a, b) => a.atNs - b.atNs)
    this.focus = data.focus || null
    this.leaked = new Set(data.leaked)
    this.entries = new Map(data.entries.map(([g, fn]) => [g, data.functions[fn]]))
    this.seekTarget.max = data.windowNs
    this.noteTarget.textContent = data.truncated ? "events were truncated: the replay stops before the end of the window" : ""
    this.reset()
    this.dirty = true
  }

  reset() {
    this.t = 0
    this.cursor = 0
    this.logCursor = 0
    this.focusReached = false
    this.seq = 0
    this.gs = new Map()
    this.procs = new Map(this.current.procs.map((p) => [p, { status: "idle", thread: -1 }]))
    this.log = []
    this.ended = false
    this.advance(0)
  }

  // --- actions ---

  togglePlay() {
    this.playing = !this.playing
    if (this.playing && this.ended) this.reset()
    this.playTarget.textContent = this.playing ? "pause" : "play"
  }

  seek() {
    this.playing = false
    this.playTarget.textContent = "play"
    this.advance(Number(this.seekTarget.value))
  }

  loadLatest() {
    if (!this.pending) return
    this.load(this.pending)
    this.pending = null
    if (this.hasPendingTarget) this.pendingTarget.hidden = true
  }

  // --- replay ---

  tick(now) {
    if (this.current && this.playing && this.lastFrame !== null && !this.ended) {
      const speed = Number(this.speedTarget.value)
      let next = Math.min(this.t + (now - this.lastFrame) * 1e6 * speed, this.current.windowNs)
      // クリックしたログの瞬間で一度止め、その時点の状態をじっくり見られるようにする
      if (this.focus && !this.focusReached && this.t < this.focus.atNs && next >= this.focus.atNs) {
        next = this.focus.atNs
        this.focusReached = true
        this.playing = false
        this.playTarget.textContent = "play"
      }
      this.advance(next)
      if (next >= this.current.windowNs) {
        this.ended = true
        if (this.following() && this.pending) this.loadLatest()
      }
    }
    this.lastFrame = now
    if (this.dirty) this.render()
    this.frame = requestAnimationFrame(this.tick)
  }

  advance(t) {
    if (t < this.t) {
      this.reset()
    }
    const events = this.current.events
    // 同じ時刻ならアプリのログを先に流す (ログを書いた直後にブロックする、という順に読めるように)
    while (true) {
      const nextLog = this.logCursor < this.appLogs.length ? this.appLogs[this.logCursor] : null
      const nextEv = this.cursor < events.length ? events[this.cursor] : null
      if (nextLog && nextLog.atNs <= t && (!nextEv || nextLog.atNs <= nextEv[0])) {
        this.applyLog(nextLog)
        this.logCursor++
      } else if (nextEv && nextEv[0] <= t) {
        this.apply(nextEv)
        this.cursor++
      } else {
        break
      }
    }
    this.t = t
    this.dirty = true
  }

  apply(ev) {
    if (ev[1] === EVENT_G) {
      const [at, , g, place, proc, thread, reason, fn] = ev
      // スタックが載らないイベントもあるので、今いる関数は最後に分かったものを持ち越す
      const current = fn >= 0 ? this.current.functions[fn] : this.gs.get(g)?.fn
      this.gs.set(g, { place, proc, thread, reason, fn: current, seq: this.seq++ })
      const at_ = fn >= 0 ? ` at ${short(current)}` : ""
      this.pushLog(at, `G${g} [${this.label(g)}] → ${this.describePlace(place, proc, thread, reason)}${at_}`)
    } else {
      const [at, , p, status, thread] = ev
      const running = status === PROC_RUNNING
      this.procs.set(p, { status: running ? "running" : "idle", thread: running ? thread : -1 })
      this.pushLog(at, running ? `P${p} acquired by M${thread}` : `P${p} → idle`)
    }
  }

  describePlace(place, proc, thread, reason) {
    switch (place) {
      case PLACE_RUNNING: return `running on P${proc} / M${thread}`
      case PLACE_LOCAL_RUN_QUEUE: return `local run queue of P${proc}`
      case PLACE_GLOBAL_RUN_QUEUE: return "global run queue"
      case PLACE_WAITING: return `waiting (${this.current.reasons[reason]})`
      case PLACE_SYSCALL: return `syscall on M${thread}`
      case PLACE_DEAD: return "exited (free G list)"
    }
  }

  applyLog(l) {
    const who = l.g >= 0 ? `G${l.g} [${this.label(l.g)}] ` : ""
    const focused = this.focus && l.atNs === this.focus.atNs
    this.pushLog(l.atNs, `${focused ? "▶ " : ""}LOG ${l.level ? l.level.toUpperCase() + " " : ""}${who}${l.text}`, focused ? "log-focus" : "log-app")
  }

  label(g) {
    return short(this.entries.get(g)) || "?"
  }

  pushLog(at, text, cls = "") {
    this.log.push({ text: `+${(at / 1e6).toFixed(3)}ms  ${text}`, cls })
    if (this.log.length > LOG_SIZE) this.log.shift()
  }

  // --- view ---

  snapshot() {
    const procs = [...this.procs.entries()].sort((a, b) => a[0] - b[0]).map(([p, s]) => ({ proc: p, status: s.status, thread: s.thread, running: null, lrq: [] }))
    const byProc = new Map(procs.map((p) => [p.proc, p]))
    const grq = []
    const waiting = new Map()
    const handedOff = []
    const dead = []
    const inSyscall = new Map()

    const sorted = [...this.gs.entries()].sort((a, b) => a[1].seq - b[1].seq)
    for (const [g, s] of sorted) {
      switch (s.place) {
        case PLACE_RUNNING: { const p = byProc.get(s.proc); if (p) p.running = { g, kind: "running" }; break }
        case PLACE_LOCAL_RUN_QUEUE: byProc.get(s.proc)?.lrq.push(g); break
        case PLACE_GLOBAL_RUN_QUEUE: grq.push(g); break
        case PLACE_WAITING: {
          // 「何の処理をする G が、何を待って止まっているか」でまとめる
          const key = `${this.label(g)} — ${this.current.reasons[s.reason]}`
          if (!waiting.has(key)) waiting.set(key, [])
          waiting.get(key).push(g)
          break
        }
        case PLACE_SYSCALL:
          if (s.proc >= 0) inSyscall.set(s.proc, { g, thread: s.thread })
          else handedOff.push({ g, thread: s.thread })
          break
        case PLACE_DEAD: dead.push(g); break
      }
    }
    // P に乗った G が syscall 中なら、P は running のまま、その G をシステムコール中として置く (P にシステムコール中の状態はない)
    for (const [proc, sg] of inSyscall) {
      const p = byProc.get(proc)
      if (p && p.status === "running" && !p.running) {
        p.running = { g: sg.g, kind: "syscall" }
      } else {
        handedOff.push(sg)
      }
    }
    return { procs, grq, waiting, handedOff, dead }
  }

  render() {
    this.dirty = false
    if (!this.current) return
    const s = this.snapshot()
    const width = LEFT_W + s.procs.length * COL_W + LEGEND_W + 40
    const height = 600
    const out = []
    out.push(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="100%" style="min-width:${Math.round(width * 0.6)}px" font-family="ui-sans-serif, system-ui, sans-serif">`)
    this.drawLeft(out, s)
    this.drawCenter(out, s)
    this.drawLegend(out, LEFT_W + s.procs.length * COL_W + 40)
    out.push("</svg>")
    this.stageTarget.innerHTML = out.join("")

    this.seekTarget.value = this.t
    const focus = this.focus ? `  ▼ log at ${(this.focus.atNs / 1e6).toFixed(3)} ms` : ""
    this.clockTarget.textContent = `t = ${(this.t / 1e6).toFixed(3)} / ${(this.current.windowNs / 1e6).toFixed(1)} ms${focus}`
    this.logTarget.innerHTML = this.log.map((l) => `<span class="${l.cls}">${esc(l.text)}</span>`).join("\n")
  }

  drawLeft(out, s) {
    const x = 10
    const waitingRows = [...s.waiting.entries()].sort((a, b) => b[1].length - a[1].length).slice(0, 5)
    box(out, x, 10, LEFT_W - 30, 250, "waiting (sudog etc.)")
    waitingRows.forEach(([key, gs], i) => {
      const y = 62 + i * 42
      out.push(`<text x="${x + 10}" y="${y - 17}" font-size="11" fill="#555"><title>${esc(key)}</title>${esc(truncate(key, 46))} (${gs.length})</text>`)
      this.row(out, gs, x + 10 + R, y, "waiting", ROW_MAX)
    })

    box(out, x, 275, LEFT_W - 30, 90, `free G list (exited in window: ${s.dead.length})`)
    this.row(out, s.dead.slice(-ROW_MAX), x + 10 + R, 325, "dead", ROW_MAX)

    const idle = s.procs.filter((p) => p.status === "idle")
    box(out, x, 380, LEFT_W - 30, 90, `idle P list (${idle.length})`)
    idle.slice(0, 8).forEach((p, i) => triangle(out, x + 30 + i * 34, 440, 28, "idle", `P${p.proc}`))

    box(out, x, 485, LEFT_W - 30, 100, `syscall, P handed off (${s.handedOff.length})`)
    s.handedOff.slice(0, 4).forEach((h, i) => {
      const cx = x + 26 + i * 70
      this.circle(out, cx, 545, h.g, "syscall")
      line(out, cx + R, 545, cx + 26, 545)
      mbox(out, cx + 26, 532, `M${h.thread}`)
      out.push(`<text x="${cx - R}" y="${573}" font-size="9" fill="#555">${esc(truncate(this.label(h.g), 12))}</text>`)
    })
  }

  drawCenter(out, s) {
    const x0 = LEFT_W
    const width = s.procs.length * COL_W
    box(out, x0, 10, width, 90, `global run queue (${s.grq.length})`)
    this.row(out, s.grq, x0 + 20 + R, 62, "runnable", Math.max(1, Math.floor((width - 60) / STEP)))

    s.procs.forEach((p, i) => {
      const cx = x0 + i * COL_W + COL_W / 2
      const lrqTop = 120
      const lrqH = LRQ_MAX * STEP + 34
      box(out, cx - COL_W / 2 + 8, lrqTop, COL_W - 16, lrqH, `local run queue (${p.lrq.length})`)
      const shown = p.lrq.slice(0, LRQ_MAX)
      shown.forEach((g, j) => this.circle(out, cx, lrqTop + lrqH - R - 6 - j * STEP, g, "runnable"))
      if (p.lrq.length > LRQ_MAX) out.push(`<text x="${cx + R + 4}" y="${lrqTop + 30}" font-size="11" fill="#555">+${p.lrq.length - LRQ_MAX}</text>`)

      const py = lrqTop + lrqH + 44
      line(out, cx, lrqTop + lrqH, cx, py - 26)
      triangle(out, cx, py, 34, p.status, `P${p.proc}`)

      if (p.status !== "idle") {
        const my = py + 52
        line(out, cx, py + 10, cx, my)
        mbox(out, cx - 18, my, p.thread >= 0 ? `M${p.thread}` : "M")
        if (p.running) {
          line(out, cx - 18, my + 13, cx - 30, my + 13)
          this.circle(out, cx - 30 - R, my + 13, p.running.g, p.running.kind)
        }
        out.push(`<ellipse cx="${cx}" cy="${my + 58}" rx="34" ry="12" fill="#ddd"/><text x="${cx}" y="${my + 62}" font-size="11" text-anchor="middle" fill="#333">CPU</text>`)
        if (p.running) {
          const label = this.label(p.running.g)
          out.push(`<text x="${cx}" y="${my + 88}" font-size="10" text-anchor="middle" fill="#222"><title>${esc(label)}</title>${esc(truncate(label, 17))}</text>`)
        }
      }
    })
  }

  drawLegend(out, x) {
    const items = [["running", "_Grunning"], ["runnable", "_Grunnable"], ["waiting", "_Gwaiting"], ["syscall", "_Gsyscall"], ["dead", "_Gdead"]]
    items.forEach(([kind, label], i) => {
      const y = 30 + i * 38
      const c = COLORS[kind]
      out.push(`<circle cx="${x + R}" cy="${y}" r="${R}" fill="${c.fill}" stroke="${c.stroke}" stroke-width="${c.width}"/>`)
      out.push(`<text x="${x + 2 * R + 8}" y="${y + 4}" font-size="13">${label}</text>`)
    })
    out.push(`<circle cx="${x + R}" cy="${30 + 5 * 38}" r="${R}" fill="${COLORS.waiting.fill}" stroke="#ff3b30" stroke-width="3" stroke-dasharray="4 2"/>`)
    out.push(`<text x="${x + 2 * R + 8}" y="${34 + 5 * 38}" font-size="13">leaked</text>`)
    ;[["idle", "_Pidle"], ["running", "_Prunning"]].forEach(([status, label], i) => {
      const y = 290 + i * 46
      triangle(out, x + R, y, 30, status, "P")
      out.push(`<text x="${x + 2 * R + 8}" y="${y + 4}" font-size="13">${label}</text>`)
    })
  }

  row(out, gs, x, y, kind, max) {
    gs.slice(0, max).forEach((g, i) => this.circle(out, x + i * STEP, y, g, kind))
    if (gs.length > max) out.push(`<text x="${x + max * STEP - 6}" y="${y + 4}" font-size="11" fill="#555">+${gs.length - max}</text>`)
  }

  circle(out, cx, cy, g, kind) {
    const c = COLORS[kind]
    const leaked = this.leaked.has(g)
    const focused = this.focus && this.focus.g === g
    const stroke = leaked ? `stroke="#ff3b30" stroke-width="3" stroke-dasharray="4 2"` : `stroke="${c.stroke}" stroke-width="${c.width}"`
    // クリックしたログを書いた G は、どこにいても見つけられるよう外側を青い輪で囲む
    if (focused) out.push(`<circle cx="${cx}" cy="${cy}" r="${R + 5}" fill="none" stroke="#1e6fff" stroke-width="3"/>`)
    const text = kind === "dead" ? "#222" : "#fff"
    const s = this.gs.get(g)
    const tip = [`G${g}${leaked ? " (leaked)" : ""}${focused ? " (wrote the clicked log)" : ""}`, `entry: ${this.entries.get(g) || "?"}`]
    if (s?.fn) tip.push(`now at: ${s.fn}`)
    out.push(`<g><title>${esc(tip.join("\n"))}</title><circle cx="${cx}" cy="${cy}" r="${R}" fill="${c.fill}" ${stroke}/>`)
    out.push(`<text x="${cx}" y="${cy + 3}" font-size="${g > 9999 ? 6 : 8}" text-anchor="middle" fill="${text}">G${g}</text></g>`)
  }
}

function box(out, x, y, w, h, label) {
  out.push(`<rect x="${x}" y="${y}" width="${w}" height="${h}" fill="none" stroke="#777" stroke-dasharray="4 3"/>`)
  out.push(`<text x="${x + 6}" y="${y + 18}" font-size="14" font-weight="700">${esc(label)}</text>`)
}

function triangle(out, cx, cy, size, status, label) {
  const c = PROC_COLORS[status]
  const h = size * 0.87
  out.push(`<polygon points="${cx},${cy - h / 2 - 4} ${cx - size / 2},${cy + h / 2 - 4} ${cx + size / 2},${cy + h / 2 - 4}" fill="${c.fill}" stroke="${c.stroke}" stroke-width="2"/>`)
  out.push(`<text x="${cx}" y="${cy + h / 2 - 9}" font-size="${size > 30 ? 10 : 9}" text-anchor="middle" fill="#fff" font-weight="700">${label}</text>`)
}

function mbox(out, x, y, label) {
  out.push(`<rect x="${x}" y="${y}" width="36" height="26" fill="#fff" stroke="#222" stroke-width="1.5"/>`)
  out.push(`<text x="${x + 18}" y="${y + 17}" font-size="11" text-anchor="middle" font-weight="700">${label}</text>`)
}

function line(out, x1, y1, x2, y2) {
  out.push(`<line x1="${x1}" y1="${y1}" x2="${x2}" y2="${y2}" stroke="#333" stroke-width="1.2"/>`)
}

// パッケージパスを落とす ("net/http.(*conn).serve" → "http.(*conn).serve")。domain.ShortFunctionName と同じ規則
function short(fn) {
  if (!fn) return ""
  const i = fn.lastIndexOf("/")
  return i >= 0 ? fn.slice(i + 1) : fn
}

function truncate(s, n) {
  return s.length <= n ? s : s.slice(0, n - 1) + "…"
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c])
}
