package domain

import "time"

// laterDataLag は、ログが trace のチャンクより先に届くぶんの猶予。agent はチャンクを数秒ごとに切って送るため
const laterDataLag = 30 * time.Second

// Arrival はインスタンスの観測データがどこまで届いたか
type Arrival struct {
	TraceEnd time.Time
	Latest   time.Time
	Ended    bool
}

// MaterialReady は need の時刻までの execution trace が届いたか、もう届かないとみなせるか。
// 1 つのインスタンスの観測データは順に届くので、need より十分後のデータが先に届いたら、抜けは agent が捨てたもの
func (a Arrival) MaterialReady(need time.Time) bool {
	return !a.TraceEnd.Before(need) || a.Ended || !a.Latest.Before(need.Add(laterDataLag))
}
