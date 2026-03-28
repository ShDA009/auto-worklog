package ews

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchMeetings(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("SOAPAction"); got == "" {
			t.Fatal("SOAPAction header is empty")
		}
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
  <s:Body>
    <m:FindItemResponse xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
      <m:ResponseMessages>
        <m:FindItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder>
            <t:Items>
              <t:CalendarItem>
                <t:Subject>Daily ODP-1001</t:Subject>
                <t:Start>2026-03-27T10:00:00Z</t:Start>
                <t:End>2026-03-27T10:30:00Z</t:End>
              </t:CalendarItem>
            </t:Items>
          </m:RootFolder>
        </m:FindItemResponseMessage>
      </m:ResponseMessages>
    </m:FindItemResponse>
  </s:Body>
</s:Envelope>`))
	}))
	defer server.Close()

	client := Client{
		URL:        server.URL,
		Username:   "user@example.com",
		Password:   "password",
		HTTPClient: server.Client(),
	}

	date := time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC)
	meetings, err := client.FetchMeetings(context.Background(), date, "Europe/Moscow")
	if err != nil {
		t.Fatalf("FetchMeetings returned error: %v", err)
	}

	if len(meetings) != 1 {
		t.Fatalf("len(meetings) = %d, want 1", len(meetings))
	}
	if meetings[0].Title != "Daily ODP-1001" {
		t.Fatalf("title = %q, want Daily ODP-1001", meetings[0].Title)
	}
	if meetings[0].DurationMinutes != 30 {
		t.Fatalf("duration = %d, want 30", meetings[0].DurationMinutes)
	}
}

func TestFetchMeetingsExcludesAllDayEventFromPreviousDay(t *testing.T) {
	t.Parallel()

	// Test case: all-day event on Monday (2026-03-23) ends Tuesday (2026-03-24 00:00)
	// When fetching Tuesday meetings, it should NOT be included
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
  <s:Body>
    <m:FindItemResponse xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages" xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
      <m:ResponseMessages>
        <m:FindItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder>
            <t:Items>
              <t:CalendarItem>
                <t:Subject>All-day Monday event</t:Subject>
                <t:Start>2026-03-23T00:00:00Z</t:Start>
                <t:End>2026-03-24T00:00:00Z</t:End>
              </t:CalendarItem>
              <t:CalendarItem>
                <t:Subject>Tuesday morning meeting ODP-123</t:Subject>
                <t:Start>2026-03-24T09:00:00Z</t:Start>
                <t:End>2026-03-24T10:00:00Z</t:End>
              </t:CalendarItem>
            </t:Items>
          </m:RootFolder>
        </m:FindItemResponseMessage>
      </m:ResponseMessages>
    </m:FindItemResponse>
  </s:Body>
</s:Envelope>`))
	}))
	defer server.Close()

	client := Client{
		URL:        server.URL,
		Username:   "user@example.com",
		Password:   "password",
		HTTPClient: server.Client(),
	}

	// Fetch Tuesday meetings
	date := time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC)
	meetings, err := client.FetchMeetings(context.Background(), date, "Europe/Moscow")
	if err != nil {
		t.Fatalf("FetchMeetings returned error: %v", err)
	}

	// Should only have the Tuesday morning meeting, NOT the all-day Monday event
	if len(meetings) != 1 {
		t.Fatalf("len(meetings) = %d, want 1 (should exclude all-day Monday event)", len(meetings))
	}
	if meetings[0].Title != "Tuesday morning meeting ODP-123" {
		t.Fatalf("title = %q, want Tuesday morning meeting ODP-123", meetings[0].Title)
	}
}

func TestBuildFindItemPayload(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 3, 27, 0, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	end := start.Add(24 * time.Hour)

	payload := buildFindItemPayload(start, end)

	// Check essential parts of the payload
	checks := []string{
		"<m:FindItem",
		`StartDate="2026-03-27T00:00:00+03:00"`,
		// EndDate is adjusted by -1 nanosecond to exclude all-day events ending at midnight
		// This results in 2026-03-27T23:59:59.999999999+03:00
		"2026-03-27T23:59:59",
		"EndDate=",
		`<t:DistinguishedFolderId Id="calendar"/>`,
	}

	for _, expected := range checks {
		if !strings.Contains(payload, expected) {
			t.Fatalf("payload does not contain %q\nPayload: %s", expected, payload)
		}
	}
}
