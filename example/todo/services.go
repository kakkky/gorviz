package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Notifier はリマインダーを外部の通知サービスに登録する。通知サービスは遅いことがある (0〜2 秒)
type Notifier struct{}

// Register は 1 秒で諦める。
// 不具合 (LEAK): 送り手の goroutine は結果を buffer のない channel に送るので、タイムアウトで受け手が先に返ると、送り手は二度と送れずに残り続ける
func (n *Notifier) Register(ctx context.Context, t Todo) error {
	result := make(chan error)
	go func() {
		time.Sleep(time.Duration(rand.IntN(2000)) * time.Millisecond)
		result <- nil
	}()
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return fmt.Errorf("register reminder for todo %d: %w", t.ID, ctx.Err())
	}
}

// Mailer は完了メールを外部のメール送信サービスに渡す。
// 不具合 (エラーが増える): 送信サービスは 2 分ごとに 30 秒落ちる。その間の完了はエラーになる
type Mailer struct{}

var errMailUnavailable = errors.New("mail service unavailable (503)")

func (m *Mailer) SendCompleted(t Todo) error {
	time.Sleep(time.Duration(20+rand.IntN(40)) * time.Millisecond)
	if time.Now().Unix()%120 < 30 {
		return fmt.Errorf("send completion mail for todo %d: %w", t.ID, errMailUnavailable)
	}
	return nil
}

// Backup は todo を JSON にしてディスクに書き出す。
// 不具合 (遅いリクエスト): 書き出しが終わるまでストアのロックを握ったままなので、その間の全リクエストがロック待ちになる
type Backup struct {
	store *Store
	dir   string
	mu    sync.Mutex
	last  atomic.Int64
}

func (b *Backup) LastAt() time.Time {
	if n := b.last.Load(); n > 0 {
		return time.Unix(0, n)
	}
	return time.Time{}
}

func (b *Backup) Run() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.store.mu.Lock()
	defer b.store.mu.Unlock()
	data, err := json.Marshal(b.store.todos)
	if err != nil {
		return "", err
	}
	// 遅いディスク (ネットワークストレージ) に見立てる
	time.Sleep(1500 * time.Millisecond)
	path := filepath.Join(b.dir, "todo-backup.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	b.last.Store(time.Now().UnixNano())
	return path, nil
}

func (b *Backup) RunEvery(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			start := time.Now()
			if path, err := b.Run(); err != nil {
				logger.Error("scheduled backup failed", "err", err)
			} else {
				logger.Info("scheduled backup done", "path", path, "duration", time.Since(start))
			}
		}
	}
}

// Stats は完了数と、同じ内容の todo (重複) の数を数える。重複はタイトルを正規化した指紋で見分ける。
// 不具合 (CPU_WAIT_SPIKE): todo ごとに goroutine を立てて重いハッシュを計算するので、件数が多いと P が埋まり、他のリクエストが実行を待たされる
func Stats(todos []Todo) (done, duplicates int) {
	var wg sync.WaitGroup
	prints := make([][32]byte, len(todos))
	for i, t := range todos {
		if t.Done {
			done++
		}
		wg.Go(func() { prints[i] = fingerprint(t.Title) })
	}
	wg.Wait()
	seen := map[[32]byte]bool{}
	for _, p := range prints {
		if seen[p] {
			duplicates++
		}
		seen[p] = true
	}
	return done, duplicates
}

var titleSuffix = regexp.MustCompile(`\s*(#\d+|\(user \d+\))$`)

func fingerprint(title string) [32]byte {
	sum := sha256.Sum256([]byte(strings.ToLower(titleSuffix.ReplaceAllString(title, ""))))
	for range 30000 {
		sum = sha256.Sum256(sum[:])
	}
	return sum
}

// ExportCSV は todo を CSV にする。
// 不具合 (MEMORY_PRESSURE): 行を足すたびに文字列全体をつなぎ直すので、件数の 2 乗のメモリを確保して GC が頻発する
func ExportCSV(todos []Todo) string {
	out := "id,title,done,created_at\n"
	for _, t := range todos {
		out += fmt.Sprintf("%d,%q,%t,%s\n", t.ID, t.Title, t.Done, t.CreatedAt.Format(time.RFC3339))
	}
	return out
}
