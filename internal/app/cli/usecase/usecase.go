package usecase

import "github.com/kakkky/gorviz/internal/app/cli/domain"

// UseCase は CLI の側。server の API から取ったものを返し、録画は書き出し先に書く
type UseCase struct {
	server          domain.Server
	recordingWriter domain.RecordingWriter
}

func NewUseCase(server domain.Server, recordingWriter domain.RecordingWriter) *UseCase {
	return &UseCase{server: server, recordingWriter: recordingWriter}
}
