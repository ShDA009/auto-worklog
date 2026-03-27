package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"auto-worklog/internal/domain"
)

type ApplyResult struct {
	Created int
	Skipped int
}

type jiraWorklogList struct {
	Worklogs []jiraWorklog `json:"worklogs"`
}

type jiraWorklog struct {
	Comment          string `json:"comment"`
	TimeSpentSeconds int    `json:"timeSpentSeconds"`
	Started          string `json:"started"`
	Author           struct {
		AccountID    string `json:"accountId"`
		Key          string `json:"key"`
		Name         string `json:"name"`
		EmailAddress string `json:"emailAddress"`
		DisplayName  string `json:"displayName"`
	} `json:"author"`
}

type worklogSignature struct {
	Comment string
	Seconds int
}

func (c Client) ApplyWorklogs(
	ctx context.Context,
	date time.Time,
	timezone string,
	entries []domain.WorklogEntry,
) (ApplyResult, error) {
	me, err := c.fetchMyself(ctx)
	if err != nil {
		return ApplyResult{}, err
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("load timezone %q: %w", timezone, err)
	}

	result := ApplyResult{}
	issueCache := make(map[string]map[worklogSignature]struct{})
	for _, entry := range entries {
		if entry.IssueKey == "" || entry.Minutes <= 0 {
			continue
		}

		if _, ok := issueCache[entry.IssueKey]; !ok {
			signatures, err := c.loadExistingWorklogSignatures(ctx, me, entry.IssueKey, date, loc)
			if err != nil {
				return result, err
			}
			issueCache[entry.IssueKey] = signatures
		}

		signature := buildWorklogSignature(entry)
		if _, exists := issueCache[entry.IssueKey][signature]; exists {
			result.Skipped++
			continue
		}

		if err := c.createWorklog(ctx, entry, date, loc); err != nil {
			return result, err
		}
		issueCache[entry.IssueKey][signature] = struct{}{}
		result.Created++
	}

	return result, nil
}

func (c Client) loadExistingWorklogSignatures(
	ctx context.Context,
	me userInfo,
	issueKey string,
	date time.Time,
	loc *time.Location,
) (map[worklogSignature]struct{}, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	query := url.Values{}
	query.Set("startAt", "0")
	query.Set("maxResults", "200")

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/rest/api/2/issue/%s/worklog?%s", base, issueKey, query.Encode()),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build worklog list request: %w", err)
	}
	req.SetBasicAuth(c.Email, c.APIToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira worklog list failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("jira worklog list status: %s", resp.Status)
	}

	var payload jiraWorklogList
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode jira worklog list: %w", err)
	}

	signatures := make(map[worklogSignature]struct{})
	for _, wl := range payload.Worklogs {
		started, err := parseJiraTime(wl.Started)
		if err != nil {
			continue
		}
		started = started.In(loc)
		if started.Year() != date.Year() || started.Month() != date.Month() || started.Day() != date.Day() {
			continue
		}

		if !isSelfAssignee(firstNonEmpty(
			wl.Author.AccountID,
			wl.Author.Key,
			wl.Author.Name,
			wl.Author.EmailAddress,
			wl.Author.DisplayName,
		), me) {
			continue
		}

		signatures[worklogSignature{
			Comment: strings.TrimSpace(wl.Comment),
			Seconds: wl.TimeSpentSeconds,
		}] = struct{}{}
	}

	return signatures, nil
}

func (c Client) createWorklog(
	ctx context.Context,
	entry domain.WorklogEntry,
	date time.Time,
	loc *time.Location,
) error {
	started := time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, loc).
		Format("2006-01-02T15:04:05.000-0700")

	body := map[string]any{
		"comment":          entry.Comment,
		"timeSpentSeconds": entry.Minutes * 60,
		"started":          started,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal worklog body: %w", err)
	}

	base := strings.TrimRight(c.BaseURL, "/")
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/rest/api/2/issue/%s/worklog", base, entry.IssueKey),
		bytes.NewReader(raw),
	)
	if err != nil {
		return fmt.Errorf("build worklog create request: %w", err)
	}
	req.SetBasicAuth(c.Email, c.APIToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("jira worklog create failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("jira worklog create status for %s: %s", entry.IssueKey, resp.Status)
	}
	return nil
}

func buildWorklogSignature(entry domain.WorklogEntry) worklogSignature {
	return worklogSignature{
		Comment: strings.TrimSpace(entry.Comment),
		Seconds: entry.Minutes * 60,
	}
}
