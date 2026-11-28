package domain

import "time"

// 終了コード。Issue がなかったか・あったか・診断できなかったか
const (
	ExitNoIssues     = 0
	ExitIssues       = 1
	ExitNotDiagnosed = 2
)

// NoScenarioWatch はシナリオを渡さず、終わらないアプリを見る時間
const NoScenarioWatch = 30 * time.Second

type StepResult struct {
	Method string
	Path   string
	Status int
	Err    string
	Passed bool
}

type SmokeResult struct {
	Flow   string
	Passed bool
	Steps  []StepResult
}

// DiagnosisProgress は診断の途中で流す 1 行。phase (ramp-up は level) が終わるたびに出す
type DiagnosisProgress struct {
	Phase string
	Smoke []SmokeResult
	Load  *LoadStats
	Note  string
}

type DiagnosedIssue struct {
	ID          IssueID
	Kind        IssueKind
	Target      string
	Certainty   Certainty
	FirstPhase  string
	Occurrences int
}

// DiagnosisSummary は診断 1 回の要約。rps・レイテンシ・ステータスの内訳は、返したら持たない
type DiagnosisSummary struct {
	From         time.Duration
	To           time.Duration
	Loaded       bool
	Smoke        []SmokeResult
	Levels       []LoadStats
	Cancellation *LoadStats
	Issues       []DiagnosedIssue
	Stopped      string
	AppExited    bool
	AppExitCode  int
	ExitCode     int
}

func SmokePassed(rs []SmokeResult) (passed, total int) {
	for _, r := range rs {
		if r.Passed {
			passed++
		}
	}
	return passed, len(rs)
}

// DiagnosedIssues は診断の期間にかかった Issue を、出始めた順に並べる
func DiagnosedIssues(book *IssueBook, from, to time.Duration) []DiagnosedIssue {
	var out []DiagnosedIssue
	for _, is := range book.IssuesIn(from, to) {
		first := ""
		n := 0
		for _, o := range is.Occurrences {
			if o.From <= to && from <= o.To {
				n++
				if first == "" {
					first = o.Phase
				}
			}
		}
		out = append(out, DiagnosedIssue{ID: is.ID, Kind: is.Kind, Target: is.TargetLabel(), Certainty: is.Certainty(), FirstPhase: first, Occurrences: n})
	}
	return out
}

func DiagnosisExitCode(s DiagnosisSummary) int {
	switch {
	case s.Stopped != "":
		return ExitNotDiagnosed
	case len(s.Issues) > 0:
		return ExitIssues
	}
	return ExitNoIssues
}
