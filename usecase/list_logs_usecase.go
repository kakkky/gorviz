package usecase

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/kakkky/gorviz/domain"
)

const logHistogramBuckets = 60

type ListLogsUsecase struct {
	logRecording   domain.LogRecording
	traceRecording domain.TraceRecording
}

func (us *UseCase) NewListLogsUsecase() *ListLogsUsecase {
	return &ListLogsUsecase{logRecording: us.logRecording, traceRecording: us.traceRecording}
}

type ListLogsUsecaseInput struct {
	Query string
	Level domain.LogLevel
	Limit int
}

type ListLogsUsecaseOutput struct {
	// Lines は新しい順
	Lines     []ListLogsUsecaseLine
	Histogram []ListLogsUsecaseBucket
	Total     int
}

type ListLogsUsecaseLine struct {
	Line domain.LogLine
	// Replayable は、その時刻の trace が録画に残っていて再生できるか
	Replayable bool
}

type ListLogsUsecaseBucket struct {
	Start  time.Time
	End    time.Time
	Counts map[domain.LogLevel]int
}

func (us *ListLogsUsecase) Exec(_ context.Context, in ListLogsUsecaseInput) (ListLogsUsecaseOutput, error) {
	query := strings.ToLower(in.Query)
	var matched []domain.LogLine
	for _, l := range us.logRecording.Snapshot() {
		if in.Level != domain.LogLevelUnknown && l.Level != in.Level {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(l.Raw), query) {
			continue
		}
		matched = append(matched, l)
	}

	out := ListLogsUsecaseOutput{Total: len(matched), Histogram: histogram(matched)}
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

func histogram(lines []domain.LogLine) []ListLogsUsecaseBucket {
	if len(lines) == 0 {
		return nil
	}
	from, to := lines[0].At, lines[0].At
	for _, l := range lines {
		if l.At.Before(from) {
			from = l.At
		}
		if l.At.After(to) {
			to = l.At
		}
	}
	width := max(to.Sub(from)/logHistogramBuckets+1, time.Second)
	n := int(to.Sub(from)/width) + 1
	buckets := make([]ListLogsUsecaseBucket, n)
	for i := range buckets {
		start := from.Add(time.Duration(i) * width)
		buckets[i] = ListLogsUsecaseBucket{Start: start, End: start.Add(width), Counts: map[domain.LogLevel]int{}}
	}
	for _, l := range lines {
		buckets[int(l.At.Sub(from)/width)].Counts[l.Level]++
	}
	return buckets
}
