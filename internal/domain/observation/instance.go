// Package observation は、agent が作り server が読み解く観測データと、server がどこまで持っているかを表すカーソル
package observation

type ServiceName string

type InstanceName string

// AgentID は agent の起動ごとの ID。ログの番号は起動ごとに振り直すので、どこまで届いたかはこれと組で持つ
type AgentID string

// Incarnation は同じ名前のインスタンスの、プロセスの 1 回分を見分ける。変わったら別のインスタンスとして扱う
type Incarnation string

type Instance struct {
	Service     ServiceName
	Name        InstanceName
	Incarnation Incarnation
}
