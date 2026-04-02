package domain

import (
	"math"
	"sort"
	"strings"
	"time"
)

const (
	SourceActivity = "activity"
)

type IssueActivityInterval struct {
	IssueKey      string
	Summary       string
	Status        string
	StatusTo      string   // non-empty if this interval ended with a status transition
	StatusChain   []string // full ordered status chain for the day (set on first interval only)
	Start         time.Time
	End           time.Time
	TransferredTo string
	IsCreation    bool // true when the user is the reporter and there is no changelog
}

func BuildActivityWorklogs(intervals []IssueActivityInterval, remainingMinutes int) DailyAllocation {
	allocation := DailyAllocation{
		Items: make([]WorklogEntry, 0),
	}
	if remainingMinutes <= 0 {
		return allocation
	}

	weights := buildIssueWeights(intervals)
	if len(weights) == 0 {
		return allocation
	}
	issueComments := buildIssueComments(intervals)

	type weightedIssue struct {
		IssueKey string
		Raw      float64
		Base     int
		Fraction float64
	}

	keys := make([]string, 0, len(weights))
	totalWeight := 0.0
	for key, w := range weights {
		if w <= 0 {
			continue
		}
		keys = append(keys, key)
		totalWeight += w
	}
	sort.Strings(keys)

	if totalWeight <= 0 {
		return allocation
	}

	issues := make([]weightedIssue, 0, len(keys))
	used := 0
	for _, key := range keys {
		raw := float64(remainingMinutes) * (weights[key] / totalWeight)
		base := int(math.Floor(raw))
		used += base
		issues = append(issues, weightedIssue{
			IssueKey: key,
			Raw:      raw,
			Base:     base,
			Fraction: raw - float64(base),
		})
	}

	left := remainingMinutes - used
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Fraction == issues[j].Fraction {
			return issues[i].IssueKey < issues[j].IssueKey
		}
		return issues[i].Fraction > issues[j].Fraction
	})
	for i := 0; i < len(issues) && left > 0; i++ {
		issues[i].Base++
		left--
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].IssueKey < issues[j].IssueKey })

	for _, issue := range issues {
		if issue.Base <= 0 {
			continue
		}
		allocation.Items = append(allocation.Items, WorklogEntry{
			IssueKey: issue.IssueKey,
			Minutes:  issue.Base,
			Source:   SourceActivity,
			Comment:  issueComments[issue.IssueKey],
		})
		allocation.TotalMinutes += issue.Base
	}

	return allocation
}

func buildIssueWeights(intervals []IssueActivityInterval) map[string]float64 {
	// Collect all unique minute boundaries where active issue set changes
	type event struct {
		minute   int64
		issueKey string
		isStart  bool
	}

	var events []event
	for _, iv := range intervals {
		if iv.IssueKey == "" || !iv.End.After(iv.Start) {
			continue
		}
		startMin := iv.Start.Unix() / 60
		endMin := iv.End.Unix() / 60
		events = append(events,
			event{minute: startMin, issueKey: iv.IssueKey, isStart: true},
			event{minute: endMin, issueKey: iv.IssueKey, isStart: false},
		)
	}
	if len(events) == 0 {
		return nil
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].minute != events[j].minute {
			return events[i].minute < events[j].minute
		}
		// Process ends before starts at the same minute
		return !events[i].isStart && events[j].isStart
	})

	weights := make(map[string]float64)
	active := make(map[string]int) // issueKey -> count of overlapping intervals
	prevMinute := events[0].minute

	for _, ev := range events {
		if ev.minute > prevMinute && len(active) > 0 {
			span := float64(ev.minute - prevMinute)
			step := span / float64(len(active))
			for key := range active {
				weights[key] += step
			}
		}
		prevMinute = ev.minute
		if ev.isStart {
			active[ev.issueKey]++
		} else {
			active[ev.issueKey]--
			if active[ev.issueKey] <= 0 {
				delete(active, ev.issueKey)
			}
		}
	}

	return weights
}

func buildIssueComments(intervals []IssueActivityInterval) map[string]string {
	type issueData struct {
		intervals []IssueActivityInterval
	}
	byIssue := make(map[string]*issueData)
	for _, iv := range intervals {
		if iv.IssueKey == "" {
			continue
		}
		if byIssue[iv.IssueKey] == nil {
			byIssue[iv.IssueKey] = &issueData{}
		}
		byIssue[iv.IssueKey].intervals = append(byIssue[iv.IssueKey].intervals, iv)
	}

	comments := make(map[string]string)
	for issueKey, data := range byIssue {
		sort.Slice(data.intervals, func(i, j int) bool {
			return data.intervals[i].Start.Before(data.intervals[j].Start)
		})

		summary := strings.TrimSpace(data.intervals[0].Summary)
		if summary == "" {
			summary = "Auto allocation from Jira activity"
		}

		// Use full status chain from first interval (covers all transitions including through inactive statuses)
		var statusChain []string
		var transferTo string
		for _, iv := range data.intervals {
			if len(iv.StatusChain) > 0 {
				statusChain = iv.StatusChain
			}
			if iv.TransferredTo != "" && transferTo == "" {
				transferTo = iv.TransferredTo
			}
		}
		// Fallback: build chain from Status fields when StatusChain is not set (e.g. in tests or direct construction)
		if len(statusChain) == 0 {
			for _, iv := range data.intervals {
				if len(statusChain) == 0 || statusChain[len(statusChain)-1] != iv.Status {
					statusChain = append(statusChain, iv.Status)
				}
			}
		}

		isCreation := false
		for _, iv := range data.intervals {
			if iv.IsCreation {
				isCreation = true
				break
			}
		}

		comment := `"` + summary + `"`
		if isCreation {
			comment = "Создание " + comment
		} else if len(statusChain) > 1 {
			comment += " (" + strings.Join(statusChain, " → ") + ")"
		} else if len(statusChain) == 1 && strings.EqualFold(strings.TrimSpace(statusChain[0]), "Подтверждение") {
			comment = "Проверка " + comment
		}

		if transferTo != "" {
			comment += " (передана " + transferTo + ")"
		}

		comments[issueKey] = comment
	}
	return comments
}
