# auto-worklog

Утилита для автоматического формирования и применения ежедневного учёта рабочего времени (worklog) на основе данных из EWS-календаря и активности в Jira.

## Возможности

- `plan`: построение плана рабочей нагрузки по встречам и активности
- `apply`: подтвреждение и отправка worklog в Jira
- буферизация времени встреч (`+20%`)
- привязка встреч к issue при наличии ключа вида `ODP-123` в теме
- fallback issue из `DEFAULT_ISSUE` для неизвестных встреч
- распределение оставшегося времени по активным Jira issue пропорционально активности
- игнорирование встреч с темами `занят` и `обед`

## Требования

- Go 1.20+
- Доступ к EWS (Exchange Web Services)
- Jira Cloud / Jira Server REST API

## Установка

```sh
go install ./cmd/worklog
```

## Сборка для всех ОС

Включаю `CGO_ENABLED=0` для статической сборки и стабильности при кросс-компиляции.

Для Linux:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/worklog-linux-amd64 ./cmd/worklog
```

Для macOS:

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o bin/worklog-darwin-amd64 ./cmd/worklog
```

Для Windows:

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/worklog-windows-amd64.exe ./cmd/worklog
```

Дополнительно можно собирать под arm64:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/worklog-linux-arm64 ./cmd/worklog
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o bin/worklog-darwin-arm64 ./cmd/worklog
```

## Конфигурация

Настройки передаются через переменные окружения или `.env` файл.

Обязательные переменные:

- `DEFAULT_ISSUE` (например `ODP-1000`)
- `EWS_USERNAME`
- `EWS_PASSWORD`

Jira:

- `JIRA_BASE_URL` (https://your-domain.atlassian.net)
- `JIRA_EMAIL`
- `JIRA_API_TOKEN`

Дополнительно (по умолчанию пустые):

- `EWS_URL` (если не задан - ошибка`)
- `EWS_IGNORED_MEETINGS` — список тем встреч для игнорирования (по умолчанию: `занят,обед`)
- `JIRA_IGNORED_STATUSES` — статусы, которые не учитывать
- `JIRA_DAY_CLOSE_STATUSES` — статусы, после которых день считается «закрытым»
- `JIRA_JQL_TEMPLATE` — JQL-запрос для поиска задач. Поддерживает подстановку переменных: `${JIRA_IGNORED_STATUSES}`, `${JIRA_DAY_CLOSE_STATUSES}`. Плейсхолдер `%s` для даты
## Использование

### План на дату

```sh
worklog plan --date 2026-03-27
```

Вывод:

- таблица issue / минуты / источник / комментарий
- итоговое количество минут

### Применение (отправка в Jira)

```shn
worklog apply --date 2026-03-27
```

После запроса `Apply these worklogs? type 'yes' to continue:` введите `yes`.

## Тестирование

```sh
go test ./...
```

## Структура проекта

- `cmd/worklog` — точка входа CLI
- `internal/app` — парсинг/рендеринг плана
- `internal/domain` — бизнес-логика (встречи, активность)
- `internal/integrations/ews` — загрузка встреч из Exchange
- `internal/integrations/jira` — загрузка активности + отправка worklog

## Соображения

- Временная зона по умолчанию: `Europe/Moscow`.
- Встречи из EWS считаются 24 часа с 00:00 до 23:59, с учётом буфера +20%.
- Если `--with-jira` выключен, учёт только по встречам.
