package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"auto-worklog/internal/app"
	"auto-worklog/internal/domain"
	"auto-worklog/internal/integrations/ews"
	"auto-worklog/internal/integrations/jira"

	"github.com/joho/godotenv"
)

const defaultTimezone = "Europe/Moscow"

func main() {
	if err := loadDotEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load .env: %v\n", err)
	}

	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "plan":
		return runPlan(args[1:], os.Stdout)
	case "apply":
		return runApply(args[1:], os.Stdin, os.Stdout)
	default:
		return usageError()
	}
}

type planOptions struct {
	dateStr  string
	withJira bool
}

func parsePlanOptions(args []string) (planOptions, error) {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dateStr := fs.String("date", "", "date in YYYY-MM-DD (default: today)")
	withJira := fs.Bool("with-jira", true, "include jira activity allocation")

	if err := fs.Parse(args); err != nil {
		return planOptions{}, err
	}

	return planOptions{
		dateStr:  *dateStr,
		withJira: *withJira,
	}, nil
}

func runPlan(args []string, out io.Writer) error {
	opts, err := parsePlanOptions(args)
	if err != nil {
		return err
	}
	allocation, _, err := buildPlanAllocation(opts)
	if err != nil {
		return err
	}
	app.RenderMeetingPlan(out, allocation)
	return nil
}

func runApply(args []string, in io.Reader, out io.Writer) error {
	opts, err := parsePlanOptions(args)
	if err != nil {
		return err
	}
	allocation, date, err := buildPlanAllocation(opts)
	if err != nil {
		return err
	}

	app.RenderMeetingPlan(out, allocation)
	fmt.Fprint(out, "\nApply these worklogs? type 'yes' to continue: ")

	reader := bufio.NewReader(in)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "yes" {
		return errors.New("apply canceled")
	}

	jiraClient := jira.Client{
		BaseURL:  os.Getenv("JIRA_BASE_URL"),
		Email:    os.Getenv("JIRA_EMAIL"),
		APIToken: os.Getenv("JIRA_API_TOKEN"),
	}
	result, err := jiraClient.ApplyWorklogs(context.Background(), date, defaultTimezone, allocation.Items)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Applied: created=%d skipped=%d\n", result.Created, result.Skipped)
	return nil
}

func buildPlanAllocation(opts planOptions) (domain.DailyAllocation, time.Time, error) {
	defaultIssueKey, err := defaultIssueFromEnv()
	if err != nil {
		return domain.DailyAllocation{}, time.Time{}, err
	}

	date, err := resolveDate(opts.dateStr, defaultTimezone)
	if err != nil {
		return domain.DailyAllocation{}, time.Time{}, err
	}

	meetings, err := loadMeetings(date, defaultTimezone)
	if err != nil {
		return domain.DailyAllocation{}, time.Time{}, err
	}
	allocation := domain.BuildMeetingWorklogs(meetings, defaultIssueKey)

	if opts.withJira {
		remaining := max(0, 8*60-allocation.TotalMinutes)
		activity, err := loadJiraAllocation(date, defaultTimezone, remaining, defaultIssueKey)
		if err != nil {
			return domain.DailyAllocation{}, time.Time{}, err
		}
		allocation.Items = append(allocation.Items, activity.Items...)
		allocation.TotalMinutes += activity.TotalMinutes
	}
	return allocation, date, nil
}

func loadMeetings(date time.Time, timezone string) ([]domain.MeetingEvent, error) {
	username := os.Getenv("EWS_USERNAME")
	password := os.Getenv("EWS_PASSWORD")
	if username == "" || password == "" {
		return nil, errors.New("EWS_USERNAME/EWS_PASSWORD are not set")
	}
	client := ews.Client{
		URL:      os.Getenv("EWS_URL"),
		Username: username,
		Password: password,
	}
	return client.FetchMeetings(context.Background(), date, timezone)
}

func usageError() error {
	return errors.New("usage: worklog plan|apply [--date YYYY-MM-DD] [--with-jira]")
}

func loadDotEnv(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return godotenv.Overload(path)
}

func loadJiraAllocation(date time.Time, timezone string, remaining int, defaultIssueKey string) (domain.DailyAllocation, error) {
	if remaining <= 0 {
		return domain.DailyAllocation{}, nil
	}

	baseURL := os.Getenv("JIRA_BASE_URL")
	email := os.Getenv("JIRA_EMAIL")
	token := os.Getenv("JIRA_API_TOKEN")
	client := jira.Client{
		BaseURL:  baseURL,
		Email:    email,
		APIToken: token,
	}
	rules := loadJiraStatusRulesFromEnv()
	intervals, err := client.FetchActivityIntervals(context.Background(), date, timezone, rules)
	if err != nil {
		return domain.DailyAllocation{}, err
	}

	activity := domain.BuildActivityWorklogs(intervals, remaining)
	if len(activity.Items) == 0 && remaining > 0 {
		activity.Items = append(activity.Items, domain.WorklogEntry{
			IssueKey: defaultIssueKey,
			Minutes:  remaining,
			Source:   domain.SourceActivity,
			Comment:  "Fallback: no active Jira issues",
		})
		activity.TotalMinutes = remaining
	}
	return activity, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func loadJiraStatusRulesFromEnv() jira.StatusRules {
	rules := jira.DefaultStatusRules()
	if v := strings.TrimSpace(os.Getenv("JIRA_IGNORED_STATUSES")); v != "" {
		rules.IgnoredStatuses = splitCSV(v)
	}
	if v := strings.TrimSpace(os.Getenv("JIRA_DAY_CLOSE_STATUSES")); v != "" {
		rules.DayCloseStatuses = splitCSV(v)
	}
	return rules
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func defaultIssueFromEnv() (string, error) {
	v := strings.TrimSpace(os.Getenv("DEFAULT_ISSUE"))
	if v == "" {
		return "", errors.New("DEFAULT_ISSUE is not set")
	}
	return v, nil
}

func resolveDate(raw, timezone string) (time.Time, error) {
	if strings.TrimSpace(raw) != "" {
		date, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse --date: %w", err)
		}
		return date, nil
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load timezone %q: %w", timezone, err)
	}
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc), nil
}
