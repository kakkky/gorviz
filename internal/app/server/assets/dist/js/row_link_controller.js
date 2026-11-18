import { Controller } from "@hotwired/stimulus"

// 一覧の行 (data-href を持つ tr) のどこをクリックしても、その行の先を開く。行の中のボタンやリンクは、それぞれの先を開く
export default class extends Controller {
  connect() {
    this.onClick = (e) => {
      if (e.target.closest("a, button, input, select, summary")) return
      const row = e.target.closest("[data-href]")
      if (!row || !this.element.contains(row)) return
      if (window.Turbo) window.Turbo.visit(row.dataset.href)
      else window.location.assign(row.dataset.href)
    }
    this.element.addEventListener("click", this.onClick)
  }

  disconnect() {
    this.element.removeEventListener("click", this.onClick)
  }
}
