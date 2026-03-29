package app

import (
	"bytes"
	"strings"
	"testing"

	"auto-worklog/internal/domain"
)

func TestRenderMeetingPlan(t *testing.T) {
	t.Parallel()

	alloc := domain.DailyAllocation{
		TotalMinutes: 90,
		Items: []domain.WorklogEntry{
			{IssueKey: "ODP-1001", Minutes: 36, Source: domain.SourceMeeting, Comment: "Daily ODP-1001"},
			{IssueKey: "ODP-2933", Minutes: 54, Source: domain.SourceMeeting, Comment: "Team sync"},
		},
	}

	var buf bytes.Buffer
	RenderMeetingPlan(&buf, alloc)
	out := buf.String()

	for _, want := range []string{
		"Issue",
		"ODP-1001",
		"ODP-2933",
		"Daily ODP-1001",
		"Team sync",
		"Total: 1.5 hours",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q. output:\n%s", want, out)
		}
	}
}
