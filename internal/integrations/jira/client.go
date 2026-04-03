package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"auto-worklog/internal/domain"
)

type Client struct {
	BaseURL    string
	Email      string
	APIToken   string
	HTTPClient *http.Client
}

type StatusRules struct {
	IgnoredStatuses  []string
	DayCloseStatuses []string
}

func DefaultStatusRules() StatusRules {
	return StatusRules{}
}

type userInfo struct {
	AccountID    string `json:"accountId"`
	Key          string `json:"key"`
	Name         string `json:"name"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
}

type searchResponse struct {
	Total      int         `json:"total"`
	StartAt    int         `json:"startAt"`
	MaxResults int         `json:"maxResults"`
	Issues     []jiraIssue `json:"issues"`
}

type jiraIssue struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Created string `json:"created"`
		Status  struct {
			Name string `json:"name"`
		} `json:"status"`
		Assignee *struct {
			AccountID    string `json:"accountId"`
			Key          string `json:"key"`
			Name         string `json:"name"`
			EmailAddress string `json:"emailAddress"`
			DisplayName  string `json:"displayName"`
		} `json:"assignee"`
		Reporter *struct {
			AccountID    string `json:"accountId"`
			Key          string `json:"key"`
			Name         string `json:"name"`
			EmailAddress string `json:"emailAddress"`
			DisplayName  string `json:"displayName"`
		} `json:"reporter"`
	} `json:"fields"`
	Changelog struct {
		Histories []history `json:"histories"`
	} `json:"changelog"`
}

type history struct {
	Created string `json:"created"`
	Items   []struct {
		Field      string `json:"field"`
		From       string `json:"from"`
		FromString string `json:"fromString"`
		To         string `json:"to"`
		ToString   string `json:"toString"`
	} `json:"items"`
}

func (c Client) FetchActivityIntervals(
	ctx context.Context,
	date time.Time,
	timezone string,
	rules StatusRules,
) ([]domain.IssueActivityInterval, error) {
	if c.BaseURL == "" || c.Email == "" || c.APIToken == "" {
		return nil, errors.New("JIRA_BASE_URL/JIRA_EMAIL/JIRA_API_TOKEN are not set")
	}
	if strings.TrimSpace(os.Getenv("JIRA_PROJECT")) == "" {
		return nil, errors.New("JIRA_PROJECT is not set")
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", timezone, err)
	}
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)

	me, err := c.fetchMyself(ctx)
	if err != nil {
		return nil, err
	}
	issues, err := c.fetchIssuesWithChangelog(ctx, dayStart, dayStart)
	if err != nil {
		return nil, err
	}

	intervals := make([]domain.IssueActivityInterval, 0)
	for _, issue := range issues {
		intervals = append(intervals, buildIssueIntervals(issue, me, dayStart, dayEnd, rules)...)
	}
	return intervals, nil
}

func (c Client) FetchActivityIntervalsForRange(
	ctx context.Context,
	fromDate time.Time,
	toDate time.Time,
	timezone string,
	rules StatusRules,
) ([]domain.IssueActivityInterval, error) {
	if c.BaseURL == "" || c.Email == "" || c.APIToken == "" {
		return nil, errors.New("JIRA_BASE_URL/JIRA_EMAIL/JIRA_API_TOKEN are not set")
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", timezone, err)
	}

	me, err := c.fetchMyself(ctx)
	if err != nil {
		return nil, err
	}

	// Fetch issues for entire date range in one query
	issues, err := c.fetchIssuesWithChangelog(ctx, fromDate, toDate)
	if err != nil {
		return nil, err
	}

	intervals := make([]domain.IssueActivityInterval, 0)

	rangeStart := time.Date(fromDate.Year(), fromDate.Month(), fromDate.Day(), 0, 0, 0, 0, loc)
	rangeEnd := toDate.Add(24 * time.Hour)

	for d := rangeStart; d.Before(rangeEnd); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		dayStart := d
		dayEnd := d.Add(24 * time.Hour)

		for _, issue := range issues {
			intervals = append(intervals, buildIssueIntervals(issue, me, dayStart, dayEnd, rules)...)
		}
	}
	
	return intervals, nil
}

func (c Client) fetchMyself(ctx context.Context) (userInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/rest/api/2/myself", nil)
	if err != nil {
		return userInfo{}, fmt.Errorf("build jira myself request: %w", err)
	}
	req.SetBasicAuth(c.Email, c.APIToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return userInfo{}, fmt.Errorf("jira myself request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return userInfo{}, fmt.Errorf("jira myself status: %s", resp.Status)
	}

	var me userInfo
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return userInfo{}, fmt.Errorf("decode jira myself response: %w", err)
	}
	return me, nil
}

func (c Client) fetchIssuesWithChangelog(ctx context.Context, fromDate, toDate time.Time) ([]jiraIssue, error) {
	base := strings.TrimRight(c.BaseURL, "/") + "/rest/api/2/search"

	// Build date string for JQL - use from date as lower bound only
	// (upper bound excluded to capture tasks updated after range end)
	fromTimeStr := fromDate.Format("2006-01-02") + " 00:00"
	jql := buildJQL(fromTimeStr)

	startAt := 0
	const pageSize = 100
	all := make([]jiraIssue, 0, pageSize)

	for {
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("startAt", fmt.Sprintf("%d", startAt))
		q.Set("maxResults", fmt.Sprintf("%d", pageSize))
		q.Set("fields", "summary,status,assignee,reporter,created")
		q.Set("expand", "changelog")

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("build jira search request: %w", err)
		}
		req.SetBasicAuth(c.Email, c.APIToken)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient().Do(req)
		if err != nil {
			return nil, fmt.Errorf("jira search request failed: %w", err)
		}

		var result searchResponse
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			return nil, fmt.Errorf("jira search status: %s\njql: %s\nbody: %s", resp.Status, jql, string(body))
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode jira search response: %w", err)
		}
		resp.Body.Close()

		all = append(all, result.Issues...)
		startAt += len(result.Issues)
		if len(result.Issues) == 0 || startAt >= result.Total {
			break
		}
	}

	return all, nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// buildIssueIntervals строит список интервалов активности пользователя (me) по задаче за указанный день [dayStart, dayEnd].
// Интервал = отрезок времени, в течение которого задача была назначена на me и не находилась в игнорируемом статусе.
// Дополнительно: если me является автором задачи и создал её в этот день — добавляется интервал создания.
func buildIssueIntervals(
	issue jiraIssue,
	me userInfo,
	dayStart time.Time,
	dayEnd time.Time,
	rules StatusRules,
) []domain.IssueActivityInterval {
	// --- Шаг 1: Определяем время создания задачи ---
	// Если задача создана позже dayEnd — она ещё не существовала в этот день, пропускаем.
	var createdAt time.Time
	if issue.Fields.Created != "" {
		if t, err := parseJiraTime(issue.Fields.Created); err == nil {
			if !t.Before(dayEnd) {
				return nil // задача создана после конца дня — не учитываем
			}
			createdAt = t
		}
	}

	// --- Шаг 2: Парсим историю изменений (changelog) ---
	// events — список всех изменений статуса/исполнителя, отсортированных по времени.
	events := parseChangelog(issue.Changelog.Histories)

	// --- Шаг 3: Восстанавливаем состояние задачи на начало дня ---
	// Берём текущее состояние (статус + исполнитель) и «откручиваем» изменения назад до dayStart.
	// Результат: state — какой статус и исполнитель были у задачи в момент dayStart.
	state := rewindStateToDay(issueState{issue.Fields.Status.Name, assigneeValue(issue)}, events, dayStart)

	// --- Шаг 4: Собираем цепочку статусов за день ---
	// statusChain — все статусы, через которые прошла задача в течение дня (для отображения).
	statusChain := collectStatusChain(state.status, events, dayStart, dayEnd)

	// --- Шаг 4.5: Пропускаем задачи, уже закрытые до начала дня ---
	// Если на момент dayStart задача находится в «закрытом» статусе (dayCloseStatus)
	// И была создана до этого дня — значит она закрыта вчера или раньше.
	// Такая задача не должна попадать в отчёт за сегодня.
	// Исключение: если задача создана сегодня — пропускать нельзя
	// (state после rewind будет текущим, т.е. уже закрытым, хотя закрытие произошло сегодня).
	if isDayCloseStatus(state.status, rules) && !createdAt.IsZero() && createdAt.Before(dayStart) {
		return nil
	}

	// --- Шаг 5: Инициализируем курсор начала текущего интервала ---
	// cursor — левая граница потенциального интервала активности.
	// Если задача создана внутри дня — сдвигаем курсор к моменту создания
	// (до создания задачи активности быть не могло).
	cursor := dayStart
	if !createdAt.IsZero() && createdAt.After(cursor) {
		cursor = createdAt
	}

	// --- Шаг 6: Проверяем, активна ли задача в начале дня ---
	// active = true, если в момент dayStart задача назначена на me и статус не игнорируемый.
	active := isSelfAssignee(state.assignee, me) && !isIgnoredStatus(state.status, rules)
	intervals := make([]domain.IssueActivityInterval, 0)

	// --- Шаг 7: Интервал создания (особый случай) ---
	// Если me является репортером И задача создана именно в этот день —
	// добавляем специальный интервал создания. Он не зависит от статуса и исполнителя:
	// сам факт создания задачи — это активность.
	if !createdAt.IsZero() && !createdAt.Before(dayStart) && isSelfReporter(issue, me) {
		iv := newInterval(issue, state.status, "", "", createdAt, dayEnd)
		iv.StatusChain = statusChain
		iv.IsCreation = true
		intervals = append(intervals, iv)
	}

	// --- Шаг 8: Обходим события изменений за день ---
	// Каждое событие — момент, когда изменился статус или исполнитель.
	// Логика: если до наступления события задача была «активна» (назначена на me, не игнор-статус),
	// то отрезок [cursor, e.At] фиксируем как интервал активности.
	for _, e := range events {
		if e.At.Before(dayStart) {
			continue // событие до начала дня — пропускаем
		}
		if e.At.After(dayEnd) {
			break // события строго отсортированы; всё после dayEnd — уже не наш день
		}

		// Если задача была активна с cursor до e.At — фиксируем интервал.
		if active && e.At.After(cursor) {
			transferredTo := ""
			// Если следующее событие — переназначение на другого — сохраняем, кому передали.
			if e.Change.assigneeTo != "" && !isSelfAssignee(e.Change.assigneeTo, me) {
				transferredTo = e.Change.assigneeTo
			}
			intervals = append(intervals, newInterval(issue, state.status, e.Change.statusTo, transferredTo, cursor, e.At))
		}

		// Применяем изменения из события к текущему state.
		if e.Change.statusTo != "" {
			state.status = e.Change.statusTo
		}
		if e.Change.assigneeTo != "" {
			state.assignee = e.Change.assigneeTo
		}

		// Сдвигаем курсор и пересчитываем active для следующего отрезка.
		cursor = e.At
		active = isSelfAssignee(state.assignee, me) && !isIgnoredStatus(state.status, rules)
	}

	// --- Шаг 9: Закрываем последний интервал до конца дня ---
	// Если после последнего события (или с начала, если событий не было) задача остаётся активной —
	// фиксируем отрезок [cursor, dayEnd].
	// Исключение: cursor == dayStart && статус «закрывающий» — тогда задача уже закрыта весь день, не учитываем.
	if active && dayEnd.After(cursor) && (cursor.After(dayStart) || !isDayCloseStatus(state.status, rules)) {
		intervals = append(intervals, newInterval(issue, state.status, "", "", cursor, dayEnd))
	}

	// --- Шаг 10: Прикрепляем цепочку статусов к первому интервалу ---
	// StatusChain нужен только один раз — на первом интервале задачи.
	if len(intervals) > 0 {
		intervals[0].StatusChain = statusChain
	}
	return intervals
}

func parseChangelog(histories []history) []timedChange {
	events := make([]timedChange, 0, len(histories))
	for _, h := range histories {
		tm, err := parseJiraTime(h.Created)
		if err != nil {
			continue
		}
		changes := changeSet{}
		for _, item := range h.Items {
			switch strings.ToLower(item.Field) {
			case "status":
				changes.statusFrom = item.FromString
				changes.statusTo = item.ToString
			case "assignee":
				changes.assigneeFrom = firstNonEmpty(item.FromString, item.From)
				changes.assigneeTo = firstNonEmpty(item.ToString, item.To)
			}
		}
		events = append(events, timedChange{At: tm, Change: changes})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	return events
}

// rewindStateToDay walks the changelog backwards to reconstruct the issue state at the start of dayStart.
func rewindStateToDay(state issueState, events []timedChange, dayStart time.Time) issueState {
	for i := len(events) - 1; i >= 0; i-- {
		if !events[i].At.After(dayStart) {
			break
		}
		if events[i].Change.statusFrom != "" {
			state.status = events[i].Change.statusFrom
		}
		if events[i].Change.assigneeFrom != "" {
			state.assignee = events[i].Change.assigneeFrom
		}
	}
	return state
}


func collectStatusChain(startStatus string, events []timedChange, dayStart, dayEnd time.Time) []string {
	chain := []string{startStatus}
	for _, e := range events {
		if e.At.Before(dayStart) || e.At.After(dayEnd) {
			continue
		}
		if e.Change.statusTo != "" && chain[len(chain)-1] != e.Change.statusTo {
			chain = append(chain, e.Change.statusTo)
		}
	}
	return chain
}

func newInterval(issue jiraIssue, status, statusTo, transferredTo string, start, end time.Time) domain.IssueActivityInterval {
	return domain.IssueActivityInterval{
		IssueKey:      issue.Key,
		Summary:       issue.Fields.Summary,
		Status:        status,
		StatusTo:      statusTo,
		TransferredTo: transferredTo,
		Start:         start,
		End:           end,
	}
}

type timedChange struct {
	At     time.Time
	Change changeSet
}

type changeSet struct {
	statusFrom   string
	statusTo     string
	assigneeFrom string
	assigneeTo   string
}

type issueState struct {
	status   string
	assignee string
}

func parseJiraTime(v string) (time.Time, error) {
	layouts := []string{
		"2006-01-02T15:04:05.000-0700",
		time.RFC3339,
	}
	for _, l := range layouts {
		t, err := time.Parse(l, v)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported jira time format: %s", v)
}

func isIgnoredStatus(status string, rules StatusRules) bool {
	return containsNormalized(rules.IgnoredStatuses, status)
}

func isDayCloseStatus(status string, rules StatusRules) bool {
	return containsNormalized(rules.DayCloseStatuses, status)
}

func normalizeStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func containsNormalized(items []string, value string) bool {
	needle := normalizeStatus(value)
	for _, item := range items {
		if normalizeStatus(item) == needle {
			return true
		}
	}
	return false
}

const jqlBase = `project = %s AND issuetype NOT IN (Epic) AND (status NOT IN (%s) AND ((assignee = currentUser() AND status NOT IN (%s)) OR (assignee WAS currentUser() AND updated >= "%s")) OR (reporter = currentUser() AND updated >= "%s"))`

func buildJQL(fromTimeStr string) string {
	project := strings.TrimSpace(os.Getenv("JIRA_PROJECT"))
	ignored := strings.TrimSpace(os.Getenv("JIRA_IGNORED_STATUSES"))
	dayClose := strings.TrimSpace(os.Getenv("JIRA_DAY_CLOSE_STATUSES"))
	
	return fmt.Sprintf(jqlBase, project, ignored, dayClose, fromTimeStr, fromTimeStr)
}

func isSelfAssignee(value string, me userInfo) bool {
	candidates := []string{
		me.AccountID,
		me.Key,
		me.Name,
		me.EmailAddress,
		me.DisplayName,
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(c)) {
			return true
		}
	}
	return false
}

func isSelfReporter(issue jiraIssue, me userInfo) bool {
	if issue.Fields.Reporter == nil {
		return false
	}
	r := issue.Fields.Reporter
	return isSelfAssignee(firstNonEmpty(r.AccountID, r.Key, r.Name, r.EmailAddress, r.DisplayName), me)
}

func assigneeValue(issue jiraIssue) string {
	if issue.Fields.Assignee == nil {
		return ""
	}
	return firstNonEmpty(
		issue.Fields.Assignee.AccountID,
		issue.Fields.Assignee.Key,
		issue.Fields.Assignee.Name,
		issue.Fields.Assignee.EmailAddress,
		issue.Fields.Assignee.DisplayName,
	)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
