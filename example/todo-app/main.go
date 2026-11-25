// todo-app は gorviz で診断するための例のアプリ。ブラウザで操作する Todo アプリで、実際の Web アプリにありがちな goroutine の不具合を含む。
// 中の構成は gorviz 自身と同じ層 (docs/architecture.md) に分けてある
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kakkky/scope"

	"github.com/kakkky/gorviz/example/todo-app/adapter/accessor"
	"github.com/kakkky/gorviz/example/todo-app/adapter/client"
	"github.com/kakkky/gorviz/example/todo-app/config"
	"github.com/kakkky/gorviz/example/todo-app/driver/httpserver"
	"github.com/kakkky/gorviz/example/todo-app/driver/memstore"
	"github.com/kakkky/gorviz/example/todo-app/presentation/web"
	"github.com/kakkky/gorviz/example/todo-app/usecase"
	"github.com/kakkky/gorviz/example/todo-app/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.NewConfig()
	if err != nil {
		slog.ErrorContext(ctx, "load config", slog.Any("err", err))
		os.Exit(1)
	}
	if err := Run(ctx, cfg); err != nil {
		slog.ErrorContext(ctx, "run todo-app", slog.Any("err", err))
		os.Exit(1)
	}
}

func Run(ctx context.Context, cfg *config.Config) error {
	// driver (adapter が使う置き場)
	store := memstore.NewMemStore()

	// adapter
	todos := accessor.NewTodoAccessor(store)
	backups := accessor.NewBackupFileAccessor(cfg.BackupDir)
	notifier, err := client.NewNotifierClient(cfg.ReminderLimit)
	if err != nil {
		return err
	}
	mail, err := client.NewMailClient()
	if err != nil {
		return err
	}

	// usecase
	uc := usecase.NewUseCase(todos, backups, notifier, mail)

	// presentation
	routes := web.NewRoutes(uc)

	// worker
	backupWorker := worker.NewBackupTodosWorker(cfg.BackupEvery, uc)

	// driver (presentation を受けて待ち受ける)
	app := httpserver.NewHTTPServer("todo-app", cfg.Addr, routes)

	return scope.Run(ctx, func(sc *scope.Scope) error {
		sc.Go(app.Run)
		sc.Go(backupWorker.Run)
		return nil
	})
}
