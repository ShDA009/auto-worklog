package ews

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"auto-worklog/internal/domain"

	"github.com/Azure/go-ntlmssp"
)

type Client struct {
	URL        string
	Username   string
	Password   string
	HTTPClient *http.Client
}

type findItemEnvelope struct {
	Body struct {
		FindItemResponse struct {
			ResponseMessages struct {
				FindItemResponseMessage struct {
					ResponseClass string `xml:"ResponseClass,attr"`
					ResponseCode  string `xml:"ResponseCode"`
					RootFolder    struct {
						Items struct {
							CalendarItems []calendarItem `xml:"CalendarItem"`
						} `xml:"Items"`
					} `xml:"RootFolder"`
				} `xml:"FindItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"FindItemResponse"`
	} `xml:"Body"`
}

type calendarItem struct {
	Subject              string `xml:"Subject"`
	Start                string `xml:"Start"`
	End                  string `xml:"End"`
	IsAllDayEvent        bool   `xml:"IsAllDayEvent"`
	LegacyFreeBusyStatus string `xml:"LegacyFreeBusyStatus"`
}

func (c Client) FetchMeetings(ctx context.Context, date time.Time, timezone string) ([]domain.MeetingEvent, error) {
	if c.Username == "" || c.Password == "" {
		return nil, errors.New("EWS credentials are not set")
	}

	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", timezone, err)
	}

	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
	end := start.Add(24 * time.Hour)
	payload := buildFindItemPayload(start, end)

	url := c.URL
	if url == "" {
		return nil, errors.New("EWS URL is not set")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build ews request: %w", err)
	}
	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", "http://schemas.microsoft.com/exchange/services/2006/messages/FindItem")

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{
			Transport: ntlmssp.Negotiator{RoundTripper: http.DefaultTransport},
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ews request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("ews request status %s: %s", resp.Status, string(body))
	}

	var envelope findItemEnvelope
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ews response: %w", err)
	}
	if err := xml.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parse ews xml: %w", err)
	}

	msg := envelope.Body.FindItemResponse.ResponseMessages.FindItemResponseMessage
	if msg.ResponseClass != "Success" || msg.ResponseCode != "NoError" {
		return nil, fmt.Errorf("ews response not successful: class=%s code=%s", msg.ResponseClass, msg.ResponseCode)
	}

	meetings := make([]domain.MeetingEvent, 0, len(msg.RootFolder.Items.CalendarItems))
	for _, item := range msg.RootFolder.Items.CalendarItems {
		// Only count Busy and Tentative; skip Free, OOF marked as free, etc.
		if item.LegacyFreeBusyStatus != "Busy" && item.LegacyFreeBusyStatus != "Tentative" && item.LegacyFreeBusyStatus != "OOF" {
			continue
		}
		startAt, err := time.Parse(time.RFC3339, item.Start)
		if err != nil {
			continue
		}
		endAt, err := time.Parse(time.RFC3339, item.End)
		if err != nil {
			continue
		}

		if item.IsAllDayEvent {
			// For all-day events check if the event range intersects the requested day.
			// EWS returns End as midnight of the day after the last day (exclusive).
			startAtLocal := startAt.In(location)
			endAtLocal := endAt.In(location)

			if !endAtLocal.After(start) || !startAtLocal.Before(end) {
				continue
			}

			meetings = append(meetings, domain.MeetingEvent{
				Title:           item.Subject,
				DurationMinutes: 480, // 8 hours per day for all-day events
				IsAllDayEvent:   true,
			})
			continue
		}

		// For regular (non-all-day) events, cap duration to requested day
		startAtLocal := startAt.In(location)
		endAtLocal := endAt.In(location)

		// Meeting must intersect with requested day: [start, end)
		if endAtLocal.Before(start) || startAtLocal.After(end) {
			continue
		}

		// Cap event to day boundaries
		eventStart := startAtLocal
		if eventStart.Before(start) {
			eventStart = start
		}
		eventEnd := endAtLocal
		if eventEnd.After(end) {
			eventEnd = end
		}
		durationSeconds := eventEnd.Sub(eventStart).Seconds()
		if durationSeconds <= 0 {
			continue
		}
		duration := min(int(durationSeconds/60), 480) // Cap at 8h for multi-day events

		meetings = append(meetings, domain.MeetingEvent{
			Title:           item.Subject,
			DurationMinutes: duration,
			StartTime:       eventStart,
		})
	}

	return meetings, nil
}

func buildFindItemPayload(start, end time.Time) string {
	var out bytes.Buffer
	out.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	out.WriteString(`<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"`)
	out.WriteString(` xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"`)
	out.WriteString(` xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">`)
	out.WriteString(`<soap:Header><t:RequestServerVersion Version="Exchange2016" /></soap:Header>`)
	out.WriteString(`<soap:Body><m:FindItem Traversal="Shallow">`)
	out.WriteString(`<m:ItemShape><t:BaseShape>Default</t:BaseShape>` +
		`<t:AdditionalProperties><t:FieldURI FieldURI="calendar:LegacyFreeBusyStatus"/></t:AdditionalProperties>` +
		`</m:ItemShape>`)
	out.WriteString(`<m:CalendarView StartDate="`)
	out.WriteString(start.Format(time.RFC3339))
	out.WriteString(`" EndDate="`)
	out.WriteString(end.Add(-1 * time.Nanosecond).Format(time.RFC3339))
	out.WriteString(`"/>`)
	out.WriteString(`<m:ParentFolderIds><t:DistinguishedFolderId Id="calendar"/></m:ParentFolderIds>`)
	out.WriteString(`</m:FindItem></soap:Body></soap:Envelope>`)
	return out.String()
}
