package handler

import "fmt"

func formatBytes(b uint64) string {
	const mib = 1 << 20
	if b == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f MiB", float64(b)/mib)
}
