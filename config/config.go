package config

import (
	"flag"
	"time"
)

type Config struct {
	TargetURL string
	HTTPAddr  string
	Interval  time.Duration
	// TraceWindow は 1 回の観測で execution trace を取る長さ。対象のオーバーヘッドに効くので短めにする。
	TraceWindow    time.Duration
	LeakMinSamples int
	LeakMinGrowth  int
	HistorySize    int
	// LeakCheckEvery 回に 1 回だけ goroutineleak を取る。対象で GC が 1 回走るため、毎回の観測では取らない。
	LeakCheckEvery  int
	HTTPTimeout     time.Duration
	TimelineBuckets int
	// ChunkDuration は録画の 1 区切りの長さ。Live の表示はこの間隔でしか新しくならない
	ChunkDuration     time.Duration
	Retention         time.Duration
	MaxRecordedEvents int
	MaxLogLines       int
	// LogsFrom が "-" なら標準入力をログとして取り込む (serve / top のみ)
	LogsFrom string
	// UI と Command は gorviz run のときだけ使う。Command は "--" の後ろ (起動する対象のコマンド)
	UI      string
	Command []string
}

func NewConfig(name string, args []string) (*Config, error) {
	var cfg Config
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&cfg.TargetURL, "target", "http://localhost:6060", "base URL of the target's net/http/pprof server")
	fs.StringVar(&cfg.HTTPAddr, "addr", "localhost:7070", "listen address of the GUI (serve only)")
	fs.DurationVar(&cfg.Interval, "interval", 2*time.Second, "polling interval")
	fs.DurationVar(&cfg.TraceWindow, "trace-window", 500*time.Millisecond, "length of execution trace captured per poll")
	fs.IntVar(&cfg.LeakMinSamples, "leak-min-samples", 5, "samples required before reporting a growth-based leak suspect")
	fs.IntVar(&cfg.LeakMinGrowth, "leak-min-growth", 10, "minimum non-decreasing growth of waiting goroutines per origin to be a suspect")
	fs.IntVar(&cfg.LeakCheckEvery, "leak-check-every", 5, "run the runtime's GC-based leak check once per this many polls (0 disables it)")
	fs.IntVar(&cfg.HistorySize, "history", 30, "number of samples kept for leak detection")
	fs.DurationVar(&cfg.HTTPTimeout, "http-timeout", 10*time.Second, "timeout of each request to the target")
	fs.IntVar(&cfg.TimelineBuckets, "buckets", 100, "number of columns of the P timeline")
	fs.DurationVar(&cfg.ChunkDuration, "chunk", 2*time.Second, "length of each recorded execution trace chunk")
	fs.DurationVar(&cfg.Retention, "retention", 5*time.Minute, "how long recorded traces are kept for replay")
	fs.IntVar(&cfg.MaxRecordedEvents, "max-events", 2_000_000, "upper bound of recorded scheduler events kept in memory")
	fs.IntVar(&cfg.MaxLogLines, "max-logs", 50_000, "upper bound of log lines kept in memory")
	fs.StringVar(&cfg.LogsFrom, "logs", "", `read the target's logs from stdin when "-" (serve / top only; run reads the child's output)`)
	fs.StringVar(&cfg.UI, "ui", "web", `UI to show while running the command: "web" or "top" (run only)`)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	cfg.Command = fs.Args()
	return &cfg, nil
}
