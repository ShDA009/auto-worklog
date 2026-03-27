package jira

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"auto-worklog/internal/domain"
)

func TestApplyWorklogsUsesPerIssueCache(t *testing.T) {
	t.Parallel()

	var myselfCalls, listCalls, createCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/api/2/myself":
			myselfCalls++
			_, _ = w.Write([]byte(`{"name":"user@example.com","emailAddress":"user@example.com"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/2/issue/ODP-1/worklog":
			listCalls++
			_, _ = w.Write([]byte(`{"worklogs":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/2/issue/ODP-1/worklog":
			createCalls++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, fmt.Sprintf("unexpected request: %s %s", r.Method, r.URL.Path), http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := Client{
		BaseURL:    server.URL,
		Email:      "user@example.com",
		APIToken:   "token",
		HTTPClient: server.Client(),
	}

	entries := []domain.WorklogEntry{
		{IssueKey: "ODP-1", Minutes: 30, Comment: "A"},
		{IssueKey: "ODP-1", Minutes: 20, Comment: "B"},
	}
	date := time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC)
	res, err := client.ApplyWorklogs(context.Background(), date, "Europe/Moscow", entries)
	if err != nil {
		t.Fatalf("ApplyWorklogs returned error: %v", err)
	}

	if res.Created != 2 || res.Skipped != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if myselfCalls != 1 {
		t.Fatalf("myselfCalls = %d, want 1", myselfCalls)
	}
	if listCalls != 1 {
		t.Fatalf("listCalls = %d, want 1", listCalls)
	}
	if createCalls != 2 {
		t.Fatalf("createCalls = %d, want 2", createCalls)
	}
}
