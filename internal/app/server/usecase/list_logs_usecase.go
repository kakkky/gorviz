package usecase

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const logHistogramBuckets = 60

type ListLogsUsecase struct {
	logRecording   domain.LogRecording
	traceRecording domain.TraceRecording
}

func (us *UseCase) NewListLogsUsecase() *ListLogsUsecase {
	return &ListLogsUsecase{logRecording: us.logRecording, traceRecording: us.traceRecording}
}

// ListLogsUsecaseInput の From / To が零値でなければ、その区間のログだけに絞る
type ListLogsUsecaseInput struct {
	Query string
	Level model.LogLevel
	From  time.Time
	To    time.Time
	Limit int
}

type ListLogsUsecaseOutput struct {
	// Lines は新しい順
	Lines []ListLogsUsecaseLine
	// Histogram は [From, To] を Step ごとに区切った件数 (From / To が零値なら、絞り込んだ行の最初から最後まで)
	Histogram []ListLogsUsecaseBucket
	Step      time.Duration
	Total     int
}

type ListLogsUsecaseLine struct {
	Line model.LogLine
	// Replayable は、その時刻の trace が録画に残っていて再生できるか
	Replayable bool
}

type ListLogsUsecaseBucket struct {
	Start  time.Time
	End    time.Time
	Counts map[model.LogLevel]int
}

func (us *ListLogsUsecase) Exec(_ context.Context, in ListLogsUsecaseInput) (ListLogsUsecaseOutput, error) {
	query := strings.ToLower(in.Query)
	var matched []model.LogLine
	for _, l := range us.logRecording.Snapshot() {
		if in.Level != model.LogLevelUnknown && l.Level != in.Level {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(l.Raw), query) {
			continue
		}
		if (!in.From.IsZero() && l.At.Before(in.From)) || (!in.To.IsZero() && l.At.After(in.To)) {
			continue
		}
		matched = append(matched, l)
	}

	out := ListLogsUsecaseOutput{Total: len(matched)}
	out.Histogram, out.Step = histogram(matched, in.From, in.To)
	for _, l := range slices.Backward(matched) {
		if len(out.Lines) >= in.Limit {
			break
		}
		_, replayable := us.traceRecording.At(l.At)
		if !replayable && l.HasTraceSeq {
			_, _, replayable = us.traceRecording.FindLogMark(l.TraceSeq)
		}
		out.Lines = append(out.Lines, ListLogsUsecaseLine{Line: l, Replayable: replayable})
	}
	return out, nil
}

// logHistogramSteps は件数の区切りに使う長さ。区切りが logHistogramBuckets 個を越えない、いちばん短いものを選ぶ (Overview と同じく切りのよい長さにする)
var logHistogramSteps = []time.Duration{
	time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute,
	15 * time.Minute, 30 * time.Minute, time.Hour, 6 * time.Hour, 24 * time.Hour,
}

func histogram(lines []model.LogLine, from, to time.Time) ([]ListLogsUsecaseBucket, time.Duration) {
	for _, l := range lines {
		if from.IsZero() || l.At.Before(from) {
			from = l.At
		}
		if to.IsZero() || l.At.After(to) {
			to = l.At
		}
	}
	if from.IsZero() || !to.After(from) {
		return nil, 0
	}
	step := logHistogramSteps[len(logHistogramSteps)-1]
	for _, s := range logHistogramSteps {
		if to.Sub(from)/s < logHistogramBuckets {
			step = s
			break
		}
	}
	from = from.Truncate(step)
	n := int(to.Sub(from)/step) + 1
	buckets := make([]ListLogsUsecaseBucket, n)
	for i := range buckets {
		start := from.Add(time.Duration(i) * step)
		buckets[i] = ListLogsUsecaseBucket{Start: start, End: start.Add(step), Counts: map[model.LogLevel]int{}}
	}
	for _, l := range lines {
		if i := int(l.At.Sub(from) / step); i >= 0 && i < n {
			buckets[i].Counts[l.Level]++
		}
	}
	return buckets, step
}
