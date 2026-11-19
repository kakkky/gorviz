package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/protocol/model"
)

func TestHistogram_Quantile(t *testing.T) {
	t.Parallel()
	var h, other model.Histogram
	for range 98 {
		h.Observe(3 * time.Microsecond)
	}
	other.Observe(30 * time.Millisecond)
	other.Observe(50 * time.Millisecond)
	h.Merge(other)

	p50, ok := h.Quantile(0.5)
	require.True(t, ok)
	assert.Equal(t, 4*time.Microsecond, p50, "answered by the upper end of the bucket")
	p99, _ := h.Quantile(0.99)
	assert.Equal(t, 32768*time.Microsecond, p99, "30ms falls in the bucket up to 2^15 µs")
	p100, _ := h.Quantile(1)
	assert.Equal(t, 50*time.Millisecond, p100, "never above the max")
	assert.Equal(t, uint64(100), h.N())

	_, ok = model.Histogram{}.Quantile(0.5)
	assert.False(t, ok)
}

func TestIsStdlibFunction(t *testing.T) {
	t.Parallel()
	for fn, want := range map[string]bool{
		"sync.(*Mutex).Lock":                 true,
		"net/http.(*conn).serve":             true,
		"runtime.gopark":                     true,
		"main.handle":                        false,
		"github.com/me/app/store.(*S).Get":   false,
		"golang.org/x/sync/errgroup.(*G).Go": false,
	} {
		assert.Equal(t, want, model.IsStdlibFunction(fn), fn)
	}
}
