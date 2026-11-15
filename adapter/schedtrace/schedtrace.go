package schedtrace

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kakkky/gorviz/domain"
)

var (
	schedLineRe = regexp.MustCompile(`^SCHED (\d+)ms: (.*)$`)
	procLineRe  = regexp.MustCompile(`^  P(\d+): (.*)$`)
	gLineRe     = regexp.MustCompile(`^  G\d+: status=(\d+)`)
	// detailLineRe は scheddetail の P / M / G の行。対象のログと同じ stderr に出るので、ログの行に割り込まれても見分けられるようにする
	detailLineRe = regexp.MustCompile(`^  [PMG]\d+: `)
)

// runtime2.go の _Pidle などの値
var procStatusNames = map[string]string{"0": "idle", "1": "running", "2": "syscall", "3": "gcstop", "4": "dead"}

// runtime2.go の _Grunnable などの値。_Gleaked / _Gdeadextra は Go 1.26 以降。表示に要らないものは other にまとめる
var goroutineStatusNames = map[int]string{0: "idle", 1: "runnable", 2: "running", 3: "syscall", 4: "waiting", 6: "dead", 9: "preempted", 10: "leaked", 11: "dead"}

// GC のスキャン中は状態に _Gscan ビットが立つ
const goroutineStatusScanBit = 0x1000

// Store は子プロセスの標準エラー出力から SCHED ブロックを読み、最新のものを持つ。
// 対象を schedtrace つきで起動していない (gorviz run 以外の) ときは何も流れてこず、Latest は常に false を返す。
type Store struct {
	mu     sync.Mutex
	latest domain.SchedTraceReport
	ok     bool
}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) Latest() (domain.SchedTraceReport, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latest, s.ok
}

// Consume は r を読み切るまでブロックする。SCHED ブロック以外の行 (対象自身のログ) は passthrough にそのまま流す。
// ブロックの終わりは次の SCHED 行が来るまでわからないので、反映は 1 周期遅れる。
func (s *Store) Consume(r io.Reader, passthrough io.Writer) error {
	var (
		cur     *domain.SchedTraceReport
		inBlock bool
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if m := schedLineRe.FindStringSubmatch(line); m != nil {
			if cur != nil {
				s.publish(*cur)
			}
			cur = parseSchedLine(m[1], m[2])
			inBlock = true
			continue
		}
		if (inBlock && strings.HasPrefix(line, "  ")) || (cur != nil && detailLineRe.MatchString(line)) {
			applyDetailLine(cur, line)
			continue
		}
		inBlock = false
		if _, err := io.WriteString(passthrough, line+"\n"); err != nil {
			return err
		}
	}
	if cur != nil {
		s.publish(*cur)
	}
	return sc.Err()
}

func (s *Store) publish(r domain.SchedTraceReport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest, s.ok = r, true
}

func parseSchedLine(uptimeMs, rest string) *domain.SchedTraceReport {
	ms, _ := strconv.Atoi(uptimeMs)
	kv := parseKV(rest)
	return &domain.SchedTraceReport{
		Uptime:          time.Duration(ms) * time.Millisecond,
		GOMAXPROCS:      atoi(kv["gomaxprocs"]),
		IdleProcs:       atoi(kv["idleprocs"]),
		Threads:         atoi(kv["threads"]),
		SpinningThreads: atoi(kv["spinningthreads"]),
		IdleThreads:     atoi(kv["idlethreads"]),
		GlobalRunQueue:  atoi(kv["runqueue"]),
		GoroutineStates: map[string]int{},
	}
}

func applyDetailLine(r *domain.SchedTraceReport, line string) {
	if m := procLineRe.FindStringSubmatch(line); m != nil {
		kv := parseKV(m[2])
		p := domain.SchedTraceProc{
			Proc:     domain.ProcID(atoi(m[1])),
			Status:   procStatusNames[kv["status"]],
			RunQueue: atoi(kv["runqsize"]),
			GFree:    atoi(kv["gfreecnt"]),
		}
		if m, ok := kv["m"]; ok && m != "nil" {
			p.Thread, p.HasThread = atoi(m), true
		}
		r.Procs = append(r.Procs, p)
		return
	}
	if m := gLineRe.FindStringSubmatch(line); m != nil {
		name, ok := goroutineStatusNames[atoi(m[1])&^goroutineStatusScanBit]
		if !ok {
			name = "other"
		}
		r.GoroutineStates[name]++
	}
}

func parseKV(s string) map[string]string {
	kv := map[string]string{}
	for _, f := range strings.Fields(s) {
		k, v, ok := strings.Cut(f, "=")
		if ok {
			kv[k] = v
		}
	}
	return kv
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
