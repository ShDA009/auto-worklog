package domain

import (
	"testing"
	"time"
)

func TestBuildActivityWorklogs(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	intervals := []IssueActivityInterval{
		{IssueKey: "ODP-1", Summary: "Task one", Status: "In Progress", Start: base, End: base.Add(60 * time.Minute)},
		{IssueKey: "ODP-2", Summary: "Task two", Status: "In Progress", Start: base.Add(30 * time.Minute), End: base.Add(90 * time.Minute)},
	}

	got := BuildActivityWorklogs(intervals, 120)
	if got.TotalMinutes != 120 {
		t.Fatalf("TotalMinutes = %d, want 120", got.TotalMinutes)
	}
	if len(got.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(got.Items))
	}

	// ODP-1 and ODP-2 should have equal weighted activity in this setup.
	if got.Items[0].Minutes != 60 || got.Items[1].Minutes != 60 {
		t.Fatalf("expected 60/60 distribution, got %d/%d", got.Items[0].Minutes, got.Items[1].Minutes)
	}
	if got.Items[0].Comment == "" || got.Items[1].Comment == "" {
		t.Fatalf("expected non-empty comments, got %+v", got.Items)
	}
}

func TestBuildActivityWorklogsConfirmationPrefix(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	intervals := []IssueActivityInterval{
		{IssueKey: "ODP-1", Summary: "Инструкции по установке", Status: "Подтверждение", Start: base, End: base.Add(30 * time.Minute)},
	}

	got := BuildActivityWorklogs(intervals, 30)
	if len(got.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(got.Items))
	}
	if got.Items[0].Comment != `Проверка "Инструкции по установке"` {
		t.Fatalf("comment = %q, want %q", got.Items[0].Comment, `Проверка "Инструкции по установке"`)
	}
}

func TestBuildActivityWorklogsEmpty(t *testing.T) {
	t.Parallel()

	got := BuildActivityWorklogs(nil, 90)
	if got.TotalMinutes != 0 || len(got.Items) != 0 {
		t.Fatalf("expected empty allocation, got %+v", got)
	}
}

func TestBuildActivityWorklogsNoRemaining(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC)
	intervals := []IssueActivityInterval{
		{IssueKey: "ODP-1", Start: base, End: base.Add(60 * time.Minute)},
	}

	got := BuildActivityWorklogs(intervals, 0)
	if got.TotalMinutes != 0 || len(got.Items) != 0 {
		t.Fatalf("expected empty allocation, got %+v", got)
	}
}
