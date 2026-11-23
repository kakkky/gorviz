package client

import (
	"bytes"
	"context"
	"runtime/pprof"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/agent/driver/flightrecorder"
)

// FlightRecorderClient はアプリ自身の runtime から取る (ライブラリ型)。pprof の公開は要らない
type FlightRecorderClient struct {
	fr *flightrecorder.FlightRecorder
}

func NewFlightRecorderClient(fr *flightrecorder.FlightRecorder) (*FlightRecorderClient, error) {
	return &FlightRecorderClient{fr: fr}, nil
}

// FetchExecutionTrace は length 待ってから FlightRecorder の中身を切り出す。前のチャンクと重なるので、重なりは読むときに落とす
func (c *FlightRecorderClient) FetchExecutionTrace(ctx context.Context, _ domain.TargetInstance, length time.Duration) (domain.RawTrace, error) {
	timer := time.NewTimer(length)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return domain.RawTrace{}, domain.ErrInternal.Wrap(ctx.Err(), "wait for the next chunk")
	case <-timer.C:
	}
	var buf bytes.Buffer
	if _, err := c.fr.WriteTo(&buf); err != nil {
		return domain.RawTrace{}, domain.ErrInternal.Wrap(err, "snapshot the flight recorder")
	}
	return domain.RawTrace{Data: buf.Bytes(), ReceivedAt: time.Now()}, nil
}

func (c *FlightRecorderClient) FetchThreadCreateProfile(context.Context, domain.TargetInstance) ([]byte, error) {
	return writeProfile("threadcreate")
}

func (c *FlightRecorderClient) FetchGoroutineLeakProfile(context.Context, domain.TargetInstance) ([]byte, error) {
	return writeProfile("goroutineleak")
}

func writeProfile(name string) ([]byte, error) {
	p := pprof.Lookup(name)
	if p == nil {
		return nil, domain.ErrNotFound.With("the " + name + " profile is not available in this Go")
	}
	var buf bytes.Buffer
	if err := p.WriteTo(&buf, 1); err != nil {
		return nil, domain.ErrInternal.Wrap(err, "write the "+name+" profile")
	}
	return buf.Bytes(), nil
}
