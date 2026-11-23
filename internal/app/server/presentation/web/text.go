package web

import (
	"strconv"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

const pastRetention = "保持期間を過ぎたため出せない"

func certaintyNote(c domain.Certainty) string {
	switch c {
	case domain.CertaintyFact:
		return "FACT: runtime の判定や trace の記録から直接言える"
	case domain.CertaintyInference:
		return "INFERENCE: gorviz が推移や比較から推し量った"
	}
	return ""
}

func itoa(n int) string { return strconv.Itoa(n) }
