package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const (
	saveBefore = 10 * time.Second
	saveAfter  = 2 * time.Second
)

// SaveRecordingsUsecase は server の側で Issue をきっかけに、その前後の録画を agent から取ってきて保存する。
// agent のリングバッファから消えたり、インスタンスが消えたりしたあとでも調べられるようにする
type SaveRecordingsUsecase struct {
	listIssues     *ListIssuesUsecase
	traceRecording domain.TraceRecording
	recordings     domain.SavedRecordings
}

func (us *UseCase) NewSaveRecordingsUsecase() *SaveRecordingsUsecase {
	return &SaveRecordingsUsecase{listIssues: us.NewListIssuesUsecase(), traceRecording: us.traceRecording, recordings: us.recordings}
}

type SaveRecordingsUsecaseInput struct {
	Now time.Time
}

type SaveRecordingsUsecaseOutput struct {
	Saved []model.SavedRecording
}

// Exec は Issue ごとに 1 分に 1 回まで保存する。前後の録画がすでにリングバッファから消えていれば保存しない
func (us *SaveRecordingsUsecase) Exec(ctx context.Context, in SaveRecordingsUsecaseInput) (SaveRecordingsUsecaseOutput, error) {
	issues, err := us.listIssues.Exec(ctx, ListIssuesUsecaseInput{Now: in.Now})
	if err != nil {
		return SaveRecordingsUsecaseOutput{}, err
	}
	var out SaveRecordingsUsecaseOutput
	for _, i := range issues.Issues {
		if i.At.IsZero() {
			continue
		}
		id := fmt.Sprintf("%s-%d-%s", i.Kind, i.At.Unix()/60, domain.MessageShape(i.Subject))
		id = sanitizeID(id)
		if us.recordings.Has(id) {
			continue
		}
		from, to := i.From.Add(-saveBefore), i.At.Add(saveAfter)
		if to.After(in.Now) {
			// 後ろ側がまだ録れていなければ次の回に回す
			continue
		}
		chunks := us.traceRecording.Between(from, to)
		if len(chunks) == 0 {
			continue
		}
		rec := model.SavedRecording{ID: id, Kind: i.Kind, Subject: i.Subject, At: i.At, From: chunks[0].StartedAt, To: chunks[len(chunks)-1].EndedAt(), Chunks: len(chunks)}
		for _, c := range chunks {
			rec.Bytes += int64(len(c.Raw))
		}
		if err := us.recordings.Save(rec, chunks); err != nil {
			return out, err
		}
		out.Saved = append(out.Saved, rec)
	}
	return out, nil
}

// ListRecordings は保存した録画の一覧
func (us *UseCase) ListRecordings() []model.SavedRecording {
	return us.recordings.List()
}

// RecordingChunks は保存した録画の区切り。CLI で `go tool trace` 向けに書き出すのに使う
func (us *UseCase) RecordingChunks(id string) ([]model.TraceChunk, bool) {
	return us.recordings.Chunks(id)
}

func sanitizeID(s string) string {
	b := []byte(s)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			b[i] = '_'
		}
	}
	if len(b) > 120 {
		b = b[:120]
	}
	return string(b)
}
