package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/agent/adapter/accessor"
	"github.com/kakkky/gorviz/internal/app/agent/config"
	"github.com/kakkky/gorviz/internal/app/agent/driver/kvs"
	"github.com/kakkky/gorviz/internal/app/agent/driver/ringbuffer"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

func newStores(t *testing.T) (*accessor.KVSAccessor, *accessor.RingBufferAccessor) {
	t.Helper()
	buf, err := ringbuffer.NewRingBuffer[observation.Instance](&config.Config{BufferBytes: 64 << 20})
	require.NoError(t, err)
	return accessor.NewKVSAccessor(kvs.NewInMemoryKVS()), accessor.NewRingBufferAccessor(buf)
}
