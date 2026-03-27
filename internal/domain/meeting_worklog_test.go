package domain

import "testing"

func TestExtractIssueKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		expected string
	}{
		{name: "valid issue key", title: "Sync ODP-1234 sprint", expected: "ODP-1234"},
		{name: "lowercase should not match", title: "sync odp-1234", expected: ""},
		{name: "wrong project key", title: "ABC-1234 kickoff", expected: ""},
		{name: "no issue key", title: "Team sync", expected: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			actual := ExtractIssueKey(tt.title)
			if actual != tt.expected {
				t.Fatalf("ExtractIssueKey(%q) = %q, want %q", tt.title, actual, tt.expected)
			}
		})
	}
}

func TestBuildMeetingWorklogs(t *testing.T) {
	t.Parallel()

	meetings := []MeetingEvent{
		{Title: "Daily ODP-1111", DurationMinutes: 60},
		{Title: "Planning ODP-1111", DurationMinutes: 30},
		{Title: "Interview https://ihelp/browse/ODP-73459 2ЛТП", DurationMinutes: 45},
		{Title: "Занят", DurationMinutes: 60},
		{Title: "  обед  ", DurationMinutes: 30},
		{Title: "Zero duration ODP-2222", DurationMinutes: 0},
		{Title: "Invalid duration ODP-3333", DurationMinutes: -5},
	}

	got := BuildMeetingWorklogs(meetings, "ODP-2933")

	if got.TotalMinutes != 162 {
		t.Fatalf("TotalMinutes = %d, want 162", got.TotalMinutes)
	}

	if len(got.Items) != 3 {
		t.Fatalf("len(Items) = %d, want 3", len(got.Items))
	}

	expected := []struct {
		issueKey string
		minutes  int
		comment  string
	}{
		{issueKey: "ODP-1111", minutes: 72, comment: `Встреча "Daily ODP-1111"`},
		{issueKey: "ODP-1111", minutes: 36, comment: `Встреча "Planning ODP-1111"`},
		{issueKey: "ODP-73459", minutes: 54, comment: `Встреча "Interview 2ЛТП"`},
	}

	for i, item := range got.Items {
		want := expected[i]
		if item.IssueKey != want.issueKey {
			t.Fatalf("item[%d] issue = %s, want %s", i, item.IssueKey, want.issueKey)
		}
		if item.Minutes != want.minutes {
			t.Fatalf("item[%d] minutes = %d, want %d", i, item.Minutes, want.minutes)
		}
		if item.Source != SourceMeeting {
			t.Fatalf("item[%d] source = %s, want %s", i, item.Source, SourceMeeting)
		}
		if item.Comment != want.comment {
			t.Fatalf("item[%d] comment = %q, want %q", i, item.Comment, want.comment)
		}
	}
}

func TestSanitizeMeetingComment(t *testing.T) {
	t.Parallel()

	in := "https://ihelp/browse/ODP-73459 2ЛТП  sync  https://example.com/a"
	got := sanitizeMeetingComment(in)
	want := "2ЛТП sync"
	if got != want {
		t.Fatalf("sanitizeMeetingComment(%q) = %q, want %q", in, got, want)
	}
}

func TestBuildMeetingWorklogsFallbackComment(t *testing.T) {
	t.Parallel()

	meetings := []MeetingEvent{
		{Title: "Обсуждение без ключа", DurationMinutes: 30},
	}

	got := BuildMeetingWorklogs(meetings, "ODP-2933")
	if len(got.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(got.Items))
	}
	if got.Items[0].IssueKey != "ODP-2933" {
		t.Fatalf("issue = %s, want ODP-2933", got.Items[0].IssueKey)
	}
	if got.Items[0].Comment != "Обсуждение без ключа" {
		t.Fatalf("comment = %q, want %q", got.Items[0].Comment, "Обсуждение без ключа")
	}
}

func TestApplyMeetingBufferMinutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in       int
		expected int
	}{
		{in: 1, expected: 2},
		{in: 30, expected: 36},
		{in: 50, expected: 60},
		{in: 0, expected: 0},
		{in: -1, expected: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run("", func(t *testing.T) {
			t.Parallel()
			actual := ApplyMeetingBufferMinutes(tt.in)
			if actual != tt.expected {
				t.Fatalf("ApplyMeetingBufferMinutes(%d) = %d, want %d", tt.in, actual, tt.expected)
			}
		})
	}
}

func TestIsIgnoredMeetingTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title    string
		expected bool
	}{
		{title: "Занят", expected: true},
		{title: "  обед ", expected: true},
		{title: "ОБЕД", expected: true},
		{title: "Daily", expected: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.title, func(t *testing.T) {
			t.Parallel()
			got := isIgnoredMeetingTitle(tt.title)
			if got != tt.expected {
				t.Fatalf("isIgnoredMeetingTitle(%q) = %v, want %v", tt.title, got, tt.expected)
			}
		})
	}
}
