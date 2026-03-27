## План: Автосписание времени в Jira Tempo на основе Exchange EWS + Jira Activity Stream

### Summary
Сделать ручной CLI-инструмент на Go, который за выбранный день:
1. Читает встречи из корпоративного Outlook через Exchange EWS (`https://company.example.com/EWS/Exchange.asmx`).
2. Списывает встречи в Tempo с буфером `+20%`:
- если в заголовке есть ключ вида `ODP-1234` -> в эту задачу;
- иначе -> в `ODP-1234`.
3. Распределяет остаток рабочего дня (до 8ч) по задачам в статусах `In Progress` и `Подтверждение` через Jira Activity Stream.
4. По умолчанию работает в `dry-run`, запись только после явного подтверждения.

### Key Changes / Interfaces
- CLI команды:
1. `worklog plan --source ews --date YYYY-MM-DD` — получить встречи из EWS и показать расчет.
2. `worklog apply --date YYYY-MM-DD` — выполнить запись в Tempo.
- Конфиг:
1. `EWS_URL`, `EWS_USERNAME`, `EWS_PASSWORD`.
2. `JIRA_BASE_URL`, `JIRA_EMAIL`, `JIRA_API_TOKEN`.
3. `TEMPO_API_TOKEN`.
4. `DEFAULT_ISSUE=ODP-1234`, `WORKDAY_HOURS=8`, `WORK_STATUSES=In Progress,Подтверждение`.

### Implementation Details
1. Получение встреч через EWS SOAP `FindItem` + NTLM auth.
2. Парсинг `CalendarItem` (`Subject`, `Start`, `End`) и конвертация UTC -> Europe/Moscow.
3. Расчет meeting worklogs (`+20%`, fallback issue).
4. Интеграция Jira Activity Stream и распределение остатка.
5. Идемпотентная запись в Tempo.

### Test Plan
1. Unit: парсинг `ODP-XXXX`, буфер `+20%`, агрегация минут.
2. Integration: мок EWS SOAP response -> корректные `MeetingEvent`.
3. CLI: валидация флагов и env для `--source ews`.
4. E2E: `plan`/`apply` на реальном дне.

### Assumptions
1. Рабочий день фиксирован: `8 часов`.
2. Рабочие статусы: `In Progress`, `Подтверждение`.
3. Запуск вручную на ПК пользователя с доступом к VPN.
