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

const LRQ_MAX = 8
const GRQ_MAX = 24
// 待っている相手の種類ごとに見せる組の数
const WAITING_GROUPS_MAX = 4
// 止まっている G が待っている相手の種類と、runtime の中でぶら下がっている場所。trace の待機理由で見分ける
const WAIT_TARGETS = [
  { key: "channel", label: "channel", where: "recvq / sendq" },
  { key: "sync", label: "lock / sync", where: "semaphore" },
  { key: "network", label: "network", where: "netpoller" },
  { key: "timer", label: "timer", where: "sleep" },
  { key: "runtime", label: "runtime / unknown", where: "GC, system goroutines" },
]
// 再生中に 1 歩進める間隔
const PLAY_INTERVAL_MS = 700
// P timeline のクリックを、近くの帯に吸い付かせる距離 (px)。帯は µs 単位で 1〜2px しかなく、ちょうど押すのは難しい
const SNAP_PX = 10

// 内訳の分類は server の domain.causeOfPlace と揃える (syscall は IO_WAIT に数える)
const CAUSES = ["RUNNING", "CPU_WAIT", "BLOCKED", "IO_WAIT", "SLEEP", "GC"]
// 図の G の置き場ごとの状態。丸の色に使う
const KIND_CAUSES = { running: "RUNNING", runnable: "CPU_WAIT", syscall: "IO_WAIT" }

// 観測窓 1 つ分の trace を、出来事 (G や P の状態が変わった瞬間) ごとに 1 つずつ進めながら、GMP の図として描く。
// 時間で流すと 1 秒に何十もの出来事が過ぎて追えないので、既定では止めておき、ボタンか再生で 1 つずつ進める。
// focus の G (ログを書いた G・選んだ G) があれば、その G の窓の中での状態の推移と内訳を P の図の下に出す
export default class extends Controller {
  static targets = ["window", "stage", "seek", "clock", "play", "note", "focusOnly", "focusOnlyLabel", "lanes", "period"]

  connect() {
    this.playing = false
    // Stimulus は connect より先に target の接続 (windowTargetConnected) を呼ぶので、最初から HTML にある窓を上書きしない
    this.current ??= null
    this.timer = null
    this.onStageClick = this.onStageClick.bind(this)
    this.onLanesClick = this.onLanesClick.bind(this)
    this.onResize = () => this.fitSide()
    this.stageTarget.addEventListener("click", this.onStageClick)
    this.lanesTarget.addEventListener("click", this.onLanesClick)
    window.addEventListener("resize", this.onResize)
  }

  disconnect() {
    this.stop()
    this.stageTarget.removeEventListener("click", this.onStageClick)
    this.lanesTarget.removeEventListener("click", this.onLanesClick)
    window.removeEventListener("resize", this.onResize)
  }

  windowTargetConnected(el) {
    this.load(JSON.parse(el.textContent))
  }

  load(data) {
    this.current = data
    this.appLogs = [...data.logs].sort((a, b) => a.atNs - b.atNs)
    this.leaked = new Set(data.leaked)
    this.entries = new Map(data.entries.map(([g, fn]) => [g, data.functions[fn]]))
    this.serverFocus = data.focus || null
    // ログから来たときだけ、focus の時刻にログがある (Goroutine 画面から来たときは、見ている時点を指すだけ)
    this.focusLog = this.serverFocus ? this.appLogs.find((l) => l.atNs === this.serverFocus.atNs) || null : null
    this.focusG = this.serverFocus && this.serverFocus.g >= 0 ? this.serverFocus.g : null
    this.seekTarget.max = data.windowNs
    this.noteTarget.textContent = data.truncated ? "events were truncated: the replay stops before the end of the window" : ""
    this.track = this.focusG !== null ? this.buildTrack(this.focusG) : null
    this.reset()
    // ログから来たときは、ログを書いた瞬間から見せる。そうでなければ、窓の頭は何も走っていないことが多いので、最初に G が走る瞬間から見せる
    this.advance(this.serverFocus ? this.serverFocus.atNs : this.firstRunningTime())
    this.drawWindow()
    this.drawLanes()
    this.render()
  }

  firstRunningTime() {
    const ev = this.current.events.find((e) => e[1] === EVENT_G && e[3] === PLACE_RUNNING)
    return ev ? ev[0] : 0
  }

