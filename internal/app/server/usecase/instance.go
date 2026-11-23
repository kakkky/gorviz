package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

// arrivalOf はインスタンスの観測データがどこまで届いたかと、インスタンスが終わったか
func arrivalOf(ctx context.Context, holdings domain.HoldingAccessor, lifecycle domain.LifecycleAccessor, s observation.ServiceName, i observation.InstanceName) (domain.Holding, domain.Arrival, error) {
	h, err := holdings.Holding(ctx, s, i)
	if err != nil {
		return domain.Holding{}, domain.Arrival{}, err
	}
	last, ok, err := lifecycle.LastLifecycle(ctx, s, i)
	if err != nil {
		return domain.Holding{}, domain.Arrival{}, err
	}
	return h, h.Arrival(ok && last.Kind == domain.LifecycleEnd), nil
}

// eachInstance はすべてのサービスのすべてのインスタンスに fn を呼ぶ。1 つが失敗しても、ほかのインスタンスは続ける
func eachInstance(ctx context.Context, inventory domain.InventoryAccessor, fn func(s observation.ServiceName, i observation.InstanceName) error) error {
	services, err := inventory.ListServices(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, s := range services {
		instances, err := inventory.ListInstances(ctx, s)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, i := range instances {
			if ctx.Err() != nil {
				return errors.Join(errs...)
			}
			if err := fn(s, i); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// firstLookback は、チェックポイントがまだないときにさかのぼって作る長さ。保存先に前から残っている分は、直近だけを作る
const firstLookback = time.Hour

// startOf はチェックポイントの続きの時刻。まだなければ、届いている最新から lookback だけさかのぼる
func startOf(cp domain.Checkpoint, h domain.Holding, lookback time.Duration) time.Time {
	if !cp.At.IsZero() {
		return cp.At
	}
	return h.Latest.Add(-lookback)
}
