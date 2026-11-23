package domain

import (
	"fmt"
	"time"
)

// ExportCSV は todo を CSV にする。
// 不具合 (GC_PRESSURE): 行を足すたびに文字列全体をつなぎ直すので、件数の 2 乗のメモリを確保して GC が頻発する
func ExportCSV(todos []Todo) string {
	out := "id,title,done,created_at\n"
	for _, t := range todos {
		out += fmt.Sprintf("%d,%q,%t,%s\n", t.ID, t.Title, t.Done, t.CreatedAt.Format(time.RFC3339))
	}
	return out
}
