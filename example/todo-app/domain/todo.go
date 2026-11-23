package domain

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

type TodoID int

type Title string

func NewTitle(s string) (Title, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrInvalidArgument.With("title is empty")
	}
	return Title(s), nil
}

type Todo struct {
	ID        TodoID
	Title     Title
	Done      bool
	Reminder  bool
	CreatedAt time.Time
	DoneAt    time.Time
}

func (t Todo) Matches(query string) bool {
	return query == "" || strings.Contains(strings.ToLower(string(t.Title)), strings.ToLower(query))
}

// SortForList は未完了を先に、新しいものを上に並べる
func SortForList(todos []Todo) {
	slices.SortFunc(todos, func(a, b Todo) int {
		if a.Done != b.Done {
			if a.Done {
				return 1
			}
			return -1
		}
		return cmp.Compare(b.ID, a.ID)
	})
}

var sampleTitles = []string{
	"牛乳を買う", "請求書を払う", "週報を書く", "歯医者を予約する", "PR をレビューする", "ジムに行く",
	"部屋を掃除する", "本を返す", "母に電話する", "旅行の宿を取る", "障害報告を書く", "依存ライブラリを更新する",
}

// SampleTitle は n 番目のサンプルの題
func SampleTitle(n int) Title {
	return Title(fmt.Sprintf("%s #%d", sampleTitles[n%len(sampleTitles)], n))
}
