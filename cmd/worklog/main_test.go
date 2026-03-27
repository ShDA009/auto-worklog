package main

import (
	"bytes"
	"os"
	"testing"
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
