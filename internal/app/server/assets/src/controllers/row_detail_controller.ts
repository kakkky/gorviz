import { Controller } from "@hotwired/stimulus"

// 一覧の行の印を押すと、その行のすぐ下の詳しい行を開け閉めする。開いたら一覧の自動更新 (SSE) を止め、開いた行が流れないようにする。
// 止めるまでに届いた書き換え (morph) でも閉じないよう、開け閉めの属性だけは書き換えさせない
export default class extends Controller {
  private onBeforeMorphAttribute = (e: Event) => {
    const el = e.target as Element
    if (!this.element.contains(el)) return
    const name = (e as CustomEvent<{ attributeName?: string }>).detail?.attributeName
    if ((name === "hidden" || name === "data-open") && el.matches("tr[data-open]")) e.preventDefault()
    if (name === "aria-expanded" && el.matches(".row-toggle[aria-expanded=true]")) e.preventDefault()
  }

  connect() {
    document.addEventListener("turbo:before-morph-attribute", this.onBeforeMorphAttribute)
  }

  disconnect() {
    document.removeEventListener("turbo:before-morph-attribute", this.onBeforeMorphAttribute)
  }

  toggleRow(e: Event) {
    if ((e.target as Element).closest("a, button")) return
    const button = (e.currentTarget as Element).querySelector<HTMLElement>(".row-toggle")
    if (button) this.toggleWith(button)
  }

  toggle(e: Event) {
    this.toggleWith(e.currentTarget as HTMLElement)
  }

  private toggleWith(button: HTMLElement) {
    const detail = button.closest("tr")?.nextElementSibling as HTMLElement | null
    if (!detail) return
    detail.hidden = !detail.hidden
    detail.toggleAttribute("data-open", !detail.hidden)
    button.setAttribute("aria-expanded", String(!detail.hidden))
    if (!detail.hidden) {
      for (const source of document.querySelectorAll("turbo-stream-source")) source.remove()
      for (const dot of document.querySelectorAll<HTMLElement>(".trp-live")) dot.hidden = true
    }
  }
}
