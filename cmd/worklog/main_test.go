package main

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestRunPlanRejectsFileSource(t *testing.T) {
	t.Parallel()

	err := runPlan([]string{"--source", "file"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunPlanRejectsTimezoneFlag(t *testing.T) {
	t.Parallel()

	err := runPlan([]string{"--timezone", "Europe/Moscow"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	envPath := dir + "/.env"

	if err := os.WriteFile(envPath, []byte("EWS_USERNAME=test.user@rr.ru\nEWS_PASSWORD=secret\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := loadDotEnv(envPath); err != nil {
		t.Fatalf("loadDotEnv returned error: %v", err)
	}

	if got := os.Getenv("EWS_USERNAME"); got != "test.user@rr.ru" {
		t.Fatalf("EWS_USERNAME = %q, want test.user@rr.ru", got)
	}
	if got := os.Getenv("EWS_PASSWORD"); got != "secret" {
		t.Fatalf("EWS_PASSWORD = %q, want secret", got)
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	t.Parallel()

	err := loadDotEnv("/nonexistent/.env")
	if err != nil {
		t.Fatalf("expected nil for missing .env, got: %v", err)
	}
}

func TestRunApplyRequiresDate(t *testing.T) {
	t.Setenv("DEFAULT_ISSUE", "ODP-2933")
	t.Setenv("EWS_USERNAME", "")
	t.Setenv("EWS_PASSWORD", "")
	err := runApply([]string{}, bytes.NewBufferString("no\n"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveDateUsesTodayWhenEmpty(t *testing.T) {
	t.Parallel()

	got, err := resolveDate("", defaultTimezone)
	if err != nil {
		t.Fatalf("resolveDate returned error: %v", err)
	}
	if got.IsZero() {
		t.Fatal("expected non-zero date")
	}
}

func TestResolveDateExplicit(t *testing.T) {
	t.Parallel()

	got, err := resolveDate("2026-03-15", defaultTimezone)
	if err != nil {
		t.Fatalf("resolveDate returned error: %v", err)
	}
	if got.Day() != 15 || got.Month() != time.March || got.Year() != 2026 {
		t.Fatalf("unexpected date: %v", got)
	}
	loc, _ := time.LoadLocation(defaultTimezone)
	if got.Location().String() != loc.String() {
		t.Fatalf("expected timezone %s, got %s", loc, got.Location())
	}
}

func TestResolveDateInvalidFormat(t *testing.T) {
	t.Parallel()

	_, err := resolveDate("not-a-date", defaultTimezone)
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
}

func TestParseDateOrRangeSingleDate(t *testing.T) {
	t.Parallel()

	dates, err := parseDateOrRange("2026-03-27", defaultTimezone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dates) != 1 {
		t.Fatalf("len(dates) = %d, want 1", len(dates))
	}
	if dates[0].Day() != 27 {
		t.Fatalf("day = %d, want 27", dates[0].Day())
	}
}

func TestParseDateOrRangeSkipsWeekends(t *testing.T) {
	t.Parallel()

	// 2026-03-23 Mon through 2026-03-29 Sun = 5 weekdays
	dates, err := parseDateOrRange("2026-03-23:2026-03-29", defaultTimezone)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dates) != 5 {
		t.Fatalf("len(dates) = %d, want 5 (weekdays only)", len(dates))
	}
	for _, d := range dates {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			t.Fatalf("unexpected weekend day: %v", d)
		}
	}
}

func TestParseDateOrRangeInvertedDates(t *testing.T) {
	t.Parallel()

	_, err := parseDateOrRange("2026-03-29:2026-03-23", defaultTimezone)
	if err == nil {
		t.Fatal("expected error for inverted range")
	}
}

func TestParseDateOrRangeInvalidFormat(t *testing.T) {
	t.Parallel()

	_, err := parseDateOrRange("2026-03-23:2026-03-25:2026-03-27", defaultTimezone)
	if err == nil {
		t.Fatal("expected error for triple-range")
	}
}

func TestRunNoArgs(t *testing.T) {
	t.Parallel()

	err := run(nil)
	if err == nil {
		t.Fatal("expected usage error")
	}
}

func TestRunUnknownCommand(t *testing.T) {
	t.Parallel()

	err := run([]string{"unknown"})
	if err == nil {
		t.Fatal("expected usage error")
	}
}

func TestSplitCSV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"a, b, c", 3},
		{`"a", 'b'`, 2},
		{"  ,  ,  ", 0},
	}
	for _, tt := range tests {
		got := splitCSV(tt.input)
		if len(got) != tt.expected {
			t.Fatalf("splitCSV(%q) = %d items, want %d", tt.input, len(got), tt.expected)
		}
	}
}

func TestDefaultIssueFromEnv(t *testing.T) {
	t.Setenv("DEFAULT_ISSUE", "ODP-123")
	got, err := defaultIssueFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ODP-123" {
		t.Fatalf("got %q, want ODP-123", got)
	}
}

func TestDefaultIssueFromEnvMissing(t *testing.T) {
	t.Setenv("DEFAULT_ISSUE", "")
	_, err := defaultIssueFromEnv()
	if err == nil {
		t.Fatal("expected error for empty DEFAULT_ISSUE")
	}
}

func TestParsePlanOptions(t *testing.T) {
	t.Parallel()

	opts, err := parsePlanOptions([]string{"-d", "2026-03-27", "-h", "6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.dateStr != "2026-03-27" {
		t.Fatalf("dateStr = %q, want 2026-03-27", opts.dateStr)
	}
	if opts.hours != 6 {
		t.Fatalf("hours = %d, want 6", opts.hours)
	}
}
