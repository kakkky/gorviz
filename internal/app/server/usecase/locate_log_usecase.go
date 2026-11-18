package usecase

import (
	"context"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// logLocateSlack は再生の窓に入るログを、ログに書かれた時刻で先に絞るときの余裕。印の付いた行も、書かれた時刻は書いた瞬間に近い
const logLocateSlack = 5 * time.Second

// LogMark はログ 1 行が録画のどこに当たるか。Exact は gorviz のロガーの差し込み口 (gorviz.NewSlogHandler など) が trace に残した印で、
// 書いた G と時刻まで分かった (FACT) か。Inferred は印はないが、ログの時刻にちょうど 1 つの G だけが走っていたので、その G が書いたと推し量った (INFERENCE) か
type LogMark struct {
	Line      model.LogLine
	At        time.Time
	Goroutine model.GoroutineID
	Exact     bool
	Inferred  bool
}

// HasGoroutine は書いた G が分かったか
func (m LogMark) HasGoroutine() bool {
	return m.Exact || m.Inferred
}

// logLocator はログを録画と突き合わせる。Logs から開くときと、再生にログの印を出すときに使う
type logLocator struct {
	traceRecording domain.TraceRecording
	logRecording   domain.LogRecording
}

func (l logLocator) locate(line model.LogLine) (LogMark, bool) {
	if line.HasTraceSeq {
		if chunk, mark, ok := l.traceRecording.FindLogMark(line.TraceSeq); ok {
			return LogMark{Line: line, At: chunk.StartedAt.Add(mark.At), Goroutine: mark.Goroutine, Exact: true}, true
		}
	}
	chunk, ok := l.traceRecording.At(line.At)
	if !ok {
		return LogMark{}, false
	}
	mark := LogMark{Line: line, At: line.At}
	if g, ok := onlyRunningGoroutine(chunk.Timeline, chunk.Offset(line.At)); ok {
		mark.Goroutine, mark.Inferred = g, true
	}
	return mark, true
}

// marksBetween は [from, to] に当たるログを古い順に返す。全行を trace と突き合わせると重いので、ログに書かれた時刻で先に絞る
func (l logLocator) marksBetween(from, to time.Time) []LogMark {
	var out []LogMark
	for _, line := range l.logRecording.Snapshot() {
		if line.At.Before(from.Add(-logLocateSlack)) || line.At.After(to.Add(logLocateSlack)) {
			continue
		}
		if m, ok := l.locate(line); ok && !m.At.Before(from) && !m.At.After(to) {
			out = append(out, m)
		}
	}
	return out
}

// onlyRunningGoroutine は at の時点で走っていた G がちょうど 1 つならそれを返す。
// ログを書く G は走っているはずだが、ログの時刻は書いた瞬間とずれうるので、候補が 1 つのときだけ推し量る
func onlyRunningGoroutine(tl model.SchedulerTimeline, at time.Duration) (model.GoroutineID, bool) {
	var (
		found model.GoroutineID
		n     int
	)
	for _, p := range tl.StateAt(at).Procs {
		if p.HasRunning && !p.RunningInSyscall {
			found = p.Running
			n++
		}
	}
	return found, n == 1
}

type LocateLogUsecase struct {
	locator logLocator
}

func (us *UseCase) NewLocateLogUsecase() *LocateLogUsecase {
	return &LocateLogUsecase{locator: logLocator{traceRecording: us.traceRecording, logRecording: us.logRecording}}
}

type LocateLogUsecaseInput struct {
	LogID model.LogLineID
}

type LocateLogUsecaseOutput struct {
	Mark LogMark
}

// Exec はログ 1 行を書いた G と時刻を、録画と突き合わせて決める。ログが消えていれば、または録画にその時刻がなければ ErrNotFound
func (us *LocateLogUsecase) Exec(_ context.Context, in LocateLogUsecaseInput) (LocateLogUsecaseOutput, error) {
	line, ok := us.locator.logRecording.Get(in.LogID)
	if !ok {
		return LocateLogUsecaseOutput{}, model.ErrNotFound.With("the log line is no longer kept")
	}
	mark, ok := us.locator.locate(line)
	if !ok {
		return LocateLogUsecaseOutput{}, model.ErrNotFound.With("the trace around this log is not recorded (dropped from the ring buffer, before recording started, or outside the intermittent recording)")
	}
	return LocateLogUsecaseOutput{Mark: mark}, nil
}
