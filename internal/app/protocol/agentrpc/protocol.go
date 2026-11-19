// Package agentrpc は agent と server の間の口の形 (agent.proto と、そこから作った gRPC の型)。両側の実装は agent/driver/serverconn と server/driver/agentlistener。
// 接続はどちらも agent から server へ外向きに張り、向きごとに 1 本ずつ持つ。
//   - push の接続: 軽いもの (ログ・goroutine dump) を agent が数秒ごとに送る (agent が gRPC の呼び出し側)
//   - pull の接続: 重い trace を server が必要なときだけ求める (gRPC を逆向きに使い、server が呼び出し側)
//
// 対象の環境の内側に外からの入口を開けずに、trace を何をいつ運ぶかは server が決められる
package agentrpc

type ConnKind string

const (
	ConnPush ConnKind = "push"
	ConnPull ConnKind = "pull"
)

// Hello は agent がつないだ直後、gRPC を流す前に 1 行の JSON で送る自己紹介。Kind でその接続の向きを伝える。
// pull の接続では server が呼び出し側になり、agent から metadata で名乗れないので、gRPC の外で送る。
// Service は対象のアプリの名前 (同じアプリの複数のインスタンスを、画面でまとめて並べるのに使う)。空ならまとめない
type Hello struct {
	Name    string   `json:"name"`
	Service string   `json:"service,omitempty"`
	Target  string   `json:"target"`
	Kind    ConnKind `json:"kind"`
}