  reset() {
    this.t = 0
    this.cursor = 0
    this.logCursor = 0
    this.seq = 0
    this.gs = new Map()
    this.procs = new Map(this.current.procs.map((p) => [p, { status: "idle", thread: -1 }]))
    this.changed = new Set()
    this.advance(0)
  }

  // --- actions ---

  togglePlay() {
    this.playing ? this.stop() : this.play()
  }

  play() {
    if (this.atEnd()) this.reset()
    this.playing = true
    this.setPlayIcon("❚❚", "pause")
    const step = () => {
      if (!this.playing) return
      if (!this.stepForward()) {
        this.stop()
        return
      }
      this.timer = setTimeout(step, PLAY_INTERVAL_MS)
    }
    step()
  }

  setPlayIcon(icon, label) {
    this.playTarget.textContent = icon
    this.playTarget.title = label
    this.playTarget.setAttribute("aria-label", label)
  }

  stop() {
    this.playing = false
    clearTimeout(this.timer)
    if (this.hasPlayTarget) this.setPlayIcon("▶", "play")
  }

  next() {
    this.stop()
    this.stepForward()
  }

  prev() {
    this.stop()
    const t = this.previousTime()
    if (t !== null) {
      this.reset()
      this.advance(t)
      this.render()
    }
  }

  seek() {
    this.stop()
    this.advance(Number(this.seekTarget.value))
    this.render()
  }

  // G を選ぶと、その G を focus にして状態の推移と内訳を出す。focus の G の帯をクリックするとその時刻へ移る
  onStageClick(e) {
    const strip = e.target.closest("[data-strip]")
    if (strip) {
      this.seekTo(this.timeAt(e, strip))
      return
    }
    if (e.target.closest("a")) return
    const chip = e.target.closest("[data-g]")
    if (!chip) return
    const g = Number(chip.dataset.g)
    this.focusG = this.focusG === g ? null : g
    this.track = this.focusG !== null ? this.buildTrack(this.focusG) : null
    this.drawLanes()
    this.render()
  }

  // P timeline をクリックすると、その時刻へ移る。帯から少し外れていれば、いちばん近い帯の始まり (G が P に乗った瞬間) に合わせる
  onLanesClick(e) {
    const track = e.target.closest("[data-track]")
    if (!track || !this.current) return
    const t = this.timeAt(e, track)
    const tolerance = (SNAP_PX / track.getBoundingClientRect().width) * this.current.windowNs
    this.seekTo(snapToSpan(this.current.lanes?.[Number(track.dataset.track)] || [], t, tolerance))
  }

  timeAt(e, el) {
    const rect = el.getBoundingClientRect()
    return Math.round(((e.clientX - rect.left) / rect.width) * this.current.windowNs)
  }

  seekTo(t) {
    this.stop()
    this.advance(Math.max(0, Math.min(t, this.current.windowNs)))
    this.render()
  }

  // --- replay ---

  focusOnly() {
    return this.hasFocusOnlyTarget && this.focusOnlyTarget.checked && this.focusG !== null
  }

  // focus の G だけで進めるときは、その G の出来事とログだけを 1 歩として数える
  relevantEvent(ev) {
    return !this.focusOnly() || (ev[1] === EVENT_G && ev[2] === this.focusG)
  }

  relevantLog(l) {
    return !this.focusOnly() || l.g === this.focusG
  }

  // stepForward は次の 1 歩の時刻まで進める。もう出来事がなければ窓の終わりまで進めて false を返す
  stepForward() {
    const events = this.current.events
    let next = Infinity
    for (let i = this.cursor; i < events.length; i++) {
      if (this.relevantEvent(events[i])) { next = events[i][0]; break }
    }
    for (let i = this.logCursor; i < this.appLogs.length; i++) {
      if (this.relevantLog(this.appLogs[i])) { next = Math.min(next, this.appLogs[i].atNs); break }
    }
    if (next === Infinity) {
      this.advance(this.current.windowNs)
      this.render()
      return false
    }
    this.advance(next)
    this.render()
    return true
  }

  previousTime() {
    const events = this.current.events
    let prev = null
    for (let i = this.cursor - 1; i >= 0; i--) {
      const at = events[i][0]
      if (at < this.t && this.relevantEvent(events[i])) { prev = at; break }
    }
    for (let i = this.logCursor - 1; i >= 0; i--) {
      const l = this.appLogs[i]
      if (l.atNs < this.t && this.relevantLog(l)) { prev = Math.max(prev ?? 0, l.atNs); break }
    }
    return prev ?? (this.t > 0 ? 0 : null)
  }

