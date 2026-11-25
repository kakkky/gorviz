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

// clock は窓の始まり (セッションの始まりからの ms) からのナノ秒を、セッションの始まりからの秒にする
export function clock(startedAtMs: number, ns: number): string {
  return `${((startedAtMs + ns / 1e6) / 1000).toFixed(3)}s`
}

// short はパッケージパスを落とす (net/http.(*conn).serve → http.(*conn).serve)
export function short(fn: string | undefined): string {
  if (!fn) return ""
  const i = fn.lastIndexOf("/")
  return i >= 0 ? fn.slice(i + 1) : fn
}
