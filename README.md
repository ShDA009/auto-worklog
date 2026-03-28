# auto-worklog

Автоматическое формирование ежедневного учёта рабочего времени на основе EWS-календаря и Jira активности.

## Возможности

- **plan** — построение плана рабочей нагрузки
- **apply** — отправка worklog в Jira через Tempo Timesheets API
- Поддержка диапазонов дат с исключением выходных
- Автопривязка встреч к issue по ключу в теме (ODP-123)
- Распределение оставшегося времени по Jira issue
- Оптимизация: весь диапазон в **одном API запросе**

## Быстрый старт

Обязательные переменные в `.env`:
```
DEFAULT_ISSUE=ODP-1000
EWS_URL=https://...
EWS_USERNAME=user@mail.com
EWS_PASSWORD=***
JIRA_BASE_URL=https://your-domain.atlassian.net
JIRA_EMAIL=user@mail.com
JIRA_API_TOKEN=***
```

Примеры:
```sh
# План на сегодня
./worklog plan

# План на дату
./worklog plan -d 2026-03-27

# План на диапазон (выходные исключены автоматически)
./worklog plan -d 2026-03-24:2026-03-28

# Применить worklogs в Jira
./worklog apply -d 2026-03-27

# Кастомные часы в день (по умолчанию 8)
./worklog plan -d 2026-03-27 -h 6
```

## Конфигурация

**Обязательные:**
| Переменная | Описание |
|---|---|
| DEFAULT_ISSUE | Default issue для неизвестных встреч (ODP-1000) |
| EWS_URL | URL Exchange Web Services |
| EWS_USERNAME | Почта или логин |
| EWS_PASSWORD | Пароль |
| JIRA_BASE_URL | URL Jira (http://jira.example.com) |
| JIRA_EMAIL | Email для API доступа |
| JIRA_API_TOKEN | Пароль или API token |

**Опциональные:**
| Переменная | Пример | Описание |
|---|---|---|
| EWS_IGNORED_MEETINGS | занят,обед | Встречи для игнорирования |
| JIRA_IGNORED_STATUSES | Новый | Статусы не для учёта |
| JIRA_DAY_CLOSE_STATUSES | Закрыт,Closed,Отменен,Отменён,"Включен в релиз" | Статусы "день закрыт" |
| JIRA_JQL_TEMPLATE | project = ODP AND status NOT IN ({JIRA_IGNORED_STATUSES}) AND issuetype NOT IN (Epic) AND ((assignee = currentUser() AND status NOT IN (\${JIRA_DAY_CLOSE_STATUSES})) OR (assignee WAS currentUser() AND updated >= "%s"))| Custom JQL (поддержка ${VAR} и %s) |
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

- Go 1.20+
- EWS доступ (Exchange)
- Jira API доступ
- Tempo Timesheets (плагин Jira)

## Тестирование

```sh
go test ./...
```

## Архитектура

- `cmd/worklog` — CLI точка входа
- `internal/app` — парсинг и рендеринг
- `internal/domain` — бизнес-логика
- `internal/integrations/ews` — Exchange интеграция
- `internal/integrations/jira` — Jira интеграция
