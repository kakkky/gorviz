// Package gorviz は、観測されるアプリが import するパッケージ。agent を置けない環境で、agent をアプリに埋め込む (ライブラリ型)。
//
// trace は runtime の FlightRecorder から取るので、pprof の公開は要らない。ログは logger/ の下のロガーアダプタから受け取る:
//
//	go func() {
//		if err := gorviz.Run(ctx, gorviz.Options{Server: "gorviz-server:7071", Token: token, Service: "checkout"}); err != nil {
//			slog.Error("gorviz agent stopped", slog.Any("err", err))
//		}
//	}()
//	logger := slog.New(gorvizslog.NewHandler(slog.NewJSONHandler(os.Stderr, nil)))
package gorviz

import (
	"context"
	"fmt"

	"github.com/kakkky/gorviz/internal/app/agent"
	"github.com/kakkky/gorviz/internal/app/agent/config"
)

// Options の空の項目は、環境変数 (gorviz-agent と同じ GORVIZ_*) か既定の値を使う
type Options struct {
	// Server は gorviz-server が agent からの接続を待ち受けるアドレス (host:port)
	Server string
	// Token は gorviz-server が発行した agent のトークン
	Token string
	// Service はサービスの名前。必須
	Service string
	// Instance はインスタンスの名前。省くとホスト名
	Instance string
	// CAFile は server の証明書を確かめる CA。CAFingerprint は server が自分で作った CA のハッシュ (SHA-256 の 16 進)
	CAFile        string
	CAFingerprint string
	// Insecure は TLS を使わずにつなぐ。同じホストの中など、利用者が明示したときだけ使う
	Insecure bool
}

// Run は ctx が終わるまで、アプリの中で agent を動かす。ctx が終わるまで戻らないので、go 文で呼ぶ。
// 1 つのプロセスで同時に動かせる agent は 1 つ。agent の中で起きた panic はアプリに伝えず、エラーとして返す
func Run(ctx context.Context, opts Options) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("gorviz: the embedded agent panicked: %v", r)
		}
	}()
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("gorviz: %w", err)
	}
	cfg.Mode = config.ModeLibrary
	cfg.Insecure = cfg.Insecure || opts.Insecure
	override(&cfg.ServerAddr, opts.Server)
	override(&cfg.Token, opts.Token)
	override(&cfg.Service, opts.Service)
	override(&cfg.Instance, opts.Instance)
	override(&cfg.CAFile, opts.CAFile)
	override(&cfg.CAFingerprint, opts.CAFingerprint)
	if err := agent.RunEmbedded(ctx, cfg); err != nil {
		return fmt.Errorf("gorviz: %w", err)
	}
	return nil
}

func override(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}
