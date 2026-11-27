package accessor

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"runtime/coverage"
	"runtime/metrics"
	"runtime/pprof"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/driver/tracer"
)

// AppRuntimeAccessor は runtime が持つ状態 (trace・coverage のカウンタ・goroutine・スレッド) を読み書きする
type AppRuntimeAccessor struct {
	tracer *tracer.ExecutionTracer
}

func NewAppRuntimeAccessor(t *tracer.ExecutionTracer) *AppRuntimeAccessor {
	return &AppRuntimeAccessor{tracer: t}
}

func (a *AppRuntimeAccessor) GoVersion() string { return runtime.Version() }

func (a *AppRuntimeAccessor) Procs() int { return runtime.GOMAXPROCS(0) }

func (a *AppRuntimeAccessor) PID() int { return os.Getpid() }

func (a *AppRuntimeAccessor) StartTrace() error {
	if err := a.tracer.Start(); err != nil {
		return domain.ErrInternal.Wrap(err, "start execution trace")
	}
	return nil
}

func (a *AppRuntimeAccessor) ReadTrace(p []byte) (int, error) {
	return a.tracer.Read(p)
}

func (a *AppRuntimeAccessor) StopTrace() { a.tracer.Stop() }

func (a *AppRuntimeAccessor) WaitTraceDrained() { <-a.tracer.Drained() }

func (a *AppRuntimeAccessor) WriteTraceEvent(event string) { a.tracer.Log(event) }

func (a *AppRuntimeAccessor) ResetCoverage() error {
	if err := coverage.ClearCounters(); err != nil {
		return domain.ErrInternal.Wrap(err, "clear coverage counters")
	}
	return nil
}

// ReadCoverage は runtime/coverage が決めた名前のまま読む。server が go tool covdata に渡すとき、名前でメタデータとカウンタを結ぶため
func (a *AppRuntimeAccessor) ReadCoverage() ([]domain.CoverageFile, error) {
	dir, err := os.MkdirTemp("", "gorviz-coverage-")
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "make coverage dir")
	}
	defer os.RemoveAll(dir)
	if err := coverage.WriteMetaDir(dir); err != nil {
		return nil, domain.ErrInternal.Wrap(err, "write coverage meta")
	}
	if err := coverage.WriteCountersDir(dir); err != nil {
		return nil, domain.ErrInternal.Wrap(err, "write coverage counters")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "list coverage files")
	}
	files := make([]domain.CoverageFile, 0, len(entries))
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, domain.ErrInternal.Wrap(err, "read coverage file")
		}
		files = append(files, domain.CoverageFile{Name: e.Name(), Data: b})
	}
	return files, nil
}

func (a *AppRuntimeAccessor) NextLeakCheckSeq() uint64 { return a.tracer.NextLeakCheckSeq() }

func (a *AppRuntimeAccessor) ReadGoroutineLeakProfile() ([]byte, error) {
	p := pprof.Lookup("goroutineleak")
	if p == nil {
		return nil, domain.ErrNotFound.With("goroutineleak profile")
	}
	var b bytes.Buffer
	if err := p.WriteTo(&b, 1); err != nil {
		return nil, domain.ErrInternal.Wrap(err, "write goroutineleak profile")
	}
	return b.Bytes(), nil
}

// ThreadCount は、runtime が持っている今の OS のスレッドの数
func (a *AppRuntimeAccessor) ThreadCount() int {
	s := []metrics.Sample{{Name: "/sched/threads/total:threads"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return int(s[0].Value.Uint64())
}
