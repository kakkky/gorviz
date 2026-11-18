import { Controller } from "@hotwired/stimulus"

// 期間を選ぶ部品 (<details>) を、外を押したときと Esc で閉じる。Cancel のボタンからも閉じる
export default class extends Controller {
  connect() {
    this.onDocumentClick = (e) => {
      if (this.element.open && !this.element.contains(e.target)) this.close()
    }
    this.onKeydown = (e) => {
      if (e.key === "Escape" && this.element.open) this.close()
    }
    document.addEventListener("click", this.onDocumentClick)
    document.addEventListener("keydown", this.onKeydown)
  }

  disconnect() {
    document.removeEventListener("click", this.onDocumentClick)
    document.removeEventListener("keydown", this.onKeydown)
  }

  close() {
    this.element.open = false
  }
}
