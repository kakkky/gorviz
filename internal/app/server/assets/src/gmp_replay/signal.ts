import { useEffect, useState } from "preact/hooks"

// Signal は controller からアイランドへ、届き続ける値を流し込む入れ物
export class Signal<T> {
  private listeners = new Set<(v: T) => void>()
  constructor(private current: T) {}

  get value(): T {
    return this.current
  }

  set(v: T) {
    this.current = v
    for (const l of this.listeners) l(v)
  }

  subscribe(l: (v: T) => void): () => void {
    this.listeners.add(l)
    return () => this.listeners.delete(l)
  }
}

export function useSignal<T>(s: Signal<T>): T {
  const [v, setV] = useState(s.value)
  useEffect(() => {
    setV(s.value)
    return s.subscribe(setV)
  }, [s])
  return v
}
