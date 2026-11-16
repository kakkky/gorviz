package model

// IssueKind は Issue の種類。どの種類も同じ形 (種類・確かさ・推移・場所・調べに入る先) で見せる
type IssueKind string

const (
	IssueLeak           IssueKind = "LEAK"
	IssueCPUWaitSpike   IssueKind = "CPU_WAIT_SPIKE"
	IssueMemoryPressure IssueKind = "MEMORY_PRESSURE"
)
