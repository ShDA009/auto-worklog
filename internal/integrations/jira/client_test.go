package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBuildIssueIntervals(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("MSK", 3*60*60)
	dayStart := time.Date(2026, 3, 27, 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)
	me := userInfo{AccountID: "u1"}
	rules := DefaultStatusRules()

	issue := jiraIssue{
		Key: "ODP-10",
	}
	issue.Fields.Status.Name = "In Progress"
	issue.Fields.Assignee = &struct {
		AccountID    string "json:\"accountId\""
		Key          string "json:\"key\""
		Name         string "json:\"name\""
		EmailAddress string "json:\"emailAddress\""
		DisplayName  string "json:\"displayName\""
	}{
		AccountID: "u1",
	}
	issue.Changelog.Histories = []history{
		{
			Created: "2026-03-27T10:00:00.000+0300",
			Items: []struct {
				Field      string "json:\"field\""
				From       string "json:\"from\""
				FromString string "json:\"fromString\""
				To         string "json:\"to\""
				ToString   string "json:\"toString\""
			}{
				{Field: "assignee", To: "u2"},
			},
		},
	}

	got := buildIssueIntervals(issue, me, dayStart, dayEnd, rules)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].IssueKey != "ODP-10" {
		t.Fatalf("issue key = %s, want ODP-10", got[0].IssueKey)
	}
	if !got[0].End.Equal(time.Date(2026, 3, 27, 10, 0, 0, 0, loc)) {
		t.Fatalf("end = %s, want 10:00", got[0].End)
	}
}

func TestBuildIssueIntervalsMatchesAssigneeByKey(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("MSK", 3*60*60)
	dayStart := time.Date(2026, 3, 27, 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)
	me := userInfo{Key: "JIRAUSER83208"}
	rules := DefaultStatusRules()

	issue := jiraIssue{Key: "ODP-69225"}
	issue.Fields.Status.Name = "Подтверждение"
	issue.Fields.Assignee = &struct {
		AccountID    string "json:\"accountId\""
		Key          string "json:\"key\""
		Name         string "json:\"name\""
		EmailAddress string "json:\"emailAddress\""
		DisplayName  string "json:\"displayName\""
	}{
		Key: "JIRAUSER83208",
	}

	got := buildIssueIntervals(issue, me, dayStart, dayEnd, rules)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if !got[0].Start.Equal(dayStart) || !got[0].End.Equal(dayEnd) {
		t.Fatalf("unexpected interval bounds: %+v", got[0])
	}
}

func TestFetchMyselfAndSearch(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv()
	
	// Set required environment variable for the test
	t.Setenv("JIRA_JQL_TEMPLATE", `assignee = currentUser() AND status NOT IN ("New") AND (updated >= "%s")`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/myself":
			_, _ = w.Write([]byte(`{"accountId":"u1","emailAddress":"me@example.com"}`))
		case "/rest/api/2/search":
			_, _ = w.Write([]byte(`{"issues":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := Client{
		BaseURL:    server.URL,
		Email:      "me@example.com",
		APIToken:   "token",
		HTTPClient: server.Client(),
	}

	date := time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC)
	got, err := client.FetchActivityIntervals(context.Background(), date, "Europe/Moscow", DefaultStatusRules())
	if err != nil {
		t.Fatalf("FetchActivityIntervals returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}

func TestBuildIssueIntervalsNewStatusIgnored(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("MSK", 3*60*60)
	dayStart := time.Date(2026, 3, 27, 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)
	me := userInfo{AccountID: "u1"}
	rules := DefaultStatusRules()

	issue := jiraIssue{Key: "ODP-NEW"}
	issue.Fields.Status.Name = "Новый"
	issue.Fields.Assignee = &struct {
		AccountID    string "json:\"accountId\""
		Key          string "json:\"key\""
		Name         string "json:\"name\""
		EmailAddress string "json:\"emailAddress\""
		DisplayName  string "json:\"displayName\""
	}{
		AccountID: "u1",
	}

	got := buildIssueIntervals(issue, me, dayStart, dayEnd, rules)
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}

func TestBuildIssueIntervalsClosedOnlyAfterTodayTransition(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("MSK", 3*60*60)
	dayStart := time.Date(2026, 3, 27, 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)
	me := userInfo{AccountID: "u1"}
	rules := DefaultStatusRules()

	issue := jiraIssue{Key: "ODP-CLOSED"}
	issue.Fields.Status.Name = "Закрыт"
	issue.Fields.Assignee = &struct {
		AccountID    string "json:\"accountId\""
		Key          string "json:\"key\""
		Name         string "json:\"name\""
		EmailAddress string "json:\"emailAddress\""
		DisplayName  string "json:\"displayName\""
	}{
		AccountID: "u1",
	}

	// No transition to closed inside day => no interval.
	got := buildIssueIntervals(issue, me, dayStart, dayEnd, rules)
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}

	// Transition to closed during day => interval counted from transition time.
	issue.Fields.Status.Name = "В работе"
	issue.Changelog.Histories = []history{
		{
			Created: "2026-03-27T12:00:00.000+0300",
			Items: []struct {
				Field      string "json:\"field\""
				From       string "json:\"from\""
				FromString string "json:\"fromString\""
				To         string "json:\"to\""
				ToString   string "json:\"toString\""
			}{
				{Field: "status", FromString: "В работе", ToString: "Закрыт"},
			},
		},
	}

	got = buildIssueIntervals(issue, me, dayStart, dayEnd, rules)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if !got[1].Start.Equal(time.Date(2026, 3, 27, 12, 0, 0, 0, loc)) {
		t.Fatalf("closed interval start = %s, want 12:00", got[1].Start)
	}
}
