// duration はナノ秒を読める長さにする
export function duration(ns: number): string {
  if (ns < 1e3) return `${Math.round(ns)}ns`
  if (ns < 1e6) return `${trim(ns / 1e3, 1)}µs`
  if (ns < 1e9) return `${trim(ns / 1e6, 2)}ms`
  return `${trim(ns / 1e9, 2)}s`
}

function trim(v: number, prec: number): string {
  return String(parseFloat(v.toFixed(prec)))
}

// clock は窓の始まり (unix ms) からのナノ秒を、ブラウザの時刻にする
export function clock(startedAtMs: number, ns: number): string {
  const d = new Date(startedAtMs + ns / 1e6)
  const pad = (v: number, n = 2) => String(v).padStart(n, "0")
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}

// short はパッケージパスを落とす (net/http.(*conn).serve → http.(*conn).serve)
export function short(fn: string | undefined): string {
  if (!fn) return ""
  const i = fn.lastIndexOf("/")
  return i >= 0 ? fn.slice(i + 1) : fn
}
