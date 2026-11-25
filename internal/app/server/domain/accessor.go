//go:generate mockgen -source=$GOFILE -destination=../testhelper/mock/mock_accessor.go -package=mock
package domain

import "io"

// SessionStore はセッションの状態を読み書きする
type SessionStore interface {
	Session() Session
	UpdateSession(f func(s *Session))
}

// TraceIndexStore は観測データの index を読み書きする。f の中では index を持ち出さない
type TraceIndexStore interface {
	UpdateTraceIndex(f func(ix *TraceIndex))
	ViewTraceIndex(f func(ix *TraceIndex))
}

// IssueBookStore は見つけた Issue を読み書きする。f の中では Issue を持ち出さない
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
