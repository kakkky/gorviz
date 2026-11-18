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
// parked の組 1 つに並べる G のチップの数の上限。残りは件数で出す
const WAIT_CHIPS_MAX = 40
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
  static values = { pageGoroutine: String, length: String, focusHost: String, live: Boolean, rangeHost: String }
  static targets = ["window", "stage", "seek", "clock", "play", "note", "lanes", "since"]

  connect() {
    this.openLogs ??= new Set()
    this.playing = false
    // Stimulus は connect より先に target の接続 (windowTargetConnected) を呼ぶので、最初から HTML にある窓を上書きしない
    this.current ??= null
    this.timer = null
    this.onStageClick = this.onStageClick.bind(this)
    this.onLanesClick = this.onLanesClick.bind(this)
    this.onSeekClick = (e) => {
      const seek = e.target.closest("[data-seek]")
      if (seek && this.current) {
        this.pauseLive()
        this.seekTo(Number(seek.dataset.seek))
      }
      const logRow = e.target.closest("[data-log-toggle]")
      if (logRow && this.current) {
        const key = logRow.dataset.logToggle
        if (this.openLogs.has(key)) {
          this.openLogs.delete(key)
        } else {
          this.openLogs.add(key)
          this.pauseLive()
          this.stop()
        }
        this.render()
      }
      const jump = e.target.closest("[data-parked-jump]")
      if (jump) {
        const side = this.stageTarget.querySelector(".gmp-side")
        const target = side?.querySelector(`[data-parked-target="${jump.dataset.parkedJump}"]`)
        if (target) side.scrollTo({ top: target.offsetTop - this.parkedHeadHeight(side), behavior: "smooth" })
      }
    }
    this.element.addEventListener("click", this.onSeekClick)
    this.stageTarget.addEventListener("click", this.onStageClick)
    // 追っている G の欄を再生の外 (Goroutine 画面のカード) に描くときは、その欄のクリックも同じように受ける。
    // カードは G を選び直すたびに差し替わる (turbo-frame) ので、document で受けて、差し替わったら描き直す
    this.onHostClick = (e) => {
      if (!this.hasFocusHostValue || !this.focusHostValue) return
      if (e.target.closest("[data-goroutine-close]")) {
        this.closeCard()
        return
      }
      if (this.focusHost?.contains(e.target)) {
        this.onSeekClick(e)
        this.onStageClick(e)
      }
    }
    // カードの高さが変わると右の列の高さも変わるので、追っている G の組も見せ直す
    this.onFrameLoad = () => {
      this.revealedFocus = null
      if (this.current) this.render()
    }
    document.addEventListener("click", this.onHostClick)
    document.addEventListener("turbo:frame-load", this.onFrameLoad)
    // 最初の窓は connect より先に (画面の配置が決まる前に) 描かれている (windowTargetConnected) ので、ここで描き直し、追っている G の組も見せ直す
    this.revealedFocus = null
    if (this.current) {
      this.render()
      // connect は窓を読んだ後に呼ばれ、再生の状態をここで初めに戻すので、続きの再生はその後に始める
      this.arrive()
    }
    this.lanesTarget.addEventListener("click", this.onLanesClick)
  }

  disconnect() {
    this.stop()
    this.element.removeEventListener("click", this.onSeekClick)
    this.stageTarget.removeEventListener("click", this.onStageClick)
    document.removeEventListener("click", this.onHostClick)
    document.removeEventListener("turbo:frame-load", this.onFrameLoad)
    this.lanesTarget.removeEventListener("click", this.onLanesClick)
  }

  // 最新を追っているあいだは、SSE で届いた新しい窓に替える。追っている G はそのまま追う。
  // 操作して追うのをやめた (pauseLive) あと・再生中は、届いても読まない (見ている窓が勝手に替わらないように)
  windowTargetConnected(el) {
    const data = JSON.parse(el.textContent)
    if (!this.current) {
      this.load(data)
      return
    }
    if (!this.liveValue || this.livePaused) return
    // 再生中は、窓の終わりまで来たときに替える (onReachedEnd)
    if (this.playing) {
      this.pendingLive = data
      if (this.waitingForLive) this.playPendingLive()
      return
    }
    this.loadLatest(data)
    this.showLatestMoment()
  }

  // loadLatest は最新の窓に替える。届く窓には追っている G の印がないので、Goroutine 画面では追っている G を保ち、診断のカードもその窓で診断し直す
  loadLatest(data) {
    const followed = this.focusG
    this.load(data)
    if (followed !== null) {
      this.focusG = followed
      this.track = this.buildTrack(followed)
      this.drawLanes()
    }
    const frame = this.focusHost ? document.getElementById("goroutine-diagnosis") : null
    if (frame && followed !== null) {
      const at = Math.round(data.startedAtNs + data.windowNs / 2)
      frame.src = `/goroutines/${followed}?${new URLSearchParams({ at: String(at), length: this.lengthValue })}`
    }
  }

  // showLatestMoment は最新を追っているあいだ、窓の終わり (録れている中でいちばん新しい時点) を見せる
  showLatestMoment() {
    this.reset()
    this.advance(this.current.windowNs)
    this.render()
  }

  // arrive は窓の端から続き・前に移ってきたときの、再生の続け方 (URL の autoplay・position)。読んだら URL から外す。
  // 最新を追っているあいだは動いている状態なので、ボタンは停止にする
  arrive() {
    const u = new URL(location.href)
    if (this.liveValue && !this.livePaused) {
      this.setPlayIcon("❚❚", "stop following the latest recording")
      this.showLatestMoment()
    }
    if (u.searchParams.get("position") === "end") {
      this.advance(this.current.windowNs)
      this.render()
    }
    const autoplay = u.searchParams.get("autoplay") === "1"
    for (const k of ["autoplay", "position"]) u.searchParams.delete(k)
    history.replaceState(history.state, "", u)
    if (autoplay) this.play()
  }

  // onReachedEnd は窓の終わりまで来たとき。最新を追っているなら次に録れた窓へ、そうでなければ録画の続きの区間へ移る
  onReachedEnd(playing) {
    if (playing && this.liveValue && !this.livePaused) {
      if (this.pendingLive && this.pendingLive.startedAtNs > this.current.startedAtNs) {
        this.playPendingLive()
      } else {
        this.waitingForLive = true
      }
      return
    }
    const after = this.current.startedAtNs + this.current.windowNs
    if (!playing) {
      this.stop()
      this.moveTo({ after }, {})
      return
    }
    // 追っている G がその窓で何もしないと、すぐ窓の終わりに来る。窓ごとに 1 歩ぶんは見せてから移る (続けざまに読み込まない)
    this.timer = setTimeout(() => {
      if (!this.playing) return
      this.stop()
      this.moveTo({ after }, { autoplay: "1" })
    }, PLAY_INTERVAL_MS)
  }

  playPendingLive() {
    const data = this.pendingLive
    this.pendingLive = null
    this.waitingForLive = false
    this.loadLatest(data)
    this.reset()
    this.render()
    this.timer = setTimeout(() => this.playStep(), PLAY_INTERVAL_MS)
  }

  // moveTo は録画の続き・前の区間の画面に移る。スクロール位置と、追っている G はそのまま
  moveTo(where, extra) {
    const u = new URL(location.href)
    for (const k of ["at", "after", "before", "since", "until", "from", "log"]) u.searchParams.delete(k)
    for (const [k, v] of Object.entries(where)) u.searchParams.set(k, String(Math.round(v)))
    for (const [k, v] of Object.entries(extra)) u.searchParams.set(k, v)
    try {
      sessionStorage.setItem("gorviz:keep-scroll", String(window.scrollY))
    } catch {
      // 保存できなければ、Turbo のとおり先頭から見せる
    }
    if (window.Turbo) window.Turbo.visit(u.pathname + u.search)
    else window.location.assign(u.pathname + u.search)
  }

  // pauseLive は最新を追うのをやめ、見出しの動いている印を消す。また追うには再生する (録画の続きをつないで最新に追いつくと、最新を追う画面に戻る)
  pauseLive() {
    if (!this.liveValue || this.livePaused) return
    this.livePaused = true
    for (const dot of document.querySelectorAll(".trp-live")) dot.hidden = true
    if (!this.playing) this.setPlayIcon("▶", "play")
  }

  load(data) {
    // 開いたログの行は窓の中の順番で覚えているので、窓が替わったら閉じる
    this.openLogs = new Set()
    this.current = data
    this.appLogs = [...data.logs].sort((a, b) => a.atNs - b.atNs)
    this.leaked = new Set(data.leaked)
    this.leakedSince = this.stuckSince(this.leaked)
    this.entries = new Map(data.entries.map(([g, fn]) => [g, data.functions[fn]]))
    this.serverFocus = data.focus || null
    this.before = data.before || null
    this.focusG = this.serverFocus && this.serverFocus.g >= 0 ? this.serverFocus.g : null
    this.seekTarget.max = data.windowNs
    this.noteTarget.textContent = data.truncated
      ? "events were truncated: the replay stops before the end of the window"
      : data.skippedNs > 0 ? `skipped ${ms(data.skippedNs)} not recorded before this window` : ""
    this.noteTarget.classList.toggle("error", !!data.truncated)
    this.noteTarget.classList.toggle("muted", !data.truncated)
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

  // 最新を追っているあいだ (再生していなくても窓が替わり続ける) と再生中は、ボタンは停止。押すと止め、最新を追うのもやめる
  togglePlay() {
    if (this.playing || (this.liveValue && !this.livePaused)) {
      this.stop()
      this.pauseLive()
    } else {
      this.play()
    }
  }

  play() {
    this.playing = true
    this.setPlayIcon("❚❚", "pause")
    this.playStep()
  }

  playStep() {
    if (!this.playing) return
    if (!this.stepForward()) {
      this.onReachedEnd(true)
      return
    }
    this.timer = setTimeout(() => this.playStep(), PLAY_INTERVAL_MS)
  }

  setPlayIcon(icon, label) {
    this.playTarget.textContent = icon
    this.playTarget.title = label
    this.playTarget.setAttribute("aria-label", label)
  }

  stop() {
    this.playing = false
    this.waitingForLive = false
    clearTimeout(this.timer)
    if (this.hasPlayTarget) {
      if (this.liveValue && !this.livePaused) this.setPlayIcon("❚❚", "stop following the latest recording")
      else this.setPlayIcon("▶", "play")
    }
  }

  // 窓の端で 1 歩進める・戻すと、録画の続き・前の区間へ移る
  next() {
    this.pauseLive()
    this.stop()
    if (!this.stepForward()) this.onReachedEnd(false)
  }

  prev() {
    this.pauseLive()
    this.stop()
    const t = this.previousTime()
    if (t === null) {
      this.moveTo({ before: this.current.startedAtNs }, { position: "end" })
      return
    }
    this.reset()
    this.advance(t)
    this.render()
  }

  seek() {
    this.pauseLive()
    this.stop()
    this.advance(Number(this.seekTarget.value))
    this.render()
  }

  // G を選ぶと、その G を focus にして状態の推移と内訳を出す。focus の G の帯をクリックするとその時刻へ移る
  onStageClick(e) {
    // 時刻を動かす操作 (帯のクリック) では、最新を追うのをやめる
    if (e.target.closest("[data-strip]")) this.pauseLive()
    const strip = e.target.closest("[data-strip]")
    if (strip) {
      this.seekTo(this.timeAt(e, strip))
      return
    }
    if (e.target.closest("a")) return
    const chip = e.target.closest("[data-g]")
    if (!chip) return
    const g = Number(chip.dataset.g)
    // Scheduler では、G を選ぶとその G の Goroutine 画面 (診断と、その G を追う再生) に移る。今の時刻を真ん中にした同じ長さの区間
    if (!this.focusHost) {
      this.openGoroutine(g)
      return
    }
    this.focusG = this.focusG === g ? null : g
    this.track = this.focusG !== null ? this.buildTrack(this.focusG) : null
    this.drawLanes()
    this.render()
    if (this.focusG !== null) this.selectOnPage(this.focusG)
  }

  openGoroutine(g) {
    const q = new URLSearchParams({ at: String(Math.round(this.current.startedAtNs + this.t)) })
    if (this.lengthValue) q.set("length", this.lengthValue)
    const url = `/goroutines/${g}?${q}`
    if (window.Turbo) window.Turbo.visit(url)
    else window.location.assign(url)
  }

  // revealFocusGroup は追っている G がいる parked の組が右の列の外にあれば、そこまでスクロールする。
  // 見せるのは G を選んだときと、その G が別の組に移ったときだけ。1 歩ごとに戻すと、自分でスクロールした位置が保てない
  revealFocusGroup() {
    const side = this.stageTarget.querySelector(".gmp-side")
    const group = side?.querySelector(".wgroup.has-focus")
    if (!group) {
      this.revealedFocus = null
      return
    }
    const key = `${this.focusG}\u0000${group.querySelector(".fn")?.textContent}`
    if (this.revealedFocus === key) return
    this.revealedFocus = key
    // 列の高さは描いた後の配置 (左の列の高さ) で決まるので、測るのは次のフレームにする
    requestAnimationFrame(() => {
      if (!group.isConnected) return
      const head = this.parkedHeadHeight(side)
      // .gmp-side は position: relative なので、offsetTop は列の中での位置
      const visible = group.offsetTop >= side.scrollTop + head && group.offsetTop + group.offsetHeight <= side.scrollTop + side.clientHeight
      if (!visible) side.scrollTop = group.offsetTop - head - side.clientHeight / 3
    })
  }

  // parkedHeadHeight は右の列の上に貼りついている見出し (件数と割合) の高さ。スクロール先がその下に隠れないようにずらす
  parkedHeadHeight(side) {
    return (side.querySelector(".parked-head")?.offsetHeight ?? 0) + 8
  }

  // focusHost は追っている G の欄を描く再生の外の要素。カードごと差し替わるので毎回引く
  get focusHost() {
    return this.focusHostValue ? document.getElementById(this.focusHostValue) : null
  }

  // closeCard は Goroutine 画面のカードを閉じ、再生でも G を追うのをやめる。開き直すのは再生で G を選んだとき
  closeCard() {
    this.focusG = null
    this.track = null
    this.drawLanes()
    this.render()
  }

  // selectOnPage は Goroutine 画面で、選んだ G に診断を揃える。再生の区間は変えず、診断の枠だけをその G の画面に差し替え、URL もその G にする。
  // from (ログのリクエストの区間) と log は元の G のものなので外す
  selectOnPage(g) {
    if (!this.focusHost || this.pageGoroutineValue === String(g)) return
    const frame = document.getElementById("goroutine-diagnosis")
    if (!frame) return
    const u = new URL(location.href)
    u.pathname = `/goroutines/${g}`
    u.searchParams.delete("from")
    u.searchParams.delete("log")
    // 再生の前後のボタンと、見出しの区間を選ぶ部品 (よく使う長さ・入力欄の送り先) も、元の G の画面を指しているので選んだ G に向け直す
    const from = `/goroutines/${this.pageGoroutineValue}`
    for (const form of document.querySelectorAll(`form[action="${from}"]`)) form.setAttribute("action", u.pathname)
    for (const link of document.querySelectorAll(`a[href^="${from}?"]`)) {
      const l = new URL(link.href)
      l.pathname = u.pathname
      l.searchParams.delete("from")
      l.searchParams.delete("log")
      link.setAttribute("href", l.pathname + l.search)
    }
    this.pageGoroutineValue = String(g)
    frame.src = u.pathname + u.search
    history.replaceState(history.state, "", u)
  }

  // P timeline をクリックすると、その時刻へ移る。帯から少し外れていれば、いちばん近い帯の始まり (G が P に乗った瞬間) に合わせる
  onLanesClick(e) {
    const track = e.target.closest("[data-track]")
    if (!track || !this.current) return
    this.pauseLive()
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

  // G を追っているときは、その G の出来事とログだけを 1 歩に数える
  focusOnly() {
    return this.focusG !== null
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
    // サーバの診断 (domain.Breakdown) と同じく、GC が全 G を止めた区間とこの G の mark assist に重なる時間は、元の状態によらず GC に数える
    const gc = mergeRanges((this.current.gc || []).filter(([, , gg]) => gg === -1 || gg === g))
    const totals = Object.fromEntries(CAUSES.map((c) => [c, 0]))
    for (const s of segments) {
      if (!(s.cause in totals)) continue
      const inGC = overlapOf(gc, s.start, s.end)
      totals[s.cause] += s.end - s.start - inGC
      totals.GC += inGC
    }
    const recorded = Object.values(totals).reduce((a, b) => a + b, 0)
    // この G が書いたログ。推し量った G のログ (probablyG) も、そう分かるように並べる
    const logs = this.appLogs.filter((l) => l.g === g || l.probablyG === g).map((l) => ({ ...l, probably: l.g !== g }))
    return { g, segments, totals, recorded, logs }
  }

  // --- view ---

  snapshot() {
    const procs = [...this.procs.entries()].sort((a, b) => a[0] - b[0]).map(([p, s]) => ({ proc: p, status: s.status, thread: s.thread, running: null, lrq: [] }))
    const byProc = new Map(procs.map((p) => [p.proc, p]))
    const grq = []
    const waiting = new Map()
    const handedOff = []
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
    return { procs, grq, waiting, handedOff }
  }

  render() {
    if (!this.current) return
    const s = this.snapshot()
    const out = []
    // P の図と待ち行列・待機は隣に並べて 1 つの図として見せる。追っている G の欄は Goroutine 画面のカード (focusHost) に描く
    out.push(`<div class="gmp-board"><div class="gmp-main">`)
    this.drawProcs(out, s)
    this.drawGlobalQueue(out, s)
    this.drawHandedOff(out, s)
    out.push(`</div>`)
    this.drawParked(out, s)
    out.push(`</div>`)
    // 描き直しても、右の列とグローバルキューのスクロール位置は保つ (1 歩進めるたびに見ている所が先頭へ戻らないように)
    const scrolls = [...this.stageTarget.querySelectorAll("[data-keep-scroll-pos]")].map((el) => [el.dataset.keepScrollPos, el.scrollTop, el.scrollLeft])
    this.stageTarget.innerHTML = out.join("")
    // 再生の上にあるカードを先に描く。カードの高さでスクロール先が変わる
    const host = this.focusHost
    if (host) {
      const focus = []
      if (this.track) this.drawFocus(focus)
      host.innerHTML = focus.join("")
      host.closest("[data-goroutine-card]")?.classList.toggle("is-closed", !this.track)
    }
    for (const [key, top, left] of scrolls) {
      const el = this.stageTarget.querySelector(`[data-keep-scroll-pos="${key}"]`)
      if (el) el.scrollTo(left, top)
    }
    this.revealFocusGroup()

    this.seekTarget.value = this.t
    // 出来事は µs 単位で並ぶので、1 歩進んだことが分かるよう今の時刻は細かく出す
    this.clockTarget.textContent = `${(this.t / 1e6).toFixed(3)}ms / ${ms(this.current.windowNs)}`
    // 追っている G の欄は、Goroutine 画面のカードと同じ「following」の枠で囲み、同じ G の話だと分かるようにする
    if (this.hasSinceTarget) {
      const since = this.track ? this.stateSinceHTML(this.track) || `<span class="gchip is-focus small">G${this.track.g}</span>` : ""
      this.sinceTarget.innerHTML = since ? `<span class="following-label">following</span>${since}` : ""
    }
    const at = pct(this.t, this.current.windowNs)
    this.lanesTarget.querySelectorAll("[data-cursor]").forEach((c) => { c.style.left = at })
  }

  // stateSinceHTML は今の再生の位置で、追っている G がいつから今の状態かと、その状態に入った瞬間へ移るボタン。
  // 窓の中で状態に入っていれば再生の中で移り、窓の頭より前からなら (Goroutine 画面の G だけ) サーバがさかのぼった時刻の画面へ移る。
  // どちらもボタンは同じ文言にする
  stateSinceHTML(tr) {
    const segs = tr.segments
    let i = segs.findIndex((s) => s.start <= this.t && this.t < s.end)
    if (i < 0) return ""
    // trace は状態が変わらなくても書き直すので、同じ状態が隙間なく続く区間はつなげて始まりを探す
    while (i > 0 && segs[i - 1].cause === segs[i].cause && segs[i - 1].reason === segs[i].reason && segs[i - 1].end === segs[i].start) i--
    const seg = segs[i]
    const who = `<span class="gchip is-focus small"><span class="gdot cause-bg-${seg.cause}"></span>G${tr.g}</span><span class="cause-name">${seg.cause}</span>`
    const label = `Go to when ${seg.cause} began`
    if (seg.start > 0) {
      const button = this.t > seg.start ? `<button type="button" class="btn small" data-seek="${seg.start}">${label}</button>` : ""
      return `${who}<span class="mono">since ${clockTime(this.current.startedAtNs + seg.start)}</span>${button}`
    }
    const before = this.before && this.before.g === tr.g && this.before.cause === seg.cause ? this.before : null
    if (!before) return `${who}<span class="mono">since before this window</span>`
    if (before.sinceNs > 0) {
      return `${who}<span class="mono">since ${clockTime(before.sinceNs)}</span><a class="btn small" href="${before.url}" data-turbo-frame="_top" data-keep-scroll>${label}</a>`
    }
    return `${who}<span class="mono">since before ${clockTime(before.recordedFromNs).slice(0, 8)}</span><span class="faint">no earlier trace is recorded</span>`
  }

  // drawWindow は最新を追っているとき、新しい窓の区間を画面の見出しの部品に書く (それ以外はサーバが描いた区間のまま)。
  // 形は components.TimeRangeBody と、短く切れたときの書き方は handler の replayRange と同じ
  drawWindow() {
    const host = this.rangeHostValue ? document.getElementById(this.rangeHostValue) : null
    const { startedAtNs: start, windowNs: w } = this.current
    if (!host || !this.liveValue || !(start > 0)) return
    const chosen = durationNs(this.lengthValue)
    const cut = chosen > 0 && w < chosen * 0.98
    host.innerHTML = timeRangeBodyHTML(start, start + w, cut ? `${ms(w)} of ${ms(chosen)}` : ms(w), cut ? "cut short at the edge of a recorded chunk" : "")
  }




  // drawLanes は P ごとに、窓の中で G が走った区間を帯にする。色は図の G と同じく入口の関数で決め、追っている G は目立たせる
  drawLanes() {
    const { procs, lanes, windowNs: w } = this.current
    const out = [`<div class="caption"><strong>CPU slots (P) over time</strong></div><div class="lanes-grid">`]
    const ticks = axisTicks(w)
    const tickLines = ticks.filter((t) => t > 0 && t < w).map((t) => `<span class="tick" style="left:${pct(t, w)}"></span>`).join("")
    procs.forEach((p, i) => {
      const spans = lanes?.[i] || []
      let busy = 0
      out.push(`<span class="pname">P${p}</span><div class="lane-track" data-track="${i}">${tickLines}`)
      for (const [start, end, g] of spans) {
        busy += end - start
        const focus = g === this.focusG ? " is-focus" : ""
        out.push(`<span class="seg${focus}" style="left:${pct(start, w)};width:${pct(end - start, w)}" title="${esc(`G${g} ${this.label(g)}\n${ms(end - start)}`)}"></span>`)
      }
      out.push(`<span class="cursor" data-cursor></span></div><span class="util" title="share of the window this P was running a goroutine">${w ? Math.round((busy / w) * 100) : 0}%</span>`)
    })
    out.push(`<span></span><div class="lanes-axis">${ticks.map((t) => `<span class="${t > w * 0.9 ? "end" : ""}" style="left:${pct(t, w)}">${t === 0 ? "0" : ms(t)}</span>`).join("")}</div><span class="util">busy</span>`)
    out.push(`</div>`)
    this.lanesTarget.innerHTML = out.join("")
  }

  drawFocus(out) {
    const tr = this.track
    const now = this.gs.get(tr.g)
    const nowCause = now ? causeOf(now.place, this.current.reasons[now.reason]) || "EXITED" : "UNRECORDED"
    const where = now ? this.describePlace(now.place, now.proc, now.thread, now.reason) : "not seen yet in this window"
    const at = now?.fn ? ` · ${esc(short(now.fn))}` : ""
    // 診断が追っている G に付いてくる (Goroutine 画面) か、その G の画面にいるなら、その G の画面へのボタンは出さない
    const onItsPage = !!this.focusHost || this.pageGoroutineValue === String(tr.g)
    const href = this.current.startedAtNs > 0 && !onItsPage ? `/goroutines/${tr.g}?at=${Math.round(this.current.startedAtNs + this.t)}` : null

    out.push(`<div class="gmp-focus">`)
    // カードに描くときは、G と関数はカードの見出しにある
    if (!onItsPage || !this.focusHost) {
      out.push(`<div class="gmp-focus-head"><span class="gchip is-focus"><span class="gdot cause-bg-${nowCause}"></span>G${tr.g}</span><strong>${esc(this.label(tr.g))}</strong>`)
      if (href) out.push(`<a class="btn gmp-focus-link" href="${href}" data-turbo-frame="_top">Diagnose G${tr.g}</a>`)
      out.push(`</div>`)
    }
    out.push(`<div class="gmp-focus-now"><span class="cause-dot cause-bg-${nowCause}"></span><span class="cause-name">${nowCause}</span><span class="muted">${esc(where)}${at}</span></div>`)

    // 状態の推移の帯。クリックでその時刻に移る
    const w = this.current.windowNs
    out.push(`<div class="gmp-strip" data-strip title="click to jump to that moment">`)
    for (const sg of tr.segments) {
      const tip = `${sg.cause}${sg.reason ? ` (${sg.reason})` : ""} ${ms(sg.end - sg.start)}${sg.fn ? `\n${short(sg.fn)}` : ""}`
      out.push(`<span class="seg cause-bg-${sg.cause}" style="left:${pct(sg.start, w)};width:${pct(sg.end - sg.start, w)}" title="${esc(tip)}"></span>`)
    }
    for (const l of tr.logs) out.push(`<span class="log-mark${l.probably ? " probably" : ""}" style="left:${pct(l.atNs, w)}" title="${esc(`${l.level} ${ms(l.atNs)}  ${l.text}`)}"></span>`)
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
    out.push(`</div>`)
    this.drawFocusLogs(out, tr)
    out.push(`</div>`)
  }

  // drawFocusLogs は追っている G がこの窓で書いたログ。見た目は Logs の一覧と同じで、行を押すと取り込んだままの行 (JSON ならキーと値) を開け閉めする。
  // 開いた行は描き直しても開いたまま (openLogs)。開いたら最新を追うのをやめ、見ている窓を替えない
  drawFocusLogs(out, tr) {
    out.push(`<div class="gmp-logs"><div class="caption">logs written by G${tr.g} in this window</div>`)
    if (tr.logs.length === 0) {
      out.push(`<p class="faint">no log written by G${tr.g} in this window</p></div>`)
      return
    }
    out.push(`<table class="log-table">`)
    tr.logs.forEach((l, i) => {
      // 属性に入れるので、特別な文字を含まない値にする (同じ時刻のログもあるので、窓の中の順番も足す)
      const key = `${l.atNs}-${i}`
      const hasDetail = (l.fields && l.fields.length > 0) || !!l.detail
      const open = hasDetail && this.openLogs.has(key)
      const level = (l.level || "unknown").toLowerCase()
      out.push(`<tr class="${hasDetail ? "openable" : ""}"${hasDetail ? ` data-log-toggle="${esc(key)}"` : ""}>`)
      out.push(`<td class="log-toggle">${hasDetail ? `<button type="button" class="row-toggle" aria-expanded="${open}" title="show the line as it was logged"></button>` : ""}</td>`)
      out.push(`<td class="log-time">${clockTime(this.current.startedAtNs + l.atNs)}</td>`)
      out.push(`<td><span class="level ${esc(level)}">${esc(level === "unknown" ? "-" : level.toUpperCase())}</span></td>`)
      out.push(`<td class="log-message">${esc(l.text)}</td>`)
      out.push(`<td class="side">${l.probably ? `<span class="badge inference" title="the only goroutine running at the log's timestamp, not located exactly through the trace">probably</span>` : ""}</td>`)
      out.push(`</tr>`)
      if (open) {
        out.push(`<tr class="log-detail"><td></td><td colspan="4">`)
        if (l.fields && l.fields.length > 0) {
          out.push(`<table class="log-fields">`)
          for (const [k, v] of l.fields) out.push(`<tr><th>${esc(k)}</th><td>${esc(v)}</td></tr>`)
          out.push(`</table>`)
        } else {
          out.push(`<pre>${esc(l.detail)}</pre>`)
        }
        out.push(`</td></tr>`)
      }
    })
    out.push(`</table></div>`)
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

  // drawGlobalQueue は全 P が取りに来るグローバルキュー。P の図のすぐ下に、並んだ順 (先頭が左上) に置く。
  // 高さを 2 行に固定し、あふれた分は枠の中でスクロールさせて、件数が変わっても下の図を動かさない
  drawGlobalQueue(out, s) {
    out.push(`<div class="gmp-grq" title="inferred from the execution trace: a goroutine readied by a P goes to that P's local run queue, one descheduled by preemption, yield or a syscall without P goes to the global run queue. runnext is not distinguished">`)
    out.push(`<h4>global run queue <span>${s.grq.length}</span><small>shared by all P · head first</small></h4>`)
    out.push(`<div class="grq-row" data-keep-scroll-pos="grq">`)
    // 1 件ずつ、syscall の M の組と同じ角丸の四角にし、並んでいる順番 (先頭が 1) を添える
    s.grq.slice(0, GRQ_MAX).forEach((g, i) => out.push(`<span class="slot-chip" title="position ${i + 1} in the global run queue"><span class="slot-label">${i + 1}</span>${this.chip(g, "runnable", true)}</span>`))
    if (s.grq.length > GRQ_MAX) out.push(`<span class="more">+${s.grq.length - GRQ_MAX}</span>`)
    out.push(`</div></div>`)
  }

  // drawHandedOff は P を手放して syscall に入っている M。P の行き来に関わるので、P の図の側に置く
  drawHandedOff(out, s) {
    // runtime にこういう置き場 (キュー) はない。G は自分の M に付いたままカーネルの中にいて、P だけが外されて別の M に渡っている。
    // キューと取り違えないよう、M と G の組のチップで描く。グローバルキューと同じく高さを固定し、あふれた分は枠の中でスクロールさせる
    out.push(`<div class="gmp-row-area" title="not a queue: each goroutine stays on its M (OS thread) inside the kernel, and its P was handed to another M"><h4>M in syscall, P released <span>${s.handedOff.length}</span></h4><div class="m-rows" data-keep-scroll-pos="handed-off">`)
    s.handedOff.forEach((h) => out.push(`<span class="slot-chip"><span class="slot-label">${h.thread >= 0 ? `M${h.thread}` : "M?"}</span><span class="arrow">→</span>${this.chip(h.g, "syscall", true)}</span>`))
    out.push(`</div></div>`)
  }

  // drawParked は右の列いっぱいに、止まっている G をすべての組まで出す。たいていの G はここにいるので、打ち切らない。
  // 止まっている G はスケジューラのどのキューにもおらず、待っている相手 (チャネル・セマフォ・netpoller・タイマー) の側からたどれるだけ。
  // trace には相手のアドレスが載らないので、相手の種類ごとに段を分け、その中を「関数 × 理由」でまとめる
  drawParked(out, s) {
    const sections = new Map(WAIT_TARGETS.map((t) => [t.key, []]))
    for (const entry of s.waiting.entries()) sections.get(waitTargetOf(entry[0].split("\u0000")[1])).push(entry)
    const counts = new Map(WAIT_TARGETS.map((t) => [t.key, sections.get(t.key).reduce((a, [, gs]) => a + gs.length, 0)]))
    const total = [...counts.values()].reduce((a, b) => a + b, 0)
    out.push(`<div class="gmp-side" data-keep-scroll-pos="side" title="parked goroutines are not in any scheduler queue; each one hangs on what it waits for">`)
    out.push(`<div class="parked-head"><h4>parked <span>${total}</span></h4>`)
    // 待っている相手ごとの割合。押すとその段へ移る
    out.push(`<div class="parked-share">`)
    for (const t of WAIT_TARGETS) {
      const n = counts.get(t.key)
      if (n) out.push(`<span class="share-${t.key}" style="flex:${n}" title="${t.label}: ${n}"></span>`)
    }
    out.push(`</div><div class="parked-legend">`)
    for (const t of WAIT_TARGETS) {
      const n = counts.get(t.key)
      if (n) out.push(`<button type="button" class="parked-jump" data-parked-jump="${t.key}"><span class="swatch share-${t.key}"></span>${t.label}<span class="mono">${n}</span></button>`)
    }
    out.push(`</div></div>`)
    for (const target of WAIT_TARGETS) {
      const groups = sections.get(target.key).sort((a, b) => b[1].length - a[1].length)
      if (groups.length === 0) continue
      out.push(`<div class="wtarget" data-parked-target="${target.key}"><div class="wtarget-head"><span class="swatch share-${target.key}"></span><span>${target.label}</span><span class="faint">${target.where}</span><span class="count">${counts.get(target.key)}</span></div>`)
      for (const [key, gs] of groups) out.push(this.waitGroup(key, gs, target.key))
      out.push(`</div>`)
    }
    if (total === 0) out.push(`<p class="faint">no parked goroutine at this moment</p>`)
    out.push(`</div>`)
  }

  waitGroup(key, gs, target) {
    const [fn, reason] = key.split("\u0000")
    const leaked = gs.filter((g) => this.isLeaked(g)).length
    const hasFocus = gs.includes(this.focusG)
    const dot = target === "runtime" ? "gdot other" : `gdot cause-bg-${causeOf(PLACE_WAITING, reason)}`
    // 組の見出し (関数・理由・件数) の下に、その G のチップを並べる。状態の色はチップの丸だけに付ける (見出しにも付けると重なって見づらい)。
    // 追っている G は上限に入らなくても必ず出す
    const out = [`<div class="wgroup${hasFocus ? " has-focus" : ""}" title="${esc(`${fn} waiting on ${reason}`)}">`]
    out.push(`<div class="wgroup-head"><span class="fn">${esc(fn)}<span class="reason">${esc(reason)}</span></span>`)
    if (leaked) out.push(`<span class="badge kind">${leaked} leaked</span>`)
    out.push(`<span class="count">${gs.length}</span></div><div class="wgroup-chips">`)
    const shown = gs.slice(0, WAIT_CHIPS_MAX)
    if (hasFocus && !shown.includes(this.focusG)) shown.push(this.focusG)
    for (const g of shown) out.push(this.chip(g, "waiting", false, dot))
    if (gs.length > shown.length) out.push(`<span class="more">+${gs.length - shown.length}</span>`)
    out.push(`</div></div>`)
    return out.join("")
  }

  // リークの判定は最新の dump でのものなので、窓の中では、その G が抜け出せなかった最後の状態に入った後だけリークとして見せる
  isLeaked(g) {
    return this.leaked.has(g) && this.t >= (this.leakedSince.get(g) ?? 0)
  }

  // stuckSince は gs の各 G が、窓の終わりまで続く状態に入った時刻。窓の頭から同じ状態なら 0。
  // trace は状態が変わらなくても書き直すことがあり、そのときは待機理由が載らないので、場所の種類が同じで理由が分からなければ同じ状態とみなす
  stuckSince(gs) {
    const since = new Map()
    const last = new Map()
    const reasonOf = (i) => (i >= 0 ? this.current.reasons[i] : "")
    const known = (r) => r !== "" && r !== "unknown"
    for (const ev of this.current.events) {
      if (ev[1] !== EVENT_G || !gs.has(ev[2])) continue
      const [at, , g, place, , , reason] = ev
      const prev = last.get(g)
      const r = reasonOf(reason)
      const same = prev && prev.place === place && (!known(prev.reason) || !known(r) || prev.reason === r)
      if (!same) since.set(g, at)
      last.set(g, { place, reason: same && !known(r) ? prev.reason : r })
    }
    return since
  }

  // chip は G 1 つ。丸の色は状態 (内訳の帯と同じ色)。クリックするとその G を追う
  // chip は G 1 つのチップ。dot を渡すと丸の色をそれにする (parked の G は、待っている理由の色)
  chip(g, kind, wide = false, dot = null) {
    const fn = this.entries.get(g)
    const cls = ["gchip"]
    if (g === this.focusG) cls.push("is-focus")
    if (this.changed.has(g)) cls.push("flash")
    const now = this.gs.get(g)?.fn
    const tip = `G${g}${this.isLeaked(g) ? " (leaked)" : ""}\nentry: ${fn || "?"}${now ? `\nnow at: ${now}` : ""}\nclick to diagnose this goroutine`
    const text = wide ? `G${g}<small>${esc(this.label(g))}</small>` : `G${g}`
    return `<button type="button" class="${cls.join(" ")}" data-g="${g}" title="${esc(tip)}"><span class="${dot || `gdot cause-bg-${KIND_CAUSES[kind]}`}${this.isLeaked(g) ? " is-leaked" : ""}"></span>${text}</button>`
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

// axisTicks は 0 から w までを 4〜5 つほどに分ける、切りのよい時刻 (1・2・5 × 10^n ナノ秒の倍数)
function axisTicks(w) {
  if (!(w > 0)) return [0]
  const raw = w / 5
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= raw)
  const ticks = []
  for (let t = 0; t <= w + 1; t += step) ticks.push(t)
  return ticks
}

// mergeRanges は [start, end, ...] の区間を、重なりをまとめて start の順に並べる
function mergeRanges(ranges) {
  const sorted = ranges.map(([s, e]) => [s, e]).sort((a, b) => a[0] - b[0])
  const out = []
  for (const r of sorted) {
    const last = out[out.length - 1]
    if (last && r[0] <= last[1]) last[1] = Math.max(last[1], r[1])
    else out.push(r)
  }
  return out
}

// overlapOf は [start, end] がまとめた区間 merged に重なる長さ
function overlapOf(merged, start, end) {
  let total = 0
  for (const [s, e] of merged) total += Math.max(0, Math.min(end, e) - Math.max(start, s))
  return total
}

// timeRangeBodyHTML は components.TimeRangeBody と同じ形の範囲の中身。再生の区間は 1 分より短いので ms まで出す
function timeRangeBodyHTML(fromNs, toNs, length, lengthTitle) {
  return `<time>${dateTime(fromNs)}</time><span class="sep">→</span><time>${dateTime(toNs)}</time><span class="len" title="${esc(lengthTitle)}">${length}</span>`
}

function dateTime(ns) {
  const d = new Date(ns / 1e6)
  const pad = (n) => String(n).padStart(2, "0")
  return `${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${clockTime(ns)}`
}

// durationNs は Go の time.Duration の文字列 (100ms・1.5s など) をナノ秒にする。読めなければ 0
function durationNs(s) {
  const m = /^([\d.]+)(ns|µs|us|ms|s|m)$/.exec(s || "")
  if (!m) return 0
  return Number(m[1]) * { ns: 1, "µs": 1e3, us: 1e3, ms: 1e6, s: 1e9, m: 6e10 }[m[2]]
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
