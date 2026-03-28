package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"auto-worklog/internal/domain"
)

type Client struct {
	BaseURL    string
	Email      string
	APIToken   string
	HTTPClient *http.Client
}

type StatusRules struct {
	IgnoredStatuses  []string
	DayCloseStatuses []string
}

type userInfo struct {
	AccountID    string `json:"accountId"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
}

type searchResponse struct {
	Total      int         `json:"total"`
	StartAt    int         `json:"startAt"`
	MaxResults int         `json:"maxResults"`
	Issues     []jiraIssue `json:"issues"`
}

type jiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Created string `json:"created"`
		Status  struct {
			Name string `json:"name"`
		} `json:"status"`
		Assignee *struct {
			AccountID    string `json:"accountId"`
			Key          string `json:"key"`
			Name         string `json:"name"`
			EmailAddress string `json:"emailAddress"`
			DisplayName  string `json:"displayName"`
		} `json:"assignee"`
	} `json:"fields"`
	Changelog struct {
		Histories []history `json:"histories"`
	} `json:"changelog"`
}

type history struct {
	Created string `json:"created"`
	Items   []struct {
		Field      string `json:"field"`
		From       string `json:"from"`
		FromString string `json:"fromString"`
		To         string `json:"to"`
		ToString   string `json:"toString"`
	} `json:"items"`
}

func (c Client) FetchActivityIntervals(
	ctx context.Context,
	date time.Time,
	timezone string,
	rules StatusRules,
) ([]domain.IssueActivityInterval, error) {
	if c.BaseURL == "" || c.Email == "" || c.APIToken == "" {
		return nil, errors.New("JIRA_BASE_URL/JIRA_EMAIL/JIRA_API_TOKEN are not set")
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", timezone, err)
	}
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)

	me, err := c.fetchMyself(ctx)
	if err != nil {
		return nil, err
	}
	issues, err := c.fetchIssuesWithChangelog(ctx, dayStart, dayStart)
	if err != nil {
		return nil, err
	}

	intervals := make([]domain.IssueActivityInterval, 0)
	for _, issue := range issues {
		intervals = append(intervals, buildIssueIntervals(issue, me, dayStart, dayEnd, rules)...)
	}
	return intervals, nil
}

func (c Client) FetchActivityIntervalsForRange(
	ctx context.Context,
	fromDate time.Time,
	toDate time.Time,
	timezone string,
	rules StatusRules,
) ([]domain.IssueActivityInterval, error) {
	if c.BaseURL == "" || c.Email == "" || c.APIToken == "" {
		return nil, errors.New("JIRA_BASE_URL/JIRA_EMAIL/JIRA_API_TOKEN are not set")
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", timezone, err)
	}

	me, err := c.fetchMyself(ctx)
	if err != nil {
		return nil, err
	}

	// Fetch issues for entire date range in one query
	issues, err := c.fetchIssuesWithChangelog(ctx, fromDate, toDate)
	if err != nil {
		return nil, err
	}

	intervals := make([]domain.IssueActivityInterval, 0)
	
	// Process each date in range to extract intervals
	rangeStart := time.Date(fromDate.Year(), fromDate.Month(), fromDate.Day(), 0, 0, 0, 0, loc)
	rangeEnd := toDate.Add(24 * time.Hour)
	
	for d := rangeStart; d.Before(rangeEnd); d = d.AddDate(0, 0, 1) {
		dayStart := d
		dayEnd := d.Add(24 * time.Hour)
		
		for _, issue := range issues {
			intervals = append(intervals, buildIssueIntervals(issue, me, dayStart, dayEnd, rules)...)
		}
	}
	
	return intervals, nil
}

func (c Client) fetchMyself(ctx context.Context) (userInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/rest/api/2/myself", nil)
	if err != nil {
		return userInfo{}, fmt.Errorf("build jira myself request: %w", err)
	}
	req.SetBasicAuth(c.Email, c.APIToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return userInfo{}, fmt.Errorf("jira myself request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return userInfo{}, fmt.Errorf("jira myself status: %s", resp.Status)
	}

	var me userInfo
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return userInfo{}, fmt.Errorf("decode jira myself response: %w", err)
	}
	return me, nil
}

func (c Client) fetchIssuesWithChangelog(ctx context.Context, fromDate, toDate time.Time) ([]jiraIssue, error) {
	base := strings.TrimRight(c.BaseURL, "/") + "/rest/api/2/search"

	jqlTemplate := os.Getenv("JIRA_JQL_TEMPLATE")
	if strings.TrimSpace(jqlTemplate) == "" {
		return nil, errors.New("JIRA_JQL_TEMPLATE is not set")
	}

	// Expand environment variables in template (e.g., ${JIRA_IGNORED_STATUSES})
	jqlTemplate = expandEnvVars(jqlTemplate)

	// Build date string for JQL - use from date as lower bound only
	// (upper bound excluded to capture tasks updated after range end)
	fromTimeStr := fromDate.Format("2006-01-02") + " 00:00"
	
	// Format JQL with only lower date bound
	jql := fmt.Sprintf(jqlTemplate, fromTimeStr)

	startAt := 0
	const pageSize = 100
	all := make([]jiraIssue, 0, pageSize)

	for {
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("startAt", fmt.Sprintf("%d", startAt))
		q.Set("maxResults", fmt.Sprintf("%d", pageSize))
		q.Set("fields", "summary,status,assignee,created")
		q.Set("expand", "changelog")

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("build jira search request: %w", err)
		}
		req.SetBasicAuth(c.Email, c.APIToken)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, fmt.Errorf("jira search request failed: %w", err)
		}

		var result searchResponse
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			fmt.Print(jql+"\n")
			return nil, fmt.Errorf("jira search status: %s\nbody: %s", resp.Status, string(body))
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode jira search response: %w", err)
		}
		resp.Body.Close()

		all = append(all, result.Issues...)
		startAt += len(result.Issues)
		if len(result.Issues) == 0 || startAt >= result.Total {
			break
		}
	}

	return all, nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func buildIssueIntervals(
	issue jiraIssue,
	me userInfo,
	dayStart time.Time,
	dayEnd time.Time,
	rules StatusRules,
) []domain.IssueActivityInterval {
	// Parse issue creation time to avoid showing it on days before it existed
	var issueCreatedAt time.Time
	if issue.Fields.Created != "" {
		if t, err := parseJiraTime(issue.Fields.Created); err == nil {
			issueCreatedAt = t
			if !issueCreatedAt.Before(dayEnd) {
				return nil // issue didn't exist yet on this day
			}
		}
	}

	events := make([]timedChange, 0, len(issue.Changelog.Histories))
	for _, h := range issue.Changelog.Histories {
		tm, err := parseJiraTime(h.Created)
		if err != nil {
			continue
		}
		changes := changeSet{}
		for _, item := range h.Items {
			switch strings.ToLower(item.Field) {
			case "status":
				changes.statusFrom = item.FromString
				changes.statusTo = item.ToString
			case "assignee":
				changes.assigneeFrom = firstNonEmpty(item.FromString, item.From)
				changes.assigneeTo = firstNonEmpty(item.ToString, item.To)
			}
		}
		events = append(events, timedChange{At: tm, Change: changes})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })

	state := issueState{
		status:   issue.Fields.Status.Name,
		assignee: assigneeValue(issue),
	}

	for i := len(events) - 1; i >= 0; i-- {
		if !events[i].At.After(dayStart) {
			continue
		}
		if events[i].Change.statusFrom != "" {
			state.status = events[i].Change.statusFrom
		}
		if events[i].Change.assigneeFrom != "" {
			state.assignee = events[i].Change.assigneeFrom
		}
	}

	// Collect full status chain for the day: starting status + all transitions within [dayStart, dayEnd]
	statusChain := []string{state.status}
	for _, event := range events {
		if event.At.Before(dayStart) || event.At.After(dayEnd) {
			continue
		}
		if event.Change.statusTo != "" && statusChain[len(statusChain)-1] != event.Change.statusTo {
			statusChain = append(statusChain, event.Change.statusTo)
		}
	}

	active := isSelfAssignee(state.assignee, me) && !isIgnoredStatus(state.status, rules)
	cursor := dayStart
	// Don't count time before the issue was created
	if !issueCreatedAt.IsZero() && issueCreatedAt.After(cursor) {
		cursor = issueCreatedAt
	}
	intervals := make([]domain.IssueActivityInterval, 0)

	for _, event := range events {
		if event.At.Before(dayStart) {
			continue
		}
		if event.At.After(dayEnd) {
			break
		}
		if active && event.At.After(cursor) {
			transferredTo := ""
			if event.Change.assigneeTo != "" && !isSelfAssignee(event.Change.assigneeTo, me) {
				transferredTo = event.Change.assigneeTo
			}
			intervals = append(intervals, domain.IssueActivityInterval{
				IssueKey:      issue.Key,
				Summary:       issue.Fields.Summary,
				Status:        state.status,
				StatusTo:      event.Change.statusTo,
				Start:         cursor,
				End:           event.At,
				TransferredTo: transferredTo,
			})
		}
		if event.Change.statusTo != "" {
			state.status = event.Change.statusTo
		}
		if event.Change.assigneeTo != "" {
			state.assignee = event.Change.assigneeTo
		}
		cursor = event.At
		active = isSelfAssignee(state.assignee, me) && !isIgnoredStatus(state.status, rules)
	}

	if active && dayEnd.After(cursor) && (cursor.After(dayStart) || !isDayCloseStatus(state.status, rules)) {
		intervals = append(intervals, domain.IssueActivityInterval{
			IssueKey: issue.Key,
			Summary:  issue.Fields.Summary,
			Status:   state.status,
			Start:    cursor,
			End:      dayEnd,
		})
	}

	// Attach full status chain to first interval
	if len(intervals) > 0 && len(statusChain) > 0 {
		intervals[0].StatusChain = statusChain
	}

	return intervals
}

type timedChange struct {
	At     time.Time
	Change changeSet
}

type changeSet struct {
	statusFrom   string
	statusTo     string
	assigneeFrom string
	assigneeTo   string
}

type issueState struct {
	status   string
	assignee string
}

func parseJiraTime(v string) (time.Time, error) {
	layouts := []string{
		"2006-01-02T15:04:05.000-0700",
		time.RFC3339,
	}
	for _, l := range layouts {
		t, err := time.Parse(l, v)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported jira time format: %s", v)
}

func isStatusActive(status string, statusEnteredToday bool, rules StatusRules) bool {
	if isIgnoredStatus(status, rules) {
		return false
	}
	if isDayCloseStatus(status, rules) {
		return statusEnteredToday
	}
	return true
}

func isIgnoredStatus(status string, rules StatusRules) bool {
	return containsNormalized(rules.IgnoredStatuses, status)
}

func isDayCloseStatus(status string, rules StatusRules) bool {
	return containsNormalized(rules.DayCloseStatuses, status)
}

func normalizeStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func containsNormalized(items []string, value string) bool {
	needle := normalizeStatus(value)
	for _, item := range items {
		if normalizeStatus(item) == needle {
			return true
		}
	}
	return false
}

func expandEnvVars(template string) string {
	// Replace ${VAR_NAME} with os.Getenv("VAR_NAME")
	re := regexp.MustCompile(`\$\{([A-Z_]+)\}`)
	return re.ReplaceAllStringFunc(template, func(match string) string {
		varName := match[2 : len(match)-1] // Extract VAR_NAME from ${VAR_NAME}
		return os.Getenv(varName)
	})
}

func DefaultStatusRules() StatusRules {
	return StatusRules{
		IgnoredStatuses: []string{
			"Новый",
			"New",
		},
		DayCloseStatuses: []string{
			"Закрыт",
			"Closed",
			"Отменен",
			"Отменён",
			"Включен в релиз",
			"Включён в релиз",
			"Отложено",
		},
	}
}

func isSelfAssignee(value string, me userInfo) bool {
	candidates := []string{
		me.AccountID,
		me.Key,
		me.Name,
		me.EmailAddress,
		me.DisplayName,
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(c)) {
			return true
		}
	}
	return false
}

func assigneeValue(issue jiraIssue) string {
	if issue.Fields.Assignee == nil {
		return ""
	}
	return firstNonEmpty(
		issue.Fields.Assignee.AccountID,
		issue.Fields.Assignee.Key,
		issue.Fields.Assignee.Name,
		issue.Fields.Assignee.EmailAddress,
		issue.Fields.Assignee.DisplayName,
	)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
