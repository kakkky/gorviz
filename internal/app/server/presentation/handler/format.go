package handler

import (
	"fmt"
	"strings"

	"github.com/kakkky/gorviz/internal/app/server/presentation/view/components"
)

func formatBytes(b uint64) string {
	const mib = 1 << 20
	if b == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f MiB", float64(b)/mib)
}

func toSparklineViewModel(values []int) components.SparklineViewModel {
	vm := components.SparklineViewModel{Samples: len(values)}
	if len(values) == 0 {
		return vm
	}
	vm.Min, vm.Max = values[0], values[0]
	for _, v := range values {
		vm.Min, vm.Max = min(vm.Min, v), max(vm.Max, v)
	}
	points := make([]string, 0, len(values))
	for i, v := range values {
		x := 100.0
		if len(values) > 1 {
			x = float64(i) / float64(len(values)-1) * 100
		}
		y := 50.0
		if vm.Max > vm.Min {
			y = 95 - float64(v-vm.Min)/float64(vm.Max-vm.Min)*90
		}
		points = append(points, fmt.Sprintf("%.1f,%.1f", x, y))
	}
	vm.Points = strings.Join(points, " ")
	return vm
}