  atEnd() {
    return this.cursor >= this.current.events.length && this.logCursor >= this.appLogs.length
  }

  advance(t) {
    if (t < this.t) this.reset()
    this.changed = new Set()
    const events = this.current.events
    // 同じ時刻ならアプリのログを先に流す (ログを書いた直後にブロックする、という順に読めるように)
    while (true) {
      const nextLog = this.logCursor < this.appLogs.length ? this.appLogs[this.logCursor] : null
      const nextEv = this.cursor < events.length ? events[this.cursor] : null
      if (nextLog && nextLog.atNs <= t && (!nextEv || nextLog.atNs <= nextEv[0])) {
        this.logCursor++
      } else if (nextEv && nextEv[0] <= t) {
        this.apply(nextEv)
        this.cursor++
      } else {
        break
      }
    }
    this.t = t
  }

  apply(ev) {
    if (ev[1] === EVENT_G) {
      const [, , g, place, proc, thread, reason, fn] = ev
      // スタックが載らないイベントもあるので、今いる関数は最後に分かったものを持ち越す
      const current = fn >= 0 ? this.current.functions[fn] : this.gs.get(g)?.fn
      this.gs.set(g, { place, proc, thread, reason, fn: current, seq: this.seq++ })
      this.changed.add(g)
    } else {
      const [, , p, status, thread] = ev
      const running = status === PROC_RUNNING
      this.procs.set(p, { status: running ? "running" : "idle", thread: running ? thread : -1 })
      this.changed.add(`P${p}`)
    }
  }

  describePlace(place, proc, thread, reason) {
    switch (place) {
      case PLACE_RUNNING: return `running on P${proc} / M${thread}`
      case PLACE_LOCAL_RUN_QUEUE: return `local run queue of P${proc}`
      case PLACE_GLOBAL_RUN_QUEUE: return "global run queue"
      case PLACE_WAITING: return `waiting (${this.current.reasons[reason]})`
      case PLACE_SYSCALL: return `syscall on M${thread}`
      case PLACE_DEAD: return "exited"
    }
  }

  // label は G の入口の関数。窓より前からいた G は trace に入口が載らないことがある
  label(g) {
    return short(this.entries.get(g)) || "entry unknown"
  }


  // --- focus の G ---

  // buildTrack は窓の中の G の状態の推移を区間に分け、主因ごとの時間を合計する
  buildTrack(g) {
    const windowNs = this.current.windowNs
    const segments = []
    let open = null
    for (const ev of this.current.events) {
      if (ev[1] !== EVENT_G || ev[2] !== g) continue
      const [at, , , place, , , reason, fn] = ev
      if (open) {
        open.end = at
        if (open.end > open.start) segments.push(open)
      }
      const cause = place === PLACE_DEAD ? "EXITED" : causeOf(place, this.current.reasons[reason])
      const prevFn = open?.fn
      open = cause ? { start: at, end: windowNs, cause, fn: fn >= 0 ? this.current.functions[fn] : prevFn, reason: place === PLACE_WAITING ? this.current.reasons[reason] : "" } : null
    }
    if (open && open.end > open.start) segments.push(open)
    const totals = Object.fromEntries(CAUSES.map((c) => [c, 0]))
    for (const s of segments) if (s.cause in totals) totals[s.cause] += s.end - s.start
    const recorded = Object.values(totals).reduce((a, b) => a + b, 0)
    const logs = this.appLogs.filter((l) => l.g === g).map((l) => l.atNs)
    return { g, segments, totals, recorded, logs }
  }

  // --- view ---

