package app

import (
	"bytes"
	"strings"
	"testing"

	"auto-worklog/internal/domain"
)

func TestLoadMeetingsFromJSON(t *testing.T) {
	t.Parallel()

	input := `[
		{"title":"Daily ODP-1001","duration_minutes":30},
		{"title":"Team sync","duration_minutes":45}
	]`

	meetings, err := LoadMeetingsFromJSON(strings.NewReader(input))
	if err != nil {
		t.Fatalf("LoadMeetingsFromJSON returned error: %v", err)
	}

	if len(meetings) != 2 {
		t.Fatalf("len(meetings) = %d, want 2", len(meetings))
	}

	if meetings[0].Title != "Daily ODP-1001" || meetings[0].DurationMinutes != 30 {
		t.Fatalf("unexpected first meeting: %+v", meetings[0])
	}
}

func TestLoadMeetingsFromJSONInvalid(t *testing.T) {
	t.Parallel()

	_, err := LoadMeetingsFromJSON(strings.NewReader(`{"bad":"json"}`))
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

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
