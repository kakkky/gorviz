import { Controller } from "@hotwired/stimulus"

// 期間を選ぶ部品 (<details>) を外を押したときと Esc で閉じる。入力欄は datetime-local でブラウザの時間帯なので、
// 開くときに server の RFC 3339 を読み替えて入れ、送るときに RFC 3339 に戻して from・to に入れる
export default class extends Controller<HTMLDetailsElement> {
  static targets = ["fromInput", "toInput", "fromValue", "toValue"]
  static values = { from: String, to: String }

  declare readonly fromInputTarget: HTMLInputElement
  declare readonly toInputTarget: HTMLInputElement
  declare readonly fromValueTarget: HTMLInputElement
  declare readonly toValueTarget: HTMLInputElement
  declare readonly fromValue: string
  declare readonly toValue: string

  private onClick = (e: MouseEvent) => {
    if (this.element.open && !this.element.contains(e.target as Node)) this.close()
  }
  private onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape" && this.element.open) this.close()
  }

  connect() {
    this.fromInputTarget.value = toLocalInput(this.fromValue)
    this.toInputTarget.value = toLocalInput(this.toValue)
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

  submit() {
    this.fromValueTarget.value = new Date(this.fromInputTarget.value).toISOString()
    this.toValueTarget.value = new Date(this.toInputTarget.value).toISOString()
  }
}

function toLocalInput(iso: string): string {
  if (!iso) return ""
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
