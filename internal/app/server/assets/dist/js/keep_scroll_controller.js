import { Controller } from "@hotwired/stimulus"

const KEY = "gorviz:keep-scroll"

// data-keep-scroll の付いたリンクで移ったときは、移る前のスクロール位置に戻す。
// 再生の前後送りや長さの切り替えは同じ画面の続きなので、Turbo が先頭に戻すと見ていた再生を見失う
document.addEventListener("click", (e) => {
  const a = e.target.closest("a[data-keep-scroll]")
  if (!a) return
  try {
    sessionStorage.setItem(KEY, String(window.scrollY))
  } catch {
    // 保存できなければ、Turbo のとおり先頭から見せる
  }
})

document.addEventListener("turbo:load", () => {
  let y = null
  try {
    y = sessionStorage.getItem(KEY)
    sessionStorage.removeItem(KEY)
  } catch {
    return
  }
  if (y !== null) window.scrollTo(0, Number(y))
})

// 読み込むためだけの controller。上の document の listener を登録する
export default class extends Controller {}
