package config

import (
	"flag"
	"strings"
	"time"
)

type Config struct {
	TargetURL string
	HTTPAddr  string
	// Refresh は画面 (Live の再生・CLI) を新しくする間隔。対象には触らず、録画済みのものを読むだけ
	Refresh time.Duration
	// DumpInterval は goroutine dump を取る間隔。dump は対象の全 G を止める (STW) ので間を空ける
	DumpInterval time.Duration
	// TraceWindow は Live に出す長さ。録画の最新の区切りの末尾をこの長さだけ切り出す
	TraceWindow    time.Duration
	LeakMinSamples int
	LeakMinGrowth  int
	HistorySize    int
	// LeakCheckEvery 回の dump に 1 回だけ goroutineleak を取る。対象で GC が 1 回走るため、毎回の dump では取らない。
	LeakCheckEvery  int
	HTTPTimeout     time.Duration
	TimelineBuckets int
	// ChunkDuration は録画の 1 区切りの長さ。Live の表示はこの間隔でしか新しくならない
	ChunkDuration     time.Duration
	Retention         time.Duration
	MaxRecordedEvents int
	MaxLogLines       int
	// DurationKeys はログの所要時間として読むキー。JSON の数値は slog の既定と同じくナノ秒、文字列は "1.2s" の形で読む
	DurationKeys []string
	// SlowRequest を超える所要時間のログを遅いリクエストとみなす
	SlowRequest time.Duration
	// Issue の固定の閾値。超えたものは FACT として出す
	ErrorSpikePerMinute int
	CPUWaitRatio        float64
	GCPerSecond         float64
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
	fs.DurationVar(&cfg.Refresh, "refresh", 2*time.Second, "refresh interval of the live views (does not touch the target)")
	fs.DurationVar(&cfg.DumpInterval, "dump-interval", time.Minute, "interval of goroutine dumps (each dump stops the world briefly)")
	fs.DurationVar(&cfg.TraceWindow, "trace-window", 500*time.Millisecond, "length of the latest recorded trace shown in the live view")
	fs.IntVar(&cfg.LeakMinSamples, "leak-min-samples", 5, "dumps required before reporting a growth-based leak suspect")
	fs.IntVar(&cfg.LeakMinGrowth, "leak-min-growth", 10, "minimum non-decreasing growth of waiting goroutines per origin to be a suspect")
	fs.IntVar(&cfg.LeakCheckEvery, "leak-check-every", 5, "run the runtime's GC-based leak check once per this many dumps (0 disables it)")
	fs.IntVar(&cfg.HistorySize, "history", 30, "number of dumps kept for leak detection")
	fs.DurationVar(&cfg.HTTPTimeout, "http-timeout", 10*time.Second, "timeout of each request to the target")
	fs.IntVar(&cfg.TimelineBuckets, "buckets", 100, "number of columns of the P timeline")
	fs.DurationVar(&cfg.ChunkDuration, "chunk", 2*time.Second, "length of each recorded execution trace chunk")
	fs.DurationVar(&cfg.Retention, "retention", 5*time.Minute, "how long recorded traces are kept for replay")
	fs.IntVar(&cfg.MaxRecordedEvents, "max-events", 2_000_000, "upper bound of recorded scheduler events kept in memory")
	fs.IntVar(&cfg.MaxLogLines, "max-logs", 50_000, "upper bound of log lines kept in memory")
	durationKeys := fs.String("duration-keys", "duration,latency,elapsed", "comma separated log keys read as the request duration")
	fs.DurationVar(&cfg.SlowRequest, "slow-request", time.Second, "a request whose logged duration exceeds this is slow")
	fs.IntVar(&cfg.ErrorSpikePerMinute, "error-spike", 10, "errors of the same shape per minute reported as ERROR_SPIKE")
	fs.Float64Var(&cfg.CPUWaitRatio, "cpu-wait", 1, "average number of goroutines waiting for a P reported as CPU_WAIT_SPIKE")
	fs.Float64Var(&cfg.GCPerSecond, "gc-per-second", 5, "GC cycles per second reported as MEMORY_PRESSURE")
	fs.StringVar(&cfg.LogsFrom, "logs", "", `read the target's logs from stdin when "-" (serve / top only; run reads the child's output)`)
	fs.StringVar(&cfg.UI, "ui", "web", `UI to show while running the command: "web" or "top" (run only)`)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	cfg.Command = fs.Args()
	for k := range strings.SplitSeq(*durationKeys, ",") {
		if k = strings.TrimSpace(k); k != "" {
			cfg.DurationKeys = append(cfg.DurationKeys, k)
		}
	}
	return &cfg, nil
}
