import { Controller } from "@hotwired/stimulus"

// カーソルが線からこの距離 (px) の中にあれば、線の上にあるとみなす
const HIT_PX = 10

// これより少ない区切りしか選ばなかったドラッグはクリックとみなし、拡大しない。短すぎる範囲はサーバが overviewMinZoom (1 分) に広げる
const MIN_ZOOM_STEPS = 2

// Overview の推移のグラフをまとめて扱う。どれかに hover すると、すべてのグラフの同じ時刻に縦線を出し、
// カーソルが線の近く (棒なら棒の上) にあるときだけ、すべてのグラフにその時刻の値を出す。ドラッグ中は値を出さない。
// ドラッグで選んだ範囲に拡大する。ページは開き直さず、SSE の接続先 (source) とアドレスバーの ?from=&to= だけを替える
// (開き直すとスクロールが先頭に戻るため)。
// グラフは SSE の morph で refresh ごとに書き換わり、JS で足した縦線などは消えるので、書き換わるたびに描き直す
export default class extends Controller {
  static values = { source: String }

  connect() {
    this.index = null
    this.detail = false
    this.drag = null
    this.onMove = (e) => this.move(e)
    this.onLeave = () => this.leave()
    this.onDown = (e) => this.down(e)
    this.onUp = () => this.up()
    this.onClick = (e) => this.click(e)
    this.onRender = (e) => {
      const render = e.detail.render
      e.detail.render = async (stream) => {
        await render(stream)
        this.redraw()
      }
    }
    this.element.addEventListener("pointermove", this.onMove)
    this.element.addEventListener("pointerleave", this.onLeave)
    this.element.addEventListener("pointerdown", this.onDown)
    this.element.addEventListener("click", this.onClick)
    window.addEventListener("pointerup", this.onUp)
    document.addEventListener("turbo:before-stream-render", this.onRender)
  }

  disconnect() {
    this.element.removeEventListener("pointermove", this.onMove)
    this.element.removeEventListener("pointerleave", this.onLeave)
    this.element.removeEventListener("pointerdown", this.onDown)
    this.element.removeEventListener("click", this.onClick)
    window.removeEventListener("pointerup", this.onUp)
    document.removeEventListener("turbo:before-stream-render", this.onRender)
  }

  frame() {
    const el = this.element.querySelector("[data-chart-steps]")
    if (!el) return null
    try {
      const f = JSON.parse(el.dataset.chartSteps)
      return f.times && f.times.length > 0 ? f : null
    } catch {
      return null
    }
  }

  indexAt(plot, clientX, n) {
    const r = plot.getBoundingClientRect()
    return Math.min(n - 1, Math.max(0, Math.floor(((clientX - r.left) / r.width) * n)))
  }

  move(e) {
    const plot = e.target.closest(".chart-plot")
    const f = this.frame()
    if (!f || (!plot && !this.drag)) {
      if (!this.drag) this.clear()
      return
    }
    if (plot) {
      this.index = this.indexAt(plot, e.clientX, f.times.length)
      this.detail = this.onData(plot, e.clientY, this.index)
    }
    if (this.drag) this.drag.to = this.index
    this.redraw()
  }

  // onData はカーソルが、線グラフならどれかの線の近くに、棒グラフなら棒の上にあるか
  onData(plot, clientY, i) {
    const chart = plot.closest(".chart")
    let readout = []
    try {
      readout = JSON.parse(chart.dataset.chartReadout || "[]")
    } catch {
      return false
    }
    const r = plot.getBoundingClientRect()
    const scale = 160 / r.height
    const y = (clientY - r.top) * scale
    const ys = readout.map((s) => s.y && s.y[i]).filter((v) => v !== null && v !== undefined)
    if (ys.length === 0) return false
    if (chart.dataset.chartKind === "bar") return y >= Math.min(...ys) - HIT_PX * scale
    return ys.some((v) => Math.abs(v - y) <= HIT_PX * scale)
  }

  leave() {
    if (!this.drag) this.clear()
  }

  down(e) {
    const plot = e.target.closest(".chart-plot")
    const f = this.frame()
    if (!plot || !f || e.button !== 0) return
    e.preventDefault()
    const i = this.indexAt(plot, e.clientX, f.times.length)
    this.drag = { from: i, to: i }
    this.index = i
    this.redraw()
  }

  up() {
    if (!this.drag) return
    const f = this.frame()
    const a = Math.min(this.drag.from, this.drag.to)
    const b = Math.max(this.drag.from, this.drag.to)
    this.drag = null
    if (!f || b - a + 1 < MIN_ZOOM_STEPS) {
      this.redraw()
      return
    }
    this.index = null
    this.switchTo({ from: f.times[a], to: f.times[b] + f.stepMillis })
  }

  // click は「reset zoom」を、開き直さずに元の期間へ戻す
  click(e) {
    if (!e.target.closest("[data-chart-reset]")) return
    e.preventDefault()
    this.switchTo(null)
  }

