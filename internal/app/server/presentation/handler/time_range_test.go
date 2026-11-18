package handler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatLength(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{282 * time.Millisecond, "282ms"},
		{1620 * time.Millisecond, "1.62s"},
		{15 * time.Minute, "15m"},
		{15*time.Minute + 30*time.Second, "15m30s"},
		{time.Hour, "1h"},
		{6*time.Hour + 30*time.Minute, "6h30m"},
		{24 * time.Hour, "1d"},
		{30 * 24 * time.Hour, "30d"},
	} {
		assert.Equal(t, tc.want, formatLength(tc.d), tc.d.String())
	}
}

func TestTimeRange(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 11, 19, 0, 58, 25, 538_000_000, time.Local)

	short := timeRange(from, from.Add(282*time.Millisecond))
	assert.Equal(t, "11/19 00:58:25.538", short.From)
	assert.Equal(t, "11/19 00:58:25.820", short.To)
	assert.Equal(t, "282ms", short.Length)

	long := timeRange(from, from.Add(time.Hour))
	assert.Equal(t, "11/19 00:58:25", long.From)
	assert.Equal(t, "11/19 01:58:25", long.To)
	assert.Equal(t, "1h", long.Length)
}
