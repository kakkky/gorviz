package web

import (
	"strconv"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const pastRetention = "past its retention, so it cannot be shown"

func certaintyNote(c domain.Certainty) string {
	switch c {
	case domain.CertaintyFact:
		return "FACT: read directly from the runtime's judgement or the recorded trace"
	case domain.CertaintyInference:
		return "INFERENCE: inferred by gorviz from trends or comparison"
	}
	return ""
}

func itoa(n int) string { return strconv.Itoa(n) }
