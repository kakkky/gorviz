// Package gorvizzap は zap のログを gorviz に結ぶ。
//
//	core := zapcore.NewCore(enc, gorvizzap.WrapWriteSyncer(zapcore.AddSync(os.Stderr)), zap.InfoLevel)
//	logger := zap.New(gorvizzap.WrapCore(core))
package gorvizzap

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kakkky/gorviz/internal/protocol/loggerhook"
)

// WrapCore は core を包み、ログ 1 行ごとに番号を振ってフィールドに足し、同じ番号を runtime/trace のログイベントとして書き込む
func WrapCore(core zapcore.Core) zapcore.Core {
	return &markCore{Core: core}
}

type markCore struct {
	zapcore.Core
}

func (c *markCore) With(fields []zapcore.Field) zapcore.Core {
	return &markCore{Core: c.Core.With(fields)}
}

// Check は包んだ core の判定 (レベル・サンプリング) に従う。包んだ core を ce に積むと番号を足す前に書かれてしまうので、積むのは自分にする
func (c *markCore) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Core.Check(e, nil) == nil {
		return ce
	}
	return ce.AddCore(e, c)
}

func (c *markCore) Write(e zapcore.Entry, fields []zapcore.Field) error {
	if n, ok := loggerhook.Mark(context.Background()); ok {
		fields = append(fields[:len(fields):len(fields)], zap.Uint64(loggerhook.SeqKey, n))
	}
	return c.Core.Write(e, fields)
}

// WrapWriteSyncer は ws を包み、書かれたログ行を、アプリに埋め込んだ agent (gorviz.Run) にもそのまま渡す
func WrapWriteSyncer(ws zapcore.WriteSyncer) zapcore.WriteSyncer {
	return agentWriteSyncer{WriteSyncer: ws}
}

type agentWriteSyncer struct {
	zapcore.WriteSyncer
}

func (w agentWriteSyncer) Write(p []byte) (int, error) {
	n, err := w.WriteSyncer.Write(p)
	loggerhook.Forward(p)
	return n, err
}
