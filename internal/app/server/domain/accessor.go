//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_accessor.go -package=mock
package domain

import "time"

// SessionStore はセッションの状態を読み書きする
type SessionStore interface {
	Session() Session
	UpdateSession(f func(s *Session))
}

// ObservationAccessor は 1 秒ごとの Observation を出し入れする
type ObservationAccessor interface {
	Put(o Observation) error
	Get(second int) (Observation, bool)
	List(from, to time.Duration) ([]Observation, error)
}

// IssueBookStore は見つけた Issue を読み書きする。UpdateIssueBook は、ほかの書き手と重ならないよう、f の中で書き換えたものをまとめて入れる。f の中では Issue を持ち出さない
type IssueBookStore interface {
	UpdateIssueBook(f func(b *IssueBook))
	ViewIssueBook(f func(b *IssueBook))
}

// TraceStream はアプリから取った trace の区切りを、取った順に読み手へ渡す。NextTrace は次の区切りが来るまで待ち、閉じて読み切ったら false を返す
type TraceStream interface {
	AppendTrace(c TraceChunk) error
	CloseTrace()
	NextTrace() (TraceChunk, bool)
}
