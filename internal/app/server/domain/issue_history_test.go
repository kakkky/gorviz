package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kakkky/gorviz/internal/app/api/model"
	"github.com/kakkky/gorviz/internal/app/server/domain"
)

func TestIssueFingerprint(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "LEAK:main.main @ main.go:50", domain.IssueFingerprint(domain.Issue{Kind: model.IssueLeak, Subject: "main.main @ main.go:50"}))
	assert.Equal(t, "CPU_WAIT_SPIKE", domain.IssueFingerprint(domain.Issue{Kind: model.IssueCPUWaitSpike, Subject: "12:00:00"}), "a spike is one issue whatever the time")
}

func TestSummarizeIssues(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	occ := func(fp string, seen time.Duration, summary string) domain.IssueOccurrence {
		return domain.IssueOccurrence{Fingerprint: fp, Summary: summary, SeenAt: base.Add(seen)}
	}
	occurrences := []domain.IssueOccurrence{
		occ("LEAK:a", 0, "10 stuck"),
		occ("LEAK:a", 5*time.Minute, "20 stuck"),
		occ("CPU_WAIT_SPIKE", 2*time.Minute, "spike"),
		occ("LEAK:a", -time.Minute, "out of range"),
	}

	out := domain.SummarizeIssues(occurrences, base, base.Add(10*time.Minute), base.Add(6*time.Minute), 2)

	require.Len(t, out, 2)
	assert.Equal(t, "20 stuck", out[0].Latest.Summary, "the latest occurrence describes the issue")
	assert.Equal(t, 2, out[0].Count)
	assert.True(t, out[0].Active, "seen a minute ago")
	assert.Equal(t, []int{1, 1}, out[0].Trend)
	assert.Equal(t, "CPU_WAIT_SPIKE", out[1].Latest.Fingerprint)
	assert.False(t, out[1].Active, "last seen four minutes ago")
}

func TestMatchesIssueQuery(t *testing.T) {
	t.Parallel()
	o := domain.IssueOccurrence{Kind: model.IssueLeak, Subject: "main.(*Notifier).Register.func1", Summary: "40 goroutines stuck"}
	assert.True(t, domain.MatchesIssueQuery(o, "notifier"))
	assert.True(t, domain.MatchesIssueQuery(o, "leak"))
	assert.True(t, domain.MatchesIssueQuery(o, ""))
	assert.False(t, domain.MatchesIssueQuery(o, "backup"))
}
