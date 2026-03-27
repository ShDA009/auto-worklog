package app

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"auto-worklog/internal/domain"
)

type meetingInput struct {
	Title           string `json:"title"`
	DurationMinutes int    `json:"duration_minutes"`
}

func LoadMeetingsFromJSON(r io.Reader) ([]domain.MeetingEvent, error) {
	var input []meetingInput
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return nil, fmt.Errorf("decode meetings json: %w", err)
	}

	meetings := make([]domain.MeetingEvent, 0, len(input))
	for _, m := range input {
		meetings = append(meetings, domain.MeetingEvent{
			Title:           m.Title,
			DurationMinutes: m.DurationMinutes,
		})
	}

	return meetings, nil
}

func RenderMeetingPlan(w io.Writer, allocation domain.DailyAllocation) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Issue\tMinutes\tSource\tComment")
	for _, item := range allocation.Items {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", item.IssueKey, item.Minutes, item.Source, item.Comment)
	}
	_ = tw.Flush()
	fmt.Fprintf(w, "\nTotal: %d min\n", allocation.TotalMinutes)
}
