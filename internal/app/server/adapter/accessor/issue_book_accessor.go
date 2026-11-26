package accessor

import (
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
)

const issueBookKey = "issue-book"

// IssueBookAccessor は Issue の一覧を出し入れする。書き換えは写しに対して行い、入れた一覧は書き換えない
type IssueBookAccessor struct {
	kvs *kvs.InMemoryKVS
}

func NewIssueBookAccessor(k *kvs.InMemoryKVS) *IssueBookAccessor {
	return &IssueBookAccessor{kvs: k}
}

func (a *IssueBookAccessor) UpdateIssueBook(f func(b *domain.IssueBook)) {
	a.kvs.Update(issueBookKey, func(v any) any {
		b, ok := v.(*domain.IssueBook)
		if !ok {
			b = domain.NewIssueBook()
		}
		next := b.Clone()
		f(next)
		return next
	})
}

func (a *IssueBookAccessor) ViewIssueBook(f func(b *domain.IssueBook)) {
	v, _ := a.kvs.Get(issueBookKey)
	b, ok := v.(*domain.IssueBook)
	if !ok {
		b = domain.NewIssueBook()
	}
	f(b)
}
