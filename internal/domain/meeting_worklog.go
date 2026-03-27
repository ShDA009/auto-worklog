package domain

import (
	"math"
	"regexp"
	"strings"
)

const (
	SourceMeeting = "meeting"
)

var issueKeyRegexp = regexp.MustCompile(`\bODP-\d+\b`)
var urlRegexp = regexp.MustCompile(`https?://\S+`)

type MeetingEvent struct {
	Title           string
	DurationMinutes int
}

type WorklogEntry struct {
	IssueKey string
	Minutes  int
	Source   string
	Comment  string
}

type DailyAllocation struct {
	Items        []WorklogEntry
	TotalMinutes int
}

func ExtractIssueKey(title string) string {
	return issueKeyRegexp.FindString(title)
}

func ApplyMeetingBufferMinutes(durationMinutes int) int {
	if durationMinutes <= 0 {
		return 0
	}

	return int(math.Ceil(float64(durationMinutes) * 1.2))
}

func BuildMeetingWorklogs(meetings []MeetingEvent, defaultIssue string) DailyAllocation {
	allocation := DailyAllocation{
		Items: make([]WorklogEntry, 0, len(meetings)),
	}

	for _, meeting := range meetings {
		if isIgnoredMeetingTitle(meeting.Title) {
			continue
		}

		bufferedMinutes := ApplyMeetingBufferMinutes(meeting.DurationMinutes)
		if bufferedMinutes <= 0 {
			continue
		}

		issueKey := ExtractIssueKey(meeting.Title)
		if issueKey == "" {
			issueKey = defaultIssue
		}
		comment := sanitizeMeetingComment(meeting.Title)
		if ExtractIssueKey(meeting.Title) != "" {
			comment = `Встреча "` + comment + `"`
		}

		allocation.Items = append(allocation.Items, WorklogEntry{
			IssueKey: issueKey,
			Minutes:  bufferedMinutes,
			Source:   SourceMeeting,
			Comment:  comment,
		})
		allocation.TotalMinutes += bufferedMinutes
	}

	return allocation
}

func isIgnoredMeetingTitle(title string) bool {
	normalized := strings.ToLower(strings.TrimSpace(title))
	return normalized == "занят" || normalized == "обед"
}

func sanitizeMeetingComment(title string) string {
	clean := urlRegexp.ReplaceAllString(title, "")
	clean = strings.Join(strings.Fields(clean), " ")
	return strings.TrimSpace(clean)
}
