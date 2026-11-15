package usecase

import (
	"context"

	"github.com/kakkky/gorviz/internal/app/cli/domain"
)

// ExportRecordingsUsecase は server に保存した録画を、区切りごとに取って書き出し先に書く
type ExportRecordingsUsecase struct {
	server          domain.Server
	recordingWriter domain.RecordingWriter
}

func (us *UseCase) NewExportRecordingsUsecase() *ExportRecordingsUsecase {
	return &ExportRecordingsUsecase{server: us.server, recordingWriter: us.recordingWriter}
}

type ExportRecordingsUsecaseInput struct {
	Out string
}

type ExportRecordingsUsecaseOutput struct {
	Exported []ExportedRecording
}

// ExportedRecording は書き出した録画 1 件。Location は書き出した場所
type ExportedRecording struct {
	ID       string
	Chunks   int
	Location string
}

func (us *ExportRecordingsUsecase) Exec(ctx context.Context, in ExportRecordingsUsecaseInput) (ExportRecordingsUsecaseOutput, error) {
	recs, err := us.server.Recordings(ctx)
	if err != nil {
		return ExportRecordingsUsecaseOutput{}, err
	}
	var out ExportRecordingsUsecaseOutput
	for _, r := range recs {
		location, err := us.recordingWriter.Create(in.Out, r.ID)
		if err != nil {
			return out, err
		}
		for n := range r.Chunks {
			raw, err := us.server.RecordingChunk(ctx, r.ID, n)
			if err != nil {
				return out, err
			}
			if err := us.recordingWriter.WriteChunk(location, n, raw); err != nil {
				return out, err
			}
		}
		out.Exported = append(out.Exported, ExportedRecording{ID: r.ID, Chunks: r.Chunks, Location: location})
	}
	return out, nil
}
