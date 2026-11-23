package flightrecorder

import (
	"io"
	"runtime/trace"

	"github.com/kakkky/gorviz/internal/app/agent/config"
)

// FlightRecorder は runtime の FlightRecorder で、アプリ自身の直近の execution trace をメモリに持ち続ける (ライブラリ型)。
// trace.Start と同時に動かせるので、アプリが自分で trace を取っても邪魔しない
type FlightRecorder struct {
	fr *trace.FlightRecorder
}

// NewFlightRecorder の MinAge はチャンク 2 つ分。チャンクを切り出すたびに、前のチャンクの終わりより前から持っているようにするため
func NewFlightRecorder(cfg *config.Config) (*FlightRecorder, func(), error) {
	fr := trace.NewFlightRecorder(trace.FlightRecorderConfig{MinAge: 2 * cfg.ChunkLength, MaxBytes: uint64(cfg.FlightRecorderBytes)})
	if err := fr.Start(); err != nil {
		return nil, nil, err
	}
	return &FlightRecorder{fr: fr}, fr.Stop, nil
}

func (f *FlightRecorder) WriteTo(w io.Writer) (int64, error) {
	return f.fr.WriteTo(w)
}
