import { Controller } from "@hotwired/stimulus"

// 一覧の行の印を押すと、その行のすぐ下の詳しい行 (取り込んだままのログなど) を開け閉めする。行そのものを押したときの移動 (row-link) とは別。
// 開いたら、一覧の自動更新 (SSE) を止める。開いた行が更新で流れたり閉じたりしないように。また動かすには期間を選び直す。
// 止めるまでのあいだに届いた書き換え (morph) でも閉じないよう、開け閉めの属性だけは書き換えさせない
export default class extends Controller {
  connect() {
    this.onBeforeMorphAttribute = (e) => {
      const el = e.target
      if (!this.element.contains(el)) return
      const name = e.detail?.attributeName
      if ((name === "hidden" || name === "data-open") && el.matches("tr[data-open]")) e.preventDefault()
      if (name === "aria-expanded" && el.matches(".row-toggle[aria-expanded=true]")) e.preventDefault()
    }
    document.addEventListener("turbo:before-morph-attribute", this.onBeforeMorphAttribute)
  }

  disconnect() {
    document.removeEventListener("turbo:before-morph-attribute", this.onBeforeMorphAttribute)
  }

  // toggleRow は行のどこを押しても開け閉めする。行の中のボタンやリンクは、それぞれの先を開く
  toggleRow(e) {
    if (e.target.closest("a, button")) return
    const button = e.currentTarget.querySelector(".row-toggle")
    if (button) this.toggleWith(button)
  }

  toggle(e) {
    this.toggleWith(e.currentTarget)
  }

  toggleWith(button) {
    const detail = button.closest("tr")?.nextElementSibling
    if (!detail) return
    detail.hidden = !detail.hidden
    detail.toggleAttribute("data-open", !detail.hidden)
    button.setAttribute("aria-expanded", String(!detail.hidden))
    if (!detail.hidden) this.stopLive()
  }

  stopLive() {
    for (const source of document.querySelectorAll("turbo-stream-source")) source.remove()
    for (const dot of document.querySelectorAll(".trp-live")) dot.hidden = true
  }
}
