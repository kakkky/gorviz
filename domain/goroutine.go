package domain

import "time"

type GoroutineID int64

type GoroutineStateCategory string

const (
	GoroutineRunning  GoroutineStateCategory = "running"
	GoroutineRunnable GoroutineStateCategory = "runnable"
	GoroutineSyscall  GoroutineStateCategory = "syscall"
	GoroutineWaiting  GoroutineStateCategory = "waiting"
)

// Goroutine は goroutine dump の 1 件。State は dump に出る待機理由そのまま ("chan receive" など)。
type Goroutine struct {
	ID    GoroutineID
	State string
	Wait  time.Duration
	// EntryFunction は go 文で起動された関数 (スタックの一番下)。G が生きている間は変わらないので、何の処理をする G かのラベルにする
	EntryFunction string
	// CurrentFunction はスケジューラ内部を除いた、スタックの一番上の関数。今どこで何をしている (止まっている) か
	CurrentFunction string
	CreatedBy       string
	CreatedAt       string
	Leaked          bool
}

func (g Goroutine) StateCategory() GoroutineStateCategory {
	switch g.State {
	case "running":
		return GoroutineRunning
	case "runnable":
		return GoroutineRunnable
	case "syscall":
		return GoroutineSyscall
	default:
		return GoroutineWaiting
	}
}

// Origin は生成元 (go 文の位置) を返す。リークは「同じ go 文から作られた G が溜まり続ける」形で現れるので、集計の単位にする。
func (g Goroutine) Origin() GoroutineOrigin {
	if g.CreatedBy == "" {
		return GoroutineOrigin(g.EntryFunction)
	}
	return GoroutineOrigin(g.CreatedBy + " @ " + g.CreatedAt)
}

type GoroutineOrigin string

type GoroutineStateCounts map[GoroutineStateCategory]int

func CountGoroutineStates(gs []Goroutine) GoroutineStateCounts {
	counts := GoroutineStateCounts{}
	for _, g := range gs {
		counts[g.StateCategory()]++
	}
	return counts
}
