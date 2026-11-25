package accessor

import (
	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
)

type IssueBookAccessor struct {
	kvs *kvs.InMemoryKVS
}

func NewIssueBookAccessor(k *kvs.InMemoryKVS) *IssueBookAccessor {
	return &IssueBookAccessor{kvs: k}
}

func newIssueBook() any { return domain.NewIssueBook() }

func (a *IssueBookAccessor) UpdateIssueBook(f func(b *domain.IssueBook)) {
	a.kvs.Update(issueBookKey, newIssueBook, func(v any) { f(v.(*domain.IssueBook)) })
}

func (a *IssueBookAccessor) ViewIssueBook(f func(b *domain.IssueBook)) {
	a.kvs.View(issueBookKey, newIssueBook, func(v any) { f(v.(*domain.IssueBook)) })
}
