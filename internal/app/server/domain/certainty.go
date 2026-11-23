package domain

type Certainty string

const (
	CertaintyFact      Certainty = "FACT"
	CertaintyInference Certainty = "INFERENCE"
)

// Stronger は FACT を INFERENCE より強いとみなす
func (c Certainty) Stronger(o Certainty) Certainty {
	if c == CertaintyFact || o == CertaintyFact {
		return CertaintyFact
	}
	if c == "" {
		return o
	}
	return c
}
