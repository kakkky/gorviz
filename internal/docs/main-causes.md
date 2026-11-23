# 主因 (goroutine が何に止まっていたか) の種類と、それぞれの次の一手

主因は、調べる期間の状態別の時間内訳のうち最も長い状態。

- `RUNNING`
  - 自分で CPU を使っていた。止まっていた場所の処理そのものを見直す
- `RUNNABLE`
  - 実行できるのに P が空かず待っていた。同じ時間帯の `CPU_CONTENTION` を見る
- `LOCK_WAIT`
  - `sync.Mutex`・`sync.RWMutex` の `Lock` で待っていた。待ちを起こした goroutine (ロックを持っていた側) をたどる
- `SYNC_WAIT`
  - channel・`select`・`sync.WaitGroup`・`sync.Cond` でほかの goroutine を待っていた。遅かった理由は待っていた相手にあるので、待ちを起こした goroutine をたどる
- `IO_WAIT`
  - ネットワーク・ディスク・システムコールを待っていた。外部の依存先を APM で見る
- `CGO`
  - cgo で C のコードを実行していた
- `SLEEP`
  - スリープ・タイマーで止まっていた
- `GC`
  - GC に止められた / mark assist をさせられていた。同じ時間帯の `GC_PRESSURE`・`LONG_STW` を見る

`LOCK_WAIT` と `sync.WaitGroup` の `SYNC_WAIT` は、trace の待機理由がどちらも「sync」なので、待機に入ったときのスタックで見分けている。

待ちを起こした goroutine が分かるのは、execution trace が残っている間だけ。
