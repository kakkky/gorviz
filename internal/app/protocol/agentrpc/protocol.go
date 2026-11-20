// Package agentrpc は agent と server の間の口の形 (agent.proto と、そこから作った gRPC の型)。両側の実装は agent/driver/serverconn と server/driver/agentlistener。
// 接続は agent から server へ外向きに 1 本張り、agent がログ・goroutine dump・読み込んだ trace の区切りを数秒ごとに送る。
// 対象の環境の内側に外からの入口を開けず、agent は送るまでの分しか持たない
package agentrpc

// Hello は agent がつないだ直後、gRPC を流す前に 1 行の JSON で送る自己紹介。
// Service は対象のアプリの名前 (同じアプリの複数のインスタンスを、画面でまとめて並べるのに使う)。空ならまとめない
type Hello struct {
	Name    string `json:"name"`
	Service string `json:"service,omitempty"`
	Target  string `json:"target"`
}
