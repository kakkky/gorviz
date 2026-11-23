// アイランドを turbo-mount に登録する。Stimulus は hotwire-go の import map から読み込まれた window.Stimulus を使う
import { h, render } from "preact"
import { TurboMount, TurboMountController, type Plugin } from "turbo-mount"
import type { GMPReplayProps, ReplayWindow } from "./gen/types"
import { GmpReplay } from "./gmp_replay/GmpReplay"
import { Signal } from "./gmp_replay/signal"

declare global {
  interface Window {
    Turbo?: { visit(url: string): void }
  }
}

const preact: Plugin<any> = {
  mountComponent({ el, Component, props }) {
    render(h(Component, props), el)
    return () => render(null, el)
  },
}

// GmpReplayController は再生する窓を受け取ってアイランドに流し込み、アイランドの外 (画面の移動) を受け持つ。
// 窓は <script type="application/json"> の target として届く (最初の窓も、SSE の turbo-stream で足された窓も)
class GmpReplayController extends TurboMountController {
  static targets = [...TurboMountController.targets, "window"]

  private windows = new Signal<ReplayWindow[]>([])
  private beforeCache = () => this.umountComponent()

  connect() {
    document.addEventListener("turbo:before-cache", this.beforeCache)
    super.connect()
  }

  disconnect() {
    document.removeEventListener("turbo:before-cache", this.beforeCache)
    super.disconnect()
  }

  windowTargetConnected(el: Element) {
    try {
      const w = JSON.parse(el.textContent ?? "") as ReplayWindow
      this.windows.set([...this.windows.value.slice(-5), w])
    } finally {
      el.remove()
    }
  }

  get componentProps(): object {
    const props = this.propsValue as GMPReplayProps
    return {
      ...props,
      windows: this.windows,
      onNavigate: (url: string) => visit(url),
      onSelect: (g: number, from: string, to: string) => {
        const u = new URL(props.goroutineUrl.replace("{goroutine}", String(g)), location.origin)
        u.searchParams.set("from", from)
        u.searchParams.set("to", to)
        visit(u.pathname + u.search)
      },
    }
  }
}

function visit(url: string) {
  if (window.Turbo) window.Turbo.visit(url)
  else location.assign(url)
}

const turboMount = new TurboMount({ application: window.Stimulus })
turboMount.register(preact, "GmpReplay", GmpReplay, GmpReplayController)