  snapshot() {
    const procs = [...this.procs.entries()].sort((a, b) => a[0] - b[0]).map(([p, s]) => ({ proc: p, status: s.status, thread: s.thread, running: null, lrq: [] }))
    const byProc = new Map(procs.map((p) => [p.proc, p]))
    const grq = []
    const waiting = new Map()
    const handedOff = []
    let dead = 0
    const inSyscall = new Map()

    const sorted = [...this.gs.entries()].sort((a, b) => a[1].seq - b[1].seq)
    for (const [g, s] of sorted) {
      switch (s.place) {
        case PLACE_RUNNING: { const p = byProc.get(s.proc); if (p) p.running = { g, kind: "running" }; break }
        case PLACE_LOCAL_RUN_QUEUE: byProc.get(s.proc)?.lrq.push(g); break
        case PLACE_GLOBAL_RUN_QUEUE: grq.push(g); break
        case PLACE_WAITING: {
          // 「何の処理をする G が、何を待って止まっているか」でまとめる
          const key = `${this.label(g)}\u0000${this.current.reasons[s.reason]}`
          if (!waiting.has(key)) waiting.set(key, [])
          waiting.get(key).push(g)
          break
        }
        case PLACE_SYSCALL:
          if (s.proc >= 0) inSyscall.set(s.proc, { g, thread: s.thread })
          else handedOff.push({ g, thread: s.thread })
          break
        case PLACE_DEAD: dead++; break
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
    if (!this.current) return
    const s = this.snapshot()
    const out = []
    // P の図と待ち行列・待機は隣に並べて 1 つの図として見せる。追っている G は P の図の下に置き、G を選んでも図が動かないようにする
    out.push(`<div class="gmp-board"><div class="gmp-main">`)
    this.drawProcs(out, s)
    if (this.track) this.drawFocus(out)
    out.push(`</div>`)
    this.drawSide(out, s)
    out.push(`</div>`)
    this.stageTarget.innerHTML = out.join("")
    this.fitSide()

    this.seekTarget.value = this.t
    const mark = this.focusLog ? `  ·  log at ${ms(this.focusLog.atNs)}` : ""
    // 出来事は µs 単位で並ぶので、1 歩進んだことが分かるよう今の時刻は細かく出す
    this.clockTarget.textContent = `${(this.t / 1e6).toFixed(3)}ms / ${ms(this.current.windowNs)}${mark}`
    this.drawFocusOnly()
    const at = pct(this.t, this.current.windowNs)
    this.lanesTarget.querySelectorAll("[data-cursor]").forEach((c) => { c.style.left = at })
  }

  // fitSide は右の列 (待ち行列・待機) を画面の下端までの高さに収める。組が多いときは列の中でスクロールさせ、ページは 1 画面に保つ
  fitSide() {
    const board = this.stageTarget.querySelector(".gmp-board")
    if (!board) return
    const top = board.getBoundingClientRect().top + window.scrollY
    board.style.setProperty("--side-height", `${Math.max(320, window.innerHeight - top - 60)}px`)
  }

  // drawFocusOnly は「追っている G だけで進める」の切り替えを、G を追っているときだけ押せるようにする
  drawFocusOnly() {
    if (!this.hasFocusOnlyTarget) return
    const following = this.focusG !== null
    this.focusOnlyTarget.disabled = !following
    if (!following) this.focusOnlyTarget.checked = false
    this.focusOnlyLabelTarget.textContent = following ? `step only through G${this.focusG}` : "step only through the followed G (click a G first)"
  }

  // drawWindow は窓の実時刻の時間帯を出す
  drawWindow() {
    const { startedAtNs: start, windowNs: w } = this.current
    this.periodTarget.textContent = start > 0 ? `replaying ${clockTime(start)} – ${clockTime(start + w)} (${ms(w)}) · ${this.current.procs.length} P` : ""
  }


  // drawLanes は P ごとに、窓の中で G が走った区間を帯にする。色は図の G と同じく入口の関数で決め、追っている G は目立たせる
  drawLanes() {
    const { procs, lanes, windowNs: w } = this.current
    const out = [`<div class="caption"><strong>CPU slots (P) over time</strong> · colored = a goroutine was running on that P (hover to see which), black = idle · click to jump to that moment</div><div class="lanes-grid">`]
    procs.forEach((p, i) => {
      const spans = lanes?.[i] || []
      let busy = 0
      out.push(`<span class="pname">P${p}</span><div class="lane-track" data-track="${i}">`)
      for (const [start, end, g] of spans) {
        busy += end - start
        const focus = g === this.focusG ? " is-focus" : ""
        out.push(`<span class="seg${focus}" style="left:${pct(start, w)};width:${pct(end - start, w)}" title="${esc(`G${g} ${this.label(g)}\n${ms(end - start)}`)}"></span>`)
      }
      out.push(`<span class="cursor" data-cursor></span></div><span class="util" title="share of the window this P was running a goroutine">${w ? Math.round((busy / w) * 100) : 0}%</span>`)
    })
    out.push(`<span></span><div class="lanes-axis"><span>0</span><span>${ms(w / 4)}</span><span>${ms(w / 2)}</span><span>${ms((w * 3) / 4)}</span><span>${ms(w)}</span></div><span class="util">busy</span>`)
    out.push(`</div>`)
    this.lanesTarget.innerHTML = out.join("")
  }

  drawFocus(out) {
    const tr = this.track
    const now = this.gs.get(tr.g)
    const nowCause = now ? causeOf(now.place, this.current.reasons[now.reason]) || "EXITED" : "UNRECORDED"
    const where = now ? this.describePlace(now.place, now.proc, now.thread, now.reason) : "not seen yet in this window"
    const at = now?.fn ? ` · ${esc(short(now.fn))}` : ""
    const href = this.current.startedAtNs > 0 ? `/goroutines/${tr.g}?at=${Math.round(this.current.startedAtNs + this.t)}` : null
    let wrote = ""
    if (this.focusLog && this.serverFocus.g === tr.g) {
      wrote = this.serverFocus.exact
        ? `<span class="badge fact" title="located exactly through the trace">wrote the log</span>`
        : `<span class="badge inference" title="the only goroutine running at the log's timestamp">probably wrote the log</span>`
    }

    out.push(`<div class="gmp-focus">`)
    out.push(`<div class="gmp-focus-head"><span class="gchip is-focus"><span class="gdot cause-bg-${nowCause}"></span>G${tr.g}</span><strong>${esc(this.label(tr.g))}</strong>${wrote}`)
    if (href) out.push(`<a class="gmp-focus-link" href="${href}" data-turbo-frame="_top">diagnose →</a>`)
    out.push(`</div>`)
    out.push(`<div class="gmp-focus-now"><span class="cause-dot cause-bg-${nowCause}"></span><span class="cause-name">${nowCause}</span><span class="muted">${esc(where)}${at}</span></div>`)

    // 状態の推移の帯。クリックでその時刻に移る
    const w = this.current.windowNs
    out.push(`<div class="gmp-strip" data-strip title="click to jump to that moment">`)
    for (const sg of tr.segments) {
      const tip = `${sg.cause}${sg.reason ? ` (${sg.reason})` : ""} ${ms(sg.end - sg.start)}${sg.fn ? `\n${short(sg.fn)}` : ""}`
      out.push(`<span class="seg cause-bg-${sg.cause}" style="left:${pct(sg.start, w)};width:${pct(sg.end - sg.start, w)}" title="${esc(tip)}"></span>`)
    }
    for (const at of tr.logs) out.push(`<span class="log-mark" style="left:${pct(at, w)}" title="log at ${ms(at)}"></span>`)
    out.push(`<span class="cursor" style="left:${pct(this.t, w)}"></span></div>`)
    out.push(`<div class="gmp-axis"><span>0</span><span>${ms(w / 2)}</span><span>${ms(w)}</span></div>`)

    out.push(`<div class="gmp-breakdown"><div class="caption">time by state in this ${ms(w)} window</div>`)
    for (const c of CAUSES) {
      const d = tr.totals[c]
      if (!d) continue
      const ratio = tr.recorded ? d / tr.recorded : 0
      out.push(`<div class="row"><span class="cause-dot cause-bg-${c}"></span><span class="name">${c}</span><span class="bar"><span class="cause-bg-${c}" style="width:${(ratio * 100).toFixed(1)}%"></span></span><span class="num">${ms(d)}</span><span class="num muted">${Math.round(ratio * 100)}%</span></div>`)
    }
    if (!tr.recorded) out.push(`<p class="muted">no state change of G${tr.g} is recorded in this window</p>`)
    out.push(`</div></div>`)
  }

  drawProcs(out, s) {
    out.push(`<div class="gmp-procs">`)
    for (const p of s.procs) {
      const changed = this.changed.has(`P${p.proc}`) ? " flash" : ""
      const empty = !p.running && p.lrq.length === 0 ? " empty" : ""
      out.push(`<div class="proc ${p.status}${empty}${changed}">`)
      out.push(`<div class="proc-head"><span class="pname">P${p.proc}</span>${p.status === "running" ? `<span class="mname">M${p.thread}</span>` : `<span class="idle-label">idle</span>`}</div>`)
      out.push(`<div class="proc-run">`)
      if (p.running) out.push(this.chip(p.running.g, p.running.kind, true))
      out.push(`</div><div class="proc-lrq">`)
      p.lrq.slice(0, LRQ_MAX).forEach((g) => out.push(this.chip(g, "runnable")))
      if (p.lrq.length > LRQ_MAX) out.push(`<span class="more">+${p.lrq.length - LRQ_MAX}</span>`)
      out.push(`</div></div>`)
    }
    out.push(`</div>`)
  }

  drawSide(out, s) {
    out.push(`<div class="gmp-side">`)
    out.push(`<div class="area" title="inferred from the execution trace: a goroutine readied by a P goes to that P's local run queue, one descheduled by preemption, yield or a syscall without P goes to the global run queue. runnext is not distinguished"><h4>global run queue <span>${s.grq.length}</span></h4><div class="chips">`)
    s.grq.slice(0, GRQ_MAX).forEach((g) => out.push(this.chip(g, "runnable")))
    if (s.grq.length > GRQ_MAX) out.push(`<span class="more">+${s.grq.length - GRQ_MAX}</span>`)
    out.push(`</div></div>`)

    // runtime にこういう置き場 (キュー) はない。G は自分の M に付いたままカーネルの中にいて、P だけが外されて別の M に渡っている。
    // キューと取り違えないよう、M と G の組で描く
    if (s.handedOff.length) {
      out.push(`<div class="area" title="not a queue: each goroutine stays on its M (OS thread) inside the kernel, and its P was handed to another M"><h4>M in syscall, P released <span>${s.handedOff.length}</span></h4><div class="m-rows">`)
      s.handedOff.forEach((h) => out.push(`<div class="m-row"><span class="mname">${h.thread >= 0 ? `M${h.thread}` : "M?"}</span><span class="faint">runs</span>${this.chip(h.g, "syscall")}</div>`))
      out.push(`</div></div>`)
    }

    // 止まっている G はスケジューラのどのキューにもおらず、待っている相手 (チャネル・セマフォ・netpoller・タイマー) の側からたどれるだけ。
    // trace には相手のアドレスが載らないので、相手の種類ごとに段を分け、その中を「関数 × 理由」でまとめる
    const total = [...s.waiting.values()].reduce((a, gs) => a + gs.length, 0)
    out.push(`<div class="area" title="parked goroutines are not in any scheduler queue; each one hangs on what it waits for"><h4>parked <span>${total}</span></h4>`)
    const sections = new Map(WAIT_TARGETS.map((t) => [t.key, []]))
    for (const entry of s.waiting.entries()) sections.get(waitTargetOf(entry[0].split("\u0000")[1])).push(entry)
    for (const target of WAIT_TARGETS) {
      const groups = sections.get(target.key).sort((a, b) => b[1].length - a[1].length)
      if (groups.length === 0) continue
      const count = groups.reduce((a, [, gs]) => a + gs.length, 0)
      out.push(`<div class="wtarget"><div class="wtarget-head"><span>${target.label}</span><span class="faint">${target.where}</span><span class="count">${count}</span></div>`)
      // focus の G がいる組は、件数が少なくても必ず見せる
      const shown = groups.slice(0, WAITING_GROUPS_MAX)
      const focusGroup = groups.find(([, gs]) => gs.includes(this.focusG))
      if (focusGroup && !shown.includes(focusGroup)) shown.push(focusGroup)
      for (const [key, gs] of shown) out.push(this.waitGroup(key, gs, target.key))
      if (groups.length > shown.length) out.push(`<p class="more">${groups.length - shown.length} more groups</p>`)
      out.push(`</div>`)
    }
    out.push(`</div>`)
    out.push(`<div class="area quiet">exited in this window <span>${s.dead}</span></div>`)
    out.push(`</div>`)
  }

  // waitGroup は同じ関数・同じ理由で止まっている G の組 1 行。runtime 内部の待ちは状態の色を付けず、空の丸にする
  waitGroup(key, gs, target) {
    const [fn, reason] = key.split("\u0000")
    const leaked = gs.filter((g) => this.leaked.has(g)).length
    const hasFocus = gs.includes(this.focusG)
    const changed = gs.some((g) => this.changed.has(g)) ? " flash" : ""
    const tip = `${fn} waiting on ${reason}\n${gs.slice(0, 30).map((g) => "G" + g).join(" ")}${gs.length > 30 ? " …" : ""}\nclick to follow one of them`
    const dot = target === "runtime" ? "gdot other" : `gdot cause-bg-${causeOf(PLACE_WAITING, reason)}`
    const out = [`<div class="wgroup${hasFocus ? " has-focus" : ""}${changed}" data-g="${hasFocus ? this.focusG : gs[0]}" title="${esc(tip)}">`]
    out.push(`<span class="${dot}"></span><span class="fn">${esc(fn)}<span class="reason">${esc(reason)}</span></span>`)
    if (leaked) out.push(`<span class="badge kind">${leaked} leaked</span>`)
    if (hasFocus) out.push(`<span class="gchip is-focus small"><span class="${dot}"></span>G${this.focusG}</span>`)
    out.push(`<span class="count">×${gs.length}</span></div>`)
    return out.join("")
  }

  // chip は G 1 つ。丸の色は状態 (内訳の帯と同じ色)。クリックするとその G を追う
  chip(g, kind, wide = false) {
    const fn = this.entries.get(g)
    const cls = ["gchip"]
    if (g === this.focusG) cls.push("is-focus")
    if (this.changed.has(g)) cls.push("flash")
    const now = this.gs.get(g)?.fn
    const tip = `G${g}${this.leaked.has(g) ? " (leaked)" : ""}\nentry: ${fn || "?"}${now ? `\nnow at: ${now}` : ""}\nclick to follow this goroutine`
    const text = wide ? `G${g}<small>${esc(this.label(g))}</small>` : `G${g}`
    return `<button type="button" class="${cls.join(" ")}" data-g="${g}" title="${esc(tip)}"><span class="gdot cause-bg-${KIND_CAUSES[kind]}${this.leaked.has(g) ? " is-leaked" : ""}"></span>${text}</button>`
  }
}

function waitTargetOf(reason) {
  if (["chan receive", "chan send", "select", "forever"].includes(reason)) return "channel"
  if (reason && reason.startsWith("sync")) return "sync"
  if (reason === "network" || reason === "IO wait") return "network"
  if (reason === "sleep") return "timer"
  return "runtime"
}

function causeOf(place, reason) {
  switch (place) {
    case PLACE_RUNNING: return "RUNNING"
    case PLACE_LOCAL_RUN_QUEUE:
    case PLACE_GLOBAL_RUN_QUEUE: return "CPU_WAIT"
    case PLACE_SYSCALL: return "IO_WAIT"
    case PLACE_WAITING:
      if (reason === "network" || reason === "IO wait") return "IO_WAIT"
      if (reason === "sleep") return "SLEEP"
      if (reason && reason.includes("GC")) return "GC"
      return "BLOCKED"
  }
  return null
}

// snapToSpan は t が帯の中ならそのまま、外なら tolerance 以内でいちばん近い帯の始まりを返す
function snapToSpan(spans, t, tolerance) {
  let best = null
  let bestDistance = tolerance
  for (const [start, end] of spans) {
    if (start <= t && t <= end) return t
    const distance = t < start ? start - t : t - end
    if (distance <= bestDistance) {
      best = start
      bestDistance = distance
    }
  }
  return best ?? t
}

function clockTime(ns) {
  const d = new Date(ns / 1e6)
  const pad = (v, n = 2) => String(v).padStart(n, "0")
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}

function ms(ns) {
  const v = ns / 1e6
  if (v >= 1000) return `${parseFloat((v / 1000).toFixed(2))}s`
  if (v >= 100) return `${v.toFixed(0)}ms`
  if (v >= 1) return `${v.toFixed(1)}ms`
  return `${(ns / 1e3).toFixed(0)}µs`
}

function pct(v, total) {
  return `${((v / total) * 100).toFixed(3)}%`
}

// パッケージパスを落とす ("net/http.(*conn).serve" → "http.(*conn).serve")。domain.ShortFunctionName と同じ規則
function short(fn) {
  if (!fn) return ""
  const i = fn.lastIndexOf("/")
  return i >= 0 ? fn.slice(i + 1) : fn
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c])
}
