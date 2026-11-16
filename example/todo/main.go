// todo は gorviz の体験を確かめるための観測対象。ブラウザで操作する Todo アプリで、実際の Web アプリにありがちな goroutine の不具合を含む:
//   - リマインダー付きで作ると、通知のタイムアウトで送り手の goroutine が残り続ける (LEAK)
//   - バックアップ中はストアのロックを握ったまま書き出すので、他のリクエストが待たされる (遅いリクエストのログから入ると BLOCKED)
//   - 完了メールの送信先がときどき落ち、その間の完了はエラーになる (エラーのログから入ると、G は外の呼び出しを待っているだけ)
//   - 統計は todo ごとに goroutine を立てて重い計算をするので、P が埋まる (CPU_WAIT_SPIKE)
//   - CSV エクスポートは文字列をつなぎ直し続けるので、GC が頻発する (MEMORY_PRESSURE)
//
// アプリは -addr (既定 localhost:8080)、pprof は別の口 -pprof-addr (既定 localhost:6060) で公開する。
// -users の数だけ、裏で仮想ユーザーが一覧・作成・完了を続ける
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kakkky/gorviz"
)

var logger = slog.New(gorviz.NewSlogHandler(slog.NewJSONHandler(os.Stderr, nil)))

func main() {
	addr := flag.String("addr", "localhost:8080", "listen address of the Todo app")
	pprofAddr := flag.String("pprof-addr", "localhost:6060", "listen address of net/http/pprof (gorviz-agent -target)")
	users := flag.Int("users", 3, "number of simulated users in the background (0 disables them)")
	backupDir := flag.String("backup-dir", os.TempDir(), "directory where backups are written")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := NewStore()
	store.Seed(30)
	app := &App{
		store:    store,
		notifier: &Notifier{},
		mailer:   &Mailer{},
		backup:   &Backup{store: store, dir: *backupDir},
	}
	go app.backup.RunEvery(ctx, time.Minute)
	for i := range *users {
		go simulateUser(ctx, "http://"+*addr, i)
	}

	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		if err := http.ListenAndServe(*pprofAddr, mux); err != nil {
			logger.Error("pprof server stopped", "err", err)
		}
	}()

	srv := &http.Server{Addr: *addr, Handler: app.Routes()}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	logger.Info("todo app started", "url", "http://"+*addr, "pprof", "http://"+*pprofAddr+"/debug/pprof/")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("todo app stopped", "err", err)
		os.Exit(1)
	}
}
