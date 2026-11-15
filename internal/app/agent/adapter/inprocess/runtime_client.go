// Package inprocess は、アプリの中から自分自身のランタイム情報を取る RuntimeClient (ライブラリ型)。
// trace は runtime の FlightRecorder で直近を持ち続け、区切りごとに切り出す
package inprocess

import (
	"bytes"
	"context"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"

	"github.com/kakkky/gorviz/internal/app/agent/domain"
	"github.com/kakkky/gorviz/internal/app/api/model"
)

type RuntimeClient struct {
	fr     *trace.FlightRecorder
	parser domain.RuntimeParser
	mu     sync.Mutex
	// last は前に切り出した区切りの終わり。FlightRecorder から切り出すと前の区切りと重なるので、ここより前を落とす
	last time.Time
}

// NewRuntimeClient は FlightRecorder を始める。FlightRecorder は trace.Start と同時に動かせるので、アプリが自分で trace を取っても邪魔しない
func NewRuntimeClient(chunk time.Duration, maxBytes uint64, parser domain.RuntimeParser) (*RuntimeClient, error) {
	fr := trace.NewFlightRecorder(trace.FlightRecorderConfig{MinAge: 2 * chunk, MaxBytes: maxBytes})
	if err := fr.Start(); err != nil {
		return nil, model.ErrUnavailable.Wrap(err, "start the flight recorder")
	}
	return &RuntimeClient{fr: fr, parser: parser}, nil
}

func (c *RuntimeClient) Stop() {
	c.fr.Stop()
}

func (c *RuntimeClient) FetchGoroutines(context.Context) ([]model.Goroutine, error) {
	return c.dump("goroutine")
}

func (c *RuntimeClient) FetchGoroutinesWithLeakCheck(context.Context) ([]model.Goroutine, error) {
	if pprof.Lookup("goroutineleak") == nil {
		return nil, model.ErrNotFound.With("the goroutineleak profile is not available in this Go")
	}
	return c.dump("goroutineleak")
}

// FetchTraceChunk は duration 待ってから FlightRecorder の中身を切り出し、前の区切りと重なる部分を落として返す
func (c *RuntimeClient) FetchTraceChunk(ctx context.Context, duration time.Duration) (model.TraceChunk, error) {
	select {
	case <-ctx.Done():
		return model.TraceChunk{}, ctx.Err()
	case <-time.After(duration):
	}
	var buf bytes.Buffer
	if _, err := c.fr.WriteTo(&buf); err != nil {
		return model.TraceChunk{}, model.ErrUnavailable.Wrap(err, "snapshot the flight recorder")
	}
	chunk, err := c.parser.ParseTrace(buf.Bytes(), time.Now())
	if err != nil {
		return model.TraceChunk{}, err
	}
	chunk.Raw = buf.Bytes()
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.last.IsZero() {
		chunk = chunk.Since(c.last)
	}
	c.last = chunk.EndedAt()
	return chunk, nil
}

func (c *RuntimeClient) dump(profile string) ([]model.Goroutine, error) {
	var buf bytes.Buffer
	if err := pprof.Lookup(profile).WriteTo(&buf, 2); err != nil {
		return nil, model.ErrInternal.Wrap(err, "write the "+profile+" profile")
	}
	return c.parser.ParseGoroutineDump(&buf)
}