  // switchTo は zoom ({from, to}、null なら拡大をやめる) の期間に、アドレスバーと SSE の接続先を替える。
  // turbo-stream-source は繋ぎ直す口を持たないので、src を替えた複製と入れ替えて繋ぎ直させる
  switchTo(zoom) {
    const page = new URL(window.location.href)
    const source = document.getElementById(this.sourceValue)
    const stream = source ? new URL(source.getAttribute("src"), window.location.href) : null
    for (const url of [page, stream]) {
      if (!url) continue
      // 期間を選ぶ部品の入力欄 (since / until) は拡大より先に読まれるので、拡大・戻すときは外す
      url.searchParams.delete("since")
      url.searchParams.delete("until")
      if (zoom) {
        url.searchParams.set("from", String(zoom.from))
        url.searchParams.set("to", String(zoom.to))
      } else {
        url.searchParams.delete("from")
        url.searchParams.delete("to")
      }
    }
    // Turbo の戻る・進むが壊れないよう、Turbo が入れた history の state はそのまま使う
    window.history.replaceState(window.history.state, "", page.toString())
    const range = page.searchParams.get("range")
    // 期間を選ぶ部品のよく使う期間と、今に合わせて動く印を、拡大したかどうかに合わせる (範囲そのものは SSE で書き換わる)
    for (const a of document.querySelectorAll(".trp-presets a")) {
      a.classList.toggle("active", !zoom && new URL(a.href).searchParams.get("range") === range)
    }
    for (const dot of document.querySelectorAll(".trp-live")) dot.hidden = !!zoom
    if (!source) {
      window.location.assign(page.toString())
      return
    }
    const next = source.cloneNode(false)
    next.setAttribute("src", stream.pathname + stream.search)
    source.replaceWith(next)
    this.redraw()
  }

  clear() {
    this.index = null
    this.detail = false
    this.redraw()
  }

  redraw() {
    const f = this.frame()
    for (const chart of this.element.querySelectorAll(".chart")) {
      const plot = chart.querySelector(".chart-plot")
      if (!plot) continue
      plot.querySelectorAll(".chart-cursor, .chart-tip, .chart-selection").forEach((el) => el.remove())
      if (!f || this.index === null) continue
      const n = f.times.length
      const i = Math.min(this.index, n - 1)
      if (this.drag) {
        const a = Math.min(this.drag.from, this.drag.to)
        const b = Math.max(this.drag.from, this.drag.to)
        const sel = document.createElement("div")
        sel.className = "chart-selection"
        sel.style.left = `${(a / n) * 100}%`
        sel.style.width = `${((b - a + 1) / n) * 100}%`
        plot.appendChild(sel)
      }
      const x = ((i + 0.5) / n) * 100
      const cursor = document.createElement("div")
      cursor.className = "chart-cursor"
      cursor.style.left = `${x}%`
      plot.appendChild(cursor)
      if (this.detail && !this.drag) plot.appendChild(this.tip(chart, f, i, x))
    }
  }

  // tip はその時刻の、そのグラフのすべての系列の値。ラベルはログのメッセージを含むので textContent で入れる
  tip(chart, f, i, x) {
    const tip = document.createElement("div")
    tip.className = "chart-tip"
    if (x > 60) {
      tip.style.right = `${100 - x}%`
      tip.classList.add("left")
    } else {
      tip.style.left = `${x}%`
    }
    const head = document.createElement("div")
    head.className = "chart-tip-time"
    head.textContent = `${f.labels[i]} (${f.step})`
    tip.appendChild(head)
    let readout = []
    try {
      readout = JSON.parse(chart.dataset.chartReadout || "[]")
    } catch {
      readout = []
    }
    let rows = 0
    for (const s of readout) {
      const text = s.text[i]
      if (!text) continue
      const row = document.createElement("div")
      row.className = "chart-tip-row"
      const swatch = document.createElement("i")
      swatch.className = s.class
      const label = document.createElement("span")
      label.className = "chart-tip-label"
      label.textContent = s.label
      const value = document.createElement("span")
      value.className = "chart-tip-value"
      value.textContent = text
      row.append(swatch, label, value)
      tip.appendChild(row)
      rows++
    }
    let notes = []
    try {
      notes = JSON.parse(chart.dataset.chartNotes || "[]")[i] || []
    } catch {
      notes = []
    }
    if (notes.length > 0) {
      const head = document.createElement("div")
      head.className = "chart-tip-notes"
      head.textContent = "top origins"
      tip.appendChild(head)
    }
    for (const n of notes) {
      const row = document.createElement("div")
      row.className = "chart-tip-row"
      const label = document.createElement("span")
      label.className = "chart-tip-label"
      label.textContent = n.label
      const value = document.createElement("span")
      value.className = "chart-tip-value"
      value.textContent = n.text
      row.append(label, value)
      tip.appendChild(row)
    }
    if (rows === 0) {
      const none = document.createElement("div")
      none.className = "chart-tip-none"
      none.textContent = "no data"
      tip.appendChild(none)
    }
    return tip
  }
}
