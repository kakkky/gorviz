package usecase

import (
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// observationSet は from から to にかかる秒の Observation を取り出し、問い合わせられるよう重ねる
func observationSet(observations domain.ObservationAccessor, sessions domain.SessionStore, from, to time.Duration) (*domain.ObservationSet, error) {
	obs, err := observations.List(from, to)
	if err != nil {
		return nil, err
	}
	return domain.NewObservationSet(obs, sessions.Session().Observed), nil
}
