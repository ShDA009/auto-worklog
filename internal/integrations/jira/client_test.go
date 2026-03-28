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

	got := buildIssueIntervals(issue, me, dayStart, dayEnd, DefaultStatusRules())
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

	got := buildIssueIntervals(issue, me, dayStart, dayEnd, DefaultStatusRules())
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

func TestBuildIssueIntervalsDayCloseStatusNoEvents(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("MSK", 3*60*60)
	me := userInfo{AccountID: "u1"}

	// ODP-5384: status "Отложено" with transitions on March 3 only
	// On March 2, the task is in "Отложено" with no events — should NOT appear
	issue := jiraIssue{Key: "ODP-5384"}
	issue.Fields.Summary = "Описать борды по бизнес метрикам НСИ"
	issue.Fields.Status.Name = "Отменен" // current status after all changes
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
			Created: "2026-03-03T10:00:00.000+0300",
			Items: []struct {
				Field      string "json:\"field\""
				From       string "json:\"from\""
				FromString string "json:\"fromString\""
				To         string "json:\"to\""
				ToString   string "json:\"toString\""
			}{
				{Field: "status", FromString: "Отложено", ToString: "Новый"},
			},
		},
		{
			Created: "2026-03-03T11:00:00.000+0300",
			Items: []struct {
				Field      string "json:\"field\""
				From       string "json:\"from\""
				FromString string "json:\"fromString\""
				To         string "json:\"to\""
				ToString   string "json:\"toString\""
			}{
				{Field: "status", FromString: "Новый", ToString: "Отменен"},
			},
		},
	}

	rules := StatusRules{
		IgnoredStatuses:  []string{"Новый"},
		DayCloseStatuses: []string{"Закрыт", "Closed", "Отменен", "Отменён", "Включен в релиз", "Отложено"},
	}

	// March 2: task is in "Отложено" (DayCloseStatus), no events → should NOT appear
	dayStart2 := time.Date(2026, 3, 2, 0, 0, 0, 0, loc)
	dayEnd2 := dayStart2.Add(24 * time.Hour)
	got2 := buildIssueIntervals(issue, me, dayStart2, dayEnd2, rules)
	if len(got2) != 0 {
		t.Fatalf("March 2: len(got) = %d, want 0 (DayCloseStatus with no events)", len(got2))
	}

	// March 3: transitions happen → should appear
	dayStart3 := time.Date(2026, 3, 3, 0, 0, 0, 0, loc)
	dayEnd3 := dayStart3.Add(24 * time.Hour)
	got3 := buildIssueIntervals(issue, me, dayStart3, dayEnd3, rules)
	if len(got3) == 0 {
		t.Fatalf("March 3: len(got) = 0, want > 0 (transitions happened on this day)")
	}

	// March 4: task is in "Отменен" (DayCloseStatus), no events → should NOT appear
	dayStart4 := time.Date(2026, 3, 4, 0, 0, 0, 0, loc)
	dayEnd4 := dayStart4.Add(24 * time.Hour)
	got4 := buildIssueIntervals(issue, me, dayStart4, dayEnd4, rules)
	if len(got4) != 0 {
		t.Fatalf("March 4: len(got) = %d, want 0 (DayCloseStatus with no events)", len(got4))
	}
}

func TestBuildIssueIntervalsStatusTransitions(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("MSK", 3*60*60)
	dayStart := time.Date(2026, 3, 27, 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)
	me := userInfo{AccountID: "u1"}

	issue := jiraIssue{Key: "ODP-CLOSED"}
	issue.Fields.Status.Name = "В работе"
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

	got := buildIssueIntervals(issue, me, dayStart, dayEnd, DefaultStatusRules())
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if !got[1].Start.Equal(time.Date(2026, 3, 27, 12, 0, 0, 0, loc)) {
		t.Fatalf("second interval start = %s, want 12:00", got[1].Start)
	}
	if got[0].StatusChain[0] != "В работе" || got[0].StatusChain[1] != "Закрыт" {
		t.Fatalf("unexpected status chain: %v", got[0].StatusChain)
	}
}
