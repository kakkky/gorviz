// Package config は gorviz-server の flag
package config

import (
	"flag"
	"fmt"
	"time"
)

type Config struct {
	HTTPAddr string
	// AgentAddr は server が agent を待ち受ける (agent がつなぐ) TCP のアドレス
	AgentAddr string
	// SaveDir は server が Issue の前後の録画を保存するディレクトリ。空なら保存しない (メモリにだけ持つ)
	SaveDir string
	// Refresh は画面 (Live の再生・CLI) を新しくする間隔。対象には触らず、録画済みのものを読むだけ
	Refresh time.Duration
	// TraceWindow は Live に出す長さ。録画の最新の区切りの末尾をこの長さだけ切り出す
	TraceWindow     time.Duration
	LeakMinSamples  int
	LeakMinGrowth   int
	HistorySize     int
	TimelineBuckets int
	MaxLogLines     int
	// Issue の固定の閾値。超えたものは FACT として出す
	CPUWaitRatio float64
	GCPerSecond  float64
	// TargetURL は画面の見出しに出す agent の名前と対象。flag ではなく、Session を作るときに agent ごとに入れる
	TargetURL string
}

func NewConfig(name string, args []string) (*Config, error) {
	var cfg Config
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&cfg.HTTPAddr, "addr", "localhost:7070", "listen address of the GUI and the API (server)")
	fs.StringVar(&cfg.AgentAddr, "agent-addr", "localhost:7071", "TCP address where the server waits for agents and agents connect to")
	fs.StringVar(&cfg.SaveDir, "save-dir", "", "directory where the server saves recordings around issues (empty: memory only)")
	fs.DurationVar(&cfg.Refresh, "refresh", 2*time.Second, "refresh interval of the live views (does not touch the target)")
	fs.DurationVar(&cfg.TraceWindow, "trace-window", 500*time.Millisecond, "length of the latest recorded trace shown in the live view")
	fs.IntVar(&cfg.LeakMinSamples, "leak-min-samples", 5, "dumps required before reporting a growth-based leak suspect")
	fs.IntVar(&cfg.LeakMinGrowth, "leak-min-growth", 10, "minimum non-decreasing growth of waiting goroutines per origin to be a suspect")
	fs.IntVar(&cfg.HistorySize, "history", 30, "number of dumps kept for leak detection")
	fs.IntVar(&cfg.TimelineBuckets, "buckets", 100, "number of columns of the P timeline")
	fs.IntVar(&cfg.MaxLogLines, "max-logs", 50_000, "upper bound of log lines kept in memory")
	fs.Float64Var(&cfg.CPUWaitRatio, "cpu-wait", 1, "average number of goroutines waiting for a P reported as CPU_WAIT_SPIKE")
	fs.Float64Var(&cfg.GCPerSecond, "gc-per-second", 5, "GC cycles per second reported as MEMORY_PRESSURE")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %q", fs.Args())
	}
	return &cfg, nil
}
