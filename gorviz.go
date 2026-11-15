// Package gorviz は、観測される側のアプリが import するパッケージ。
//
// agent を別プロセスにする場合も埋め込む場合も、ロガーに gorviz を差し込む。ログを書いた goroutine を trace から特定できるようになる。
// slog は NewSlogHandler で包み、zap・zerolog・logrus は logger/ の下のアダプタを使う:
//
//	logger := slog.New(gorviz.NewSlogHandler(slog.NewJSONHandler(os.Stderr, nil)))
//
// agent を隣に置けない環境 (sidecar を常時動かせないなど) では、Start で agent をアプリに埋め込む (ライブラリ型)。
// 差し込んだロガーのログは、そのまま埋め込んだ agent にも渡る:
//
//	a, err := gorviz.Start(ctx, gorviz.Options{Server: "gorviz-server:7071"})
//	defer a.Stop()
package gorviz
