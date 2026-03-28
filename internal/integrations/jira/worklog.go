package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"auto-worklog/internal/domain"
)

type ApplyResult struct {
	Created int
	Skipped int
}

type tempoWorklog struct {
	TempoWorklogID   int    `json:"tempoWorklogId"`
	Comment          string `json:"comment"`
	TimeSpentSeconds int    `json:"timeSpentSeconds"`
	Started          string `json:"started"`
	Worker           string `json:"worker"`
	Issue            struct {
		Key string `json:"key"`
		ID  int    `json:"id"`
	} `json:"issue"`
	OriginTaskID int `json:"originTaskId"`
	Attributes   map[string]struct {
		WorkAttributeID int    `json:"workAttributeId"`
		Value           string `json:"value"`
		Type            string `json:"type"`
		Key             string `json:"key"`
		Name            string `json:"name"`
	} `json:"attributes"`
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
	signatureCache := make(map[string]map[worklogSignature]struct{})
	issueIDCache := make(map[string]int) // issueKey -> numeric ID

	for _, entry := range entries {
		if entry.IssueKey == "" || entry.Minutes <= 0 {
			continue
		}

		if _, ok := signatureCache[entry.IssueKey]; !ok {
			signatures, err := c.loadExistingWorklogSignatures(ctx, me, entry.IssueKey, date, loc)
			if err != nil {
				return result, err
			}
			signatureCache[entry.IssueKey] = signatures
		}

		signature := buildWorklogSignature(entry)
		if _, exists := signatureCache[entry.IssueKey][signature]; exists {
			result.Skipped++
			continue
		}

		// Resolve numeric issue ID for Tempo API
		if _, ok := issueIDCache[entry.IssueKey]; !ok {
			id, err := c.resolveIssueID(ctx, entry.IssueKey)
			if err != nil {
				return result, err
			}
			issueIDCache[entry.IssueKey] = id
		}

		if err := c.createWorklog(ctx, entry, date, loc, me.Key, issueIDCache[entry.IssueKey]); err != nil {
			return result, err
		}
		signatureCache[entry.IssueKey][signature] = struct{}{}
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
	dateStr := date.In(loc).Format("2006-01-02")

	searchBody := map[string]any{
		"from":   dateStr,
		"to":     dateStr,
		"worker": []string{me.Key},
	}
	raw, err := json.Marshal(searchBody)
	if err != nil {
		return nil, fmt.Errorf("marshal tempo search body: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/rest/tempo-timesheets/4/worklogs/search", base),
		bytes.NewReader(raw),
	)
	if err != nil {
		return nil, fmt.Errorf("build tempo worklog search request: %w", err)
	}
	req.SetBasicAuth(c.Email, c.APIToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("tempo worklog search failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("tempo worklog search status: %s\nbody: %s", resp.Status, string(body))
	}

	var worklogs []tempoWorklog
	if err := json.NewDecoder(resp.Body).Decode(&worklogs); err != nil {
		return nil, fmt.Errorf("decode tempo worklog search: %w", err)
	}

	signatures := make(map[worklogSignature]struct{})
	for _, wl := range worklogs {
		if wl.Issue.Key != issueKey {
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
	workerKey string,
	issueID int,
) error {
	started := date.In(loc).Format("2006-01-02")

	body := map[string]any{
		"originTaskId":     issueID,
		"worker":           workerKey,
		"comment":          entry.Comment,
		"timeSpentSeconds": entry.Minutes * 60,
		"started":          started,
	}
	if entry.WorkType != "" {
		body["attributes"] = map[string]any{
			"_Видработ_": map[string]any{
				"workAttributeId": 1,
				"value":           entry.WorkType,
				"type":            "STATIC_LIST",
				"key":             "_Видработ_",
				"name":            "Вид работ",
			},
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal worklog body: %w", err)
	}

	base := strings.TrimRight(c.BaseURL, "/")
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/rest/tempo-timesheets/4/worklogs/", base),
		bytes.NewReader(raw),
	)
	if err != nil {
		return fmt.Errorf("build tempo worklog create request: %w", err)
	}
	req.SetBasicAuth(c.Email, c.APIToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("tempo worklog create failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("tempo worklog create status for %s: %s\nbody: %s", entry.IssueKey, resp.Status, string(respBody))
	}
	return nil
}

func (c Client) resolveIssueID(ctx context.Context, issueKey string) (int, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/rest/api/2/issue/%s?fields=", base, issueKey),
		nil,
	)
	if err != nil {
		return 0, fmt.Errorf("build issue resolve request: %w", err)
	}
	req.SetBasicAuth(c.Email, c.APIToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("issue resolve request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("issue resolve status for %s: %s", issueKey, resp.Status)
	}

	var issue struct {
		ID json.Number `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return 0, fmt.Errorf("decode issue response: %w", err)
	}
	id, err := issue.ID.Int64()
	if err != nil {
		return 0, fmt.Errorf("parse issue ID %q: %w", issue.ID, err)
	}
	return int(id), nil
}

func buildWorklogSignature(entry domain.WorklogEntry) worklogSignature {
	return worklogSignature{
		Comment: strings.TrimSpace(entry.Comment),
		Seconds: entry.Minutes * 60,
	}
}
