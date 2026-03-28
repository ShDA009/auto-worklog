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
	hours    int
}

func parsePlanOptions(args []string) (planOptions, error) {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dateStr := fs.String("d", "", "date in YYYY-MM-DD or range YYYY-MM-DD:YYYY-MM-DD (default: today)")
	withJira := fs.Bool("with-jira", true, "include jira activity allocation")
	hours := fs.Int("h", 8, "working hours per day (default: 8)")

	if err := fs.Parse(args); err != nil {
		return planOptions{}, err
	}

	return planOptions{
		dateStr:  *dateStr,
		withJira: *withJira,
		hours:    *hours,
	}, nil
}

func parseDateOrRange(dateStr, timezone string) ([]time.Time, error) {
	if strings.TrimSpace(dateStr) == "" {
		// Default: today
		date, err := resolveDate("", timezone)
		return []time.Time{date}, err
	}

	// Check if it's a range (contains ':')
	if strings.Contains(dateStr, ":") {
		parts := strings.Split(dateStr, ":")
		if len(parts) != 2 {
			return nil, errors.New("invalid date range format, use YYYY-MM-DD:YYYY-MM-DD")
		}

		fromDate, err := time.Parse("2006-01-02", strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, fmt.Errorf("parse from date: %w", err)
		}

		toDate, err := time.Parse("2006-01-02", strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("parse to date: %w", err)
		}

		if toDate.Before(fromDate) {
			return nil, errors.New("to date must be after from date")
		}

		// Generate list of working days (excluding weekends)
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return nil, fmt.Errorf("load timezone %q: %w", timezone, err)
		}

		var dates []time.Time
		for d := fromDate; !d.After(toDate); d = d.AddDate(0, 0, 1) {
			// Skip weekends (Saturday=6, Sunday=0)
			if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
				dates = append(dates, time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc))
			}
		}
		return dates, nil
	}

	// Single date
	date, err := resolveDate(dateStr, timezone)
	return []time.Time{date}, err
}

func loadJiraIntervalsForRange(dates []time.Time, timezone string) ([]domain.IssueActivityInterval, error) {
	if len(dates) == 0 {
		return nil, nil
	}

	baseURL := os.Getenv("JIRA_BASE_URL")
	email := os.Getenv("JIRA_EMAIL")
	token := os.Getenv("JIRA_API_TOKEN")
	
	if baseURL == "" || email == "" || token == "" {
		return nil, nil
	}

	client := jira.Client{
		BaseURL:  baseURL,
		Email:    email,
		APIToken: token,
	}
	rules := loadJiraStatusRulesFromEnv()

	// Fetch intervals for entire date range in one request
	return client.FetchActivityIntervalsForRange(
		context.Background(),
		dates[0],
		dates[len(dates)-1],
		timezone,
		rules,
	)
}

func runPlan(args []string, out io.Writer) error {
	opts, err := parsePlanOptions(args)
	if err != nil {
		return err
	}

	dates, err := parseDateOrRange(opts.dateStr, defaultTimezone)
	if err != nil {
		return err
	}

	// Fetch Jira intervals for entire date range once
	var cachedIntervals []domain.IssueActivityInterval
	if opts.withJira {
		cachedIntervals, _ = loadJiraIntervalsForRange(dates, defaultTimezone)
	}

	for i, date := range dates {
		if i > 0 {
			fmt.Fprintln(out) // Empty line between days
		}

		// Print day header
		fmt.Fprintf(out, "=== %s (%s) ===\n", date.Format("2006-01-02"), date.Format("Monday"))

		allocation, _, err := buildPlanAllocationForDate(date, opts, cachedIntervals)
		if err != nil {
			return err
		}
		app.RenderMeetingPlan(out, allocation)
	}

	return nil
}

