# auto-worklog

Автоматическое формирование ежедневного учёта рабочего времени на основе EWS-календаря и Jira активности.

## Возможности

- **plan** -- построение плана рабочей нагрузки
- **apply** -- отправка worklog в Jira через Tempo Timesheets API
- Поддержка диапазонов дат с исключением выходных
- Автопривязка встреч к issue по ключу в теме (ODP-123)
- Коэффициент 1.2x на длительность встреч (кроме all-day и 8-часовых)
- Дедупликация: повторный apply не создаёт дубли (сигнатура по `[HH:MM] comment` + duration)
- Распределение оставшегося времени по Jira issue (weighted по пересечению интервалов)

Примеры:
```sh
# Списать время/показать план на сегодня
./worklog apply/plan

# Списать время/показать план на дату
./worklog apply/plan -d 2026-03-27

# Списать время/показат план на диапазон (выходные исключены)
./worklog apply/plan -d 2026-03-24:2026-03-28

# Кастомные часы в день (по умолчанию 8)
./worklog apply/plan -d 2026-03-27 -h 6
```

## Конфигурация .env

Файл `.env` должен находиться в директории запуска (рядом с бинарником или в корне проекта при `go run`).  

| Переменная | Описание |
|---|---|
| DEFAULT_ISSUE | Default issue для неизвестных встреч (YOU_PROJECT-1000) |
| EWS_URL | URL Exchange Web Services https://mail.example.com/EWS/Exchange.asmx|
| EWS_USERNAME | Почта или логин |
| EWS_PASSWORD | Пароль |
| JIRA_BASE_URL | URL Jira (http://jira.example.com) |
| JIRA_EMAIL | Email для API доступа |
| JIRA_API_TOKEN | Пароль или API token |
| EWS_IGNORED_MEETINGS | занят,обед | Встречи для игнорирования |
| JIRA_IGNORED_STATUSES | Новый | Статусы не для учёта |
| JIRA_DAY_CLOSE_STATUSES | Закрыт,Closed,Отменен,Отменён,"Включен в релиз" | Статусы "день закрыт" |
| JIRA_JQL_TEMPLATE | project = YOU_PROJECT AND (status NOT IN (${JIRA_IGNORED_STATUSES}) AND issuetype NOT IN (Epic) AND ((assignee = currentUser() AND status NOT IN (${JIRA_DAY_CLOSE_STATUSES})) OR	(assignee WAS currentUser() AND updated >= "%s")) OR reporter = currentUser() AND updated >= "%s") | Custom JQL (поддержка ${VAR} и %s) |
| WORK_TYPE | Руководство | Вид работ (атрибут Tempo: Руководство, Разработка, Аналитика, Тестирование) |
| IS_MANAGER | false | Распределять неиспользованное время |
| MANAGER_ACTIVITY_COMMENT | Координация и синхронизация задач | Comment для менеджерского времени |

## Флаги команды

```
-d DATE       Дата (YYYY-MM-DD) или диапазон (YYYY-MM-DD:YYYY-MM-DD)
-h HOURS      Рабочих часов в день (по умолчанию 8)
--with-jira   Включить Jira активность (по умолчанию true)
```

## Сборка

```sh
# macOS arm64
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o bin/worklog ./cmd/worklog

# Linux amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/worklog ./cmd/worklog

# Windows amd64
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/worklog.exe ./cmd/worklog
```

## Требования

- Go 1.24+
- EWS доступ (Exchange)
- Jira API доступ
- Tempo Timesheets (плагин Jira)

## Тестирование

```sh
go test ./...

# С race detector
go test -race ./...

# С покрытием
go test -cover ./...
```

## CI/CD

При пуше тега `v*` GitHub Actions собирает бинарники для darwin-arm64, darwin-amd64, linux-amd64, windows-amd64 и создаёт GitHub Release.

## Архитектура

```
cmd/worklog/              CLI точка входа, оркестрация plan/apply
internal/
  app/                    Рендеринг плана (tabwriter)
  domain/
    meeting_worklog.go    MeetingEvent -> WorklogEntry (буфер 1.2x, [HH:MM] префикс)
    activity_worklog.go   IssueActivityInterval -> WorklogEntry (weighted allocation)
  integrations/
    ews/                  Exchange Web Services (NTLM, SOAP/XML)
    jira/
      client.go           Jira REST API + changelog -> activity intervals
      worklog.go          Tempo Timesheets API (create/dedup worklogs)
```

## Лицензия

[PolyForm Noncommercial 1.0.0](LICENSE) — свободное использование в некоммерческих целях. Коммерческое использование запрещено без отдельного соглашения.
