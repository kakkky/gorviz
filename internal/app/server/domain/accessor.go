//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_accessor.go -package=mock
package domain

import (
	"io"
	"time"
)

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

// TraceStream は agent から届いた execution trace のバイト列を、届いた順に読み手へ渡す
type TraceStream interface {
	AppendTrace(p []byte) error
	CloseTrace(broken bool)
	TraceReader() io.Reader
	TraceBroken() bool
}