func runApply(args []string, in io.Reader, out io.Writer) error {
	opts, err := parsePlanOptions(args)
	if err != nil {
		return err
	}

	dates, err := parseDateOrRange(opts.dateStr, defaultTimezone)
	if err != nil {
		return err
	}

	// Fetch Jira intervals for entire date range once
	var cachedIntervals []domain.IssueActivityInterval
	if opts.withJira {
		cachedIntervals, _ = loadJiraIntervalsForRange(dates, defaultTimezone)
	}

	// Display plan for all dates
	for i, date := range dates {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "=== %s (%s) ===\n", date.Format("2006-01-02"), date.Format("Monday"))

		allocation, _, err := buildPlanAllocationForDate(date, opts, cachedIntervals)
		if err != nil {
			return err
		}
		app.RenderMeetingPlan(out, allocation)
	}

	// Single confirmation for entire range
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

	totalCreated := 0
	totalSkipped := 0

	// Apply worklogs for each date
	for _, date := range dates {
		allocation, _, err := buildPlanAllocationForDate(date, opts, cachedIntervals)
		if err != nil {
			return err
		}

		result, err := jiraClient.ApplyWorklogs(context.Background(), date, defaultTimezone, allocation.Items)
		if err != nil {
			return err
		}

		fmt.Fprintf(out, "%s: created=%d skipped=%d\n", date.Format("2006-01-02"), result.Created, result.Skipped)
		totalCreated += result.Created
		totalSkipped += result.Skipped
	}

	fmt.Fprintf(out, "\nTotal: created=%d skipped=%d\n", totalCreated, totalSkipped)
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
	ignoredMeetings := splitCSV(os.Getenv("EWS_IGNORED_MEETINGS"))
	allocation := domain.BuildMeetingWorklogs(meetings, defaultIssueKey, ignoredMeetings)

	if opts.withJira {
		remaining := max(0, opts.hours*60-allocation.TotalMinutes)
		activity, err := loadJiraAllocation(date, defaultTimezone, remaining, defaultIssueKey, nil)
		if err != nil {
			return domain.DailyAllocation{}, time.Time{}, err
		}
		allocation.Items = append(allocation.Items, activity.Items...)
		allocation.TotalMinutes += activity.TotalMinutes
	}
	return allocation, date, nil
}

func buildPlanAllocationForDate(date time.Time, opts planOptions, cachedIntervals []domain.IssueActivityInterval) (domain.DailyAllocation, time.Time, error) {
	defaultIssueKey, err := defaultIssueFromEnv()
	if err != nil {
		return domain.DailyAllocation{}, time.Time{}, err
	}

	meetings, err := loadMeetings(date, defaultTimezone)
	if err != nil {
		return domain.DailyAllocation{}, time.Time{}, err
	}
	ignoredMeetings := splitCSV(os.Getenv("EWS_IGNORED_MEETINGS"))
	allocation := domain.BuildMeetingWorklogs(meetings, defaultIssueKey, ignoredMeetings)

	if opts.withJira {
		remaining := max(0, opts.hours*60-allocation.TotalMinutes)
		activity, err := loadJiraAllocation(date, defaultTimezone, remaining, defaultIssueKey, cachedIntervals)
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
	return errors.New("usage: worklog plan|apply [-d YYYY-MM-DD|YYYY-MM-DD:YYYY-MM-DD] [--with-jira] [-h N]")
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

func loadJiraAllocation(date time.Time, timezone string, remaining int, defaultIssueKey string, cachedIntervals []domain.IssueActivityInterval) (domain.DailyAllocation, error) {
	if remaining <= 0 {
		return domain.DailyAllocation{}, nil
	}

	isManager := strings.EqualFold(strings.TrimSpace(os.Getenv("IS_MANAGER")), "true")
	managerComment := strings.TrimSpace(os.Getenv("MANAGER_ACTIVITY_COMMENT"))

	// If pre-cached intervals provided, filter for this date only
	if len(cachedIntervals) > 0 {
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return domain.DailyAllocation{}, fmt.Errorf("load timezone: %w", err)
		}
		
		dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
		dayEnd := dayStart.Add(24 * time.Hour)
		
		// Filter intervals for this specific date
		var filtered []domain.IssueActivityInterval
		for _, interval := range cachedIntervals {
			if interval.Start.Before(dayEnd) && interval.End.After(dayStart) {
				filtered = append(filtered, interval)
			}
		}
		
		activity := domain.BuildActivityWorklogs(filtered, remaining)
		if len(activity.Items) == 0 && remaining > 0 {
			if isManager {
				// Manager: allocate unspent time to DEFAULT_ISSUE with manager comment
				activity.Items = append(activity.Items, domain.WorklogEntry{
					IssueKey: defaultIssueKey,
					Minutes:  remaining,
					Source:   domain.SourceActivity,
					Comment:  managerComment,
				})
				activity.TotalMinutes = remaining
			} else {
				// Non-manager: mark time as unallocated, do not add to worklog
				activity.Unallocated = remaining
			}
		}
		return activity, nil
	}

	// Fallback: fetch single day (for backward compatibility)
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
		if isManager {
			// Manager: allocate unspent time to DEFAULT_ISSUE with manager comment
			activity.Items = append(activity.Items, domain.WorklogEntry{
				IssueKey: defaultIssueKey,
				Minutes:  remaining,
				Source:   domain.SourceActivity,
				Comment:  managerComment,
			})
			activity.TotalMinutes = remaining
		} else {
			// Non-manager: mark time as unallocated, do not add to worklog
			activity.Unallocated = remaining
		}
	}
	return activity, nil
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
		part = strings.Trim(part, `"'`)
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
