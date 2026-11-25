import { Controller } from "@hotwired/stimulus"

// 期間を選ぶ部品 (<details>) を外を押したときと Esc で閉じる。期間はセッションの始まりからの秒 (12.5s) で、入力欄からそのまま送る
export default class extends Controller<HTMLDetailsElement> {
  private onClick = (e: MouseEvent) => {
    if (this.element.open && !this.element.contains(e.target as Node)) this.close()
  }
  private onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape" && this.element.open) this.close()
  }

  connect() {
    document.addEventListener("click", this.onClick)
    document.addEventListener("keydown", this.onKey)
  }

  disconnect() {
    document.removeEventListener("click", this.onClick)
    document.removeEventListener("keydown", this.onKey)
  }

  close() {
    this.element.open = false
  }
}
