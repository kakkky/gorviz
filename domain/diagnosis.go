package domain

// Certainty は診断の確かさ。何から判定したかで決まり、どの診断にもどちらも付きうる。
type Certainty string

const (
	// CertaintyFact は runtime の判定か、trace / dump の記録から直接言えること
	CertaintyFact Certainty = "FACT"
	// CertaintyInference は gorviz が推移・比較・時刻の突き合わせなどから推し量ったこと
	CertaintyInference Certainty = "INFERENCE"
)
