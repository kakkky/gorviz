package web

import (
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/usecase"
)

// ongoingWindow は発生が「今も続いている」とみなす直近の長さ
const ongoingWindow = 2 * domain.BucketSize

func certaintyNote(c domain.Certainty) string {
	if c == domain.CertaintyFact {
		return "said by the runtime or recorded in the execution trace"
	}
	return "inferred by gorviz from a threshold, a trend or a stack"
}

func ongoing(st usecase.GetSessionStatusUsecaseOutput, lastAt time.Duration) bool {
	return st.Session.App.State == domain.AppRunning && lastAt >= st.Elapsed-ongoingWindow
}
