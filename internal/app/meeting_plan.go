package app

import (
	"fmt"
	"io"
	"text/tabwriter"

	"auto-worklog/internal/domain"
)

func RenderMeetingPlan(w io.Writer, allocation domain.DailyAllocation) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Issue\tMinutes\tSource\tComment")
	for _, item := range allocation.Items {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", item.IssueKey, item.Minutes, item.Source, item.Comment)
	}
	_ = tw.Flush()
	
	hours := float64(allocation.TotalMinutes) / 60.0
	fmt.Fprintf(w, "\nTotal: %.1f hours\n", hours)
	
	if allocation.Unallocated > 0 {
		unallocatedHours := float64(allocation.Unallocated) / 60.0
		fmt.Fprintf(w, "⚠ Unallocated: %.1f hours - requires manual distribution\n", unallocatedHours)
	}
}
