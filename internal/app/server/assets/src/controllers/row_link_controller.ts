import { Controller } from "@hotwired/stimulus"

// 一覧の行 (data-href を持つ tr) のどこを押しても、その行の先を開く。行の中のボタンやリンクは、それぞれの先を開く
export default class extends Controller {
  private onClick = (e: MouseEvent) => {
    const target = e.target as Element
    if (target.closest("a, button, input, select, summary")) return
    const row = target.closest<HTMLElement>("[data-href]")
    if (!row || !this.element.contains(row) || !row.dataset.href) return
    const w = window as unknown as { Turbo?: { visit(url: string): void } }
    if (w.Turbo) w.Turbo.visit(row.dataset.href)
    else location.assign(row.dataset.href)
  }

  connect() {
    this.element.addEventListener("click", this.onClick as EventListener)
  }

  disconnect() {
    this.element.removeEventListener("click", this.onClick as EventListener)
  }
}
