package accessor

import (
	"strconv"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/kvs"
)

// ObservationAccessor は 1 秒ごとの Observation を、秒ごとのキーで出し入れする
type ObservationAccessor struct {
	kvs *kvs.InMemoryKVS
}

func NewObservationAccessor(k *kvs.InMemoryKVS) *ObservationAccessor {
	return &ObservationAccessor{kvs: k}
}

func observationKey(second int) string {
	return "observation/" + strconv.Itoa(second)
}

func (a *ObservationAccessor) Put(o domain.Observation) error {
	a.kvs.Set(observationKey(o.Second), o)
	return nil
}

func (a *ObservationAccessor) Get(second int) (domain.Observation, bool) {
	v, ok := a.kvs.Get(observationKey(second))
	if !ok {
		return domain.Observation{}, false
	}
	o, ok := v.(domain.Observation)
	return o, ok
}

// List は from から to にかかる秒の Observation を、秒の順に返す。まだ閉じていない秒は含まない
func (a *ObservationAccessor) List(from, to time.Duration) ([]domain.Observation, error) {
	if to < from {
		return nil, domain.ErrInvalidArgument.With("the end of the range is before its start")
	}
	last := int(to / domain.BucketSize)
	if to > from && to%domain.BucketSize == 0 {
		last--
	}
	var out []domain.Observation
	for s := int(from / domain.BucketSize); s <= last; s++ {
		if o, ok := a.Get(s); ok {
			out = append(out, o)
		}
	}
	return out, nil
}
