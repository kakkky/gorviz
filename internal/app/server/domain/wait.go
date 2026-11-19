package domain

import (
	"strings"

	"github.com/kakkky/gorviz/internal/app/api/model"
)

// WaitSiteKey は待ち場所を集計する単位。「何の処理をする G が (Entry)、どの行で (Location)、何を呼んで (Function)、何で止まったか (Cause)」
type WaitSiteKey struct {
	Cause    PrimaryCause
	Entry    string
	Function string
	Location string
}

// MarshalText は JSON の map のキーにするため、タブでつないだ 1 つの文字列にする (関数名・パスにタブは入らない)
func (k WaitSiteKey) MarshalText() ([]byte, error) {
	return []byte(strings.Join([]string{string(k.Cause), k.Entry, k.Function, k.Location}, "\t")), nil
}

func (k *WaitSiteKey) UnmarshalText(b []byte) error {
	parts := strings.SplitN(string(b), "\t", 4)
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	*k = WaitSiteKey{Cause: PrimaryCause(parts[0]), Entry: parts[1], Function: parts[2], Location: parts[3]}
	return nil
}

// WaitTotal は待ち場所 1 つで待った量。Seconds は G ごとの待った時間の合計 (秒)、MaxSeconds は 1 回の待ちの最大
type WaitTotal struct {
	Seconds    float64 `json:"seconds"`
	Count      int     `json:"count"`
	MaxSeconds float64 `json:"max"`
	// Reason は runtime の待機理由 ("chan receive"・"sync.Mutex.Lock" など)
	Reason string `json:"reason,omitempty"`
}

func (w *WaitTotal) Merge(o WaitTotal) {
	w.Seconds += o.Seconds
	w.Count += o.Count
	w.MaxSeconds = max(w.MaxSeconds, o.MaxSeconds)
	if w.Reason == "" {
		w.Reason = o.Reason
	}
}

// CauseOfWaitSite は待ち場所の主因。窓より前から待っている G は trace に待機理由が載らない (unknown) ので、そのときは止まった関数から推し量る
func CauseOfWaitSite(s model.WaitSite) PrimaryCause {
	if s.Kind == model.PlaceSyscall {
		if s.Cgo {
			return CauseCGO
		}
		return CauseIOWait
	}
	if s.Reason != "" && s.Reason != model.UnknownWaitReason {
		return CauseOfWaitReason(s.Reason)
	}
	switch {
	case strings.HasPrefix(s.Function, "time.Sleep"):
		return CauseSleep
	case strings.HasPrefix(s.Function, "net.") || strings.HasPrefix(s.Function, "os.") || strings.HasPrefix(s.Function, "internal/poll."):
		return CauseIOWait
	}
	return CauseBlocked
}
