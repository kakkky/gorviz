// Package config は gorviz-agent (とアプリに埋め込む agent) の flag
package config

import (
	"flag"
	"fmt"
	"strings"
	"time"
)

type Config struct {
	TargetURL string
	// AgentAddr は server が agent を待ち受ける (agent がつなぐ) TCP のアドレス
	AgentAddr string
	// Name は agent の名前。server はこの名前で agent を束ね、つなぎ直しても同じものとして扱う
	Name string
	// Refresh は画面 (Live の再生・CLI) を新しくする間隔。対象には触らず、録画済みのものを読むだけ
	Refresh time.Duration
	// DumpInterval は goroutine dump を取る間隔。dump は対象の全 G を止める (STW) ので間を空ける
	DumpInterval time.Duration
	// LeakCheckEvery 回の dump に 1 回だけ goroutineleak を取る。対象で GC が 1 回走るため、毎回の dump では取らない。
	LeakCheckEvery int
	HTTPTimeout    time.Duration
	// ChunkDuration は録画の 1 区切りの長さ。Live の表示はこの間隔でしか新しくならない
	ChunkDuration time.Duration
	// TraceBufferBytes は録画のリングバッファの上限。時間ではなくバイト数で決め、超えたら古い区切りから捨てる
	TraceBufferBytes int64
	// RecordMode は trace の録り方。always (既定) は間を空けずに録り続ける。intermittent は RecordEvery ごとに RecordFor だけ録り、負荷の総量を減らす代わりに問題の瞬間を録り逃すことがある
	RecordMode  string
	RecordEvery time.Duration
	RecordFor   time.Duration
	MaxLogLines int
	// DurationKeys はログの所要時間として読むキー。JSON の数値は slog の既定と同じくナノ秒、文字列は "1.2s" の形で読む
	DurationKeys []string
	// LogsFrom が "-" なら標準入力をログとして取り込む
	LogsFrom string
}

func NewConfig(name string, args []string) (*Config, error) {
	var cfg Config
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&cfg.TargetURL, "target", "http://localhost:6060", "base URL of the target's net/http/pprof server")
	fs.StringVar(&cfg.AgentAddr, "agent-addr", "localhost:7071", "TCP address where the server waits for agents and agents connect to")
	fs.StringVar(&cfg.Name, "name", "", "name of the agent (default: the hostname; on Cloud Run the instance ID is appended)")
	fs.DurationVar(&cfg.Refresh, "refresh", 2*time.Second, "refresh interval of the live views (does not touch the target)")
	fs.DurationVar(&cfg.DumpInterval, "dump-interval", time.Minute, "interval of goroutine dumps (each dump stops the world briefly)")
	fs.IntVar(&cfg.LeakCheckEvery, "leak-check-every", 5, "run the runtime's GC-based leak check once per this many dumps (0 disables it)")
	fs.DurationVar(&cfg.HTTPTimeout, "http-timeout", 10*time.Second, "timeout of each request to the target")
	fs.DurationVar(&cfg.ChunkDuration, "chunk", 2*time.Second, "length of each recorded execution trace chunk")
	traceBufferMiB := fs.Int64("trace-buffer-mib", 256, "upper bound of the recorded trace ring buffer in MiB (raw trace and its parsed events)")
	fs.StringVar(&cfg.RecordMode, "record-mode", "always", `how to record the trace: "always" or "intermittent"`)
	fs.DurationVar(&cfg.RecordEvery, "record-every", 15*time.Minute, "interval of recording in the intermittent mode")
	fs.DurationVar(&cfg.RecordFor, "record-for", time.Minute, "length of each recording in the intermittent mode")
	fs.IntVar(&cfg.MaxLogLines, "max-logs", 50_000, "upper bound of log lines kept in memory")
	durationKeys := fs.String("duration-keys", "duration,latency,elapsed", "comma separated log keys read as the request duration")
	fs.StringVar(&cfg.LogsFrom, "logs", "", `read the target's logs from stdin when "-"`)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %q", fs.Args())
	}
	cfg.TraceBufferBytes = *traceBufferMiB << 20
	if cfg.RecordMode != "always" && cfg.RecordMode != "intermittent" {
		return nil, fmt.Errorf(`-record-mode must be "always" or "intermittent", got %q`, cfg.RecordMode)
	}
	for k := range strings.SplitSeq(*durationKeys, ",") {
		if k = strings.TrimSpace(k); k != "" {
			cfg.DurationKeys = append(cfg.DurationKeys, k)
		}
	}
	return &cfg, nil
}
