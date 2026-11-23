import { Controller } from "@hotwired/stimulus"

// これより少ない区切りしか選ばなかったドラッグはクリックとみなし、拡大しない (区切りの帯のリンクを開く)
const MIN_ZOOM_STEPS = 2

// グラフをドラッグして選んだ範囲 (区切りに揃える) を、画面の期間 (from・to) にして開き直す。
// 開き直すとスクロールが先頭に戻るので、開き直した後に元の位置へ戻す
export default class extends Controller<HTMLElement> {
  static targets = ["plot"]
  static values = { from: Number, to: Number, first: Number, step: Number, steps: Number }

  declare readonly plotTarget: HTMLElement
  declare readonly fromValue: number
  declare readonly toValue: number
  declare readonly firstValue: number
  declare readonly stepValue: number
  declare readonly stepsValue: number

  private drag: { from: number; to: number } | null = null
  private dragged = false

  private onUp = () => this.up()
  // ドラッグの後の click は、区切りの帯のリンクに届かせない
  private onClick = (e: MouseEvent) => {
    if (!this.dragged) return
    this.dragged = false
    e.preventDefault()
    e.stopPropagation()
  }

  connect() {
    window.addEventListener("pointerup", this.onUp)
    this.plotTarget.addEventListener("click", this.onClick, true)
  }

  disconnect() {
    window.removeEventListener("pointerup", this.onUp)
    this.plotTarget.removeEventListener("click", this.onClick, true)
  }

  down(e: PointerEvent) {
    if (e.button !== 0) return
    e.preventDefault()
    const i = this.stepAt(e.clientX)
    this.drag = { from: i, to: i }
    this.dragged = false
    this.draw()
  }

  move(e: PointerEvent) {
    if (!this.drag) return
    this.drag.to = this.stepAt(e.clientX)
    this.draw()
  }

  private up() {
    if (!this.drag) return
    const [a, b] = this.selected()
    this.drag = null
    this.draw()
    if (b - a + 1 < MIN_ZOOM_STEPS) return
    this.dragged = true
    this.zoom(this.startOf(a), this.startOf(b + 1))
  }

  private selected(): [number, number] {
    const { from, to } = this.drag!
    return [Math.min(from, to), Math.max(from, to)]
  }

  // startOf は i 番目の区切りの始まりを、グラフの期間の中に収めたもの
  private startOf(i: number): number {
    return Math.min(Math.max(this.firstValue + i * this.stepValue, this.fromValue), this.toValue)
  }

  private stepAt(clientX: number): number {
    const r = this.plotTarget.getBoundingClientRect()
    const t = this.fromValue + ((clientX - r.left) / r.width) * (this.toValue - this.fromValue)
    return Math.min(this.stepsValue - 1, Math.max(0, Math.floor((t - this.firstValue) / this.stepValue)))
  }

  private draw() {
    this.plotTarget.querySelector(".chart-selection")?.remove()
    if (!this.drag) return
    const [a, b] = this.selected()
    const span = this.toValue - this.fromValue
    const left = (this.startOf(a) - this.fromValue) / span
    const right = (this.startOf(b + 1) - this.fromValue) / span
    const sel = document.createElement("div")
    sel.className = "chart-selection"
    sel.style.left = `${left * 100}%`
    sel.style.width = `${(right - left) * 100}%`
    this.plotTarget.appendChild(sel)
  }

  private zoom(from: number, to: number) {
    const url = new URL(window.location.href)
    url.searchParams.set("from", new Date(from).toISOString())
    url.searchParams.set("to", new Date(to).toISOString())
    // 続きの位置と開いていたログの行は、前の期間でしか意味を持たない
    url.searchParams.delete("cursor")
    url.searchParams.delete("log")
    const y = window.scrollY
    document.addEventListener("turbo:load", () => window.scrollTo(0, y), { once: true })
    const w = window as unknown as { Turbo?: { visit(url: string): void } }
    if (w.Turbo) w.Turbo.visit(url.toString())
    else location.assign(url.toString())
  }
}
