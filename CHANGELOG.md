# Changelog

All notable changes to kai are documented here.

## [0.3.0] - 2026-04-07

### Added

- **Team standup management** (`cmd/kai/main.go`): `kai standup submit --member <name>` records daily standup entries (done/today/blocked fields) in Chinese or English format; `kai standup parse --file <path> [--date <date>]` batch-imports from a text file; `kai standup serve [--port N]` starts a local HTTP server on `127.0.0.1` for teammates to POST standups
- **Standup database schema** (`memory/db.go`): new `standups` table with `UNIQUE(member, date)` constraint and `ON CONFLICT REPLACE` upsert semantics; `InsertStandup`, `StandupsForDate`, and `MemberStandupHistory` methods use the write-serializer goroutine
- **Team standup briefing context** (`core/runner.go`): `buildStandupContext` injects today's standup data into the morning briefing system prompt with blocked (🔴), in-progress (🟡), and missing (📭) indicators including multi-day streak counts
- **Cross-day comparison** (`core/runner.go`): `crossDayMatch` verifies that yesterday's planned tasks appear in today's completed work (requires 2+ non-trivial words of length ≥ 4, case-insensitive)
- **Prompt injection defense** (`core/runner.go`): `sanitize` strips `</external_content>` close tags from all standup field values before injecting into prompts
- **Standup reminder cron** (`scheduler/daemon.go`): `standupReminderCron` converts `HH:MM` config to a weekday-only cron expression; `sendStandupReminders` notifies members who have not submitted by reminder time
- **Team configuration** (`config/config.go`): `TeamConfig` struct with `members`, `standup_reminder`, `stale_threshold_days`, `serve_port`, and `standup_history_days` fields; all fields have safe defaults applied in `applyDefaults`
- **Graceful HTTP shutdown** (`cmd/kai/main.go`): `kai standup serve` uses `http.Server` with a context-driven shutdown goroutine; binds to `127.0.0.1` only; handles EADDRINUSE portably via `strings.Contains`

### Tests

- `internal/memory/db_test.go`: `TestInsertStandup_HappyPath`, `TestInsertStandup_Upsert`, `TestStandupsForDate_Empty`, `TestMemberStandupHistory_CrossDay`, `TestMemberStandupHistory_Empty`
- `internal/core/runner_test.go`: `TestCrossDayMatch` (6 subtests), `TestSanitize`, `TestBuildStandupContext_WithData`, `TestBuildStandupContext_EmptyMembers`, `TestBuildStandupContext_SanitizesInjection`
- `cmd/kai/main_test.go`: `TestParseStandupLine` (12 cases), `TestValidateStandupDate`

## [0.2.0] - 2026-04-07

### Features

- **GitHub repo polling** (`scheduler/daemon.go`): background `pollLoop` watches configured repos at 15-minute (active) or 4-hour (idle) intervals; triggers `github_summary` runs automatically when commits are detected
- **Repo activity classification** (`scheduler/daemon.go`): `classifyRepo` queries GitHub commits-since-24h with a 24-hour SQLite cache to minimize API calls
- **Stale pending action expiry** (`safety/gate.go`, `scheduler/daemon.go`): `ExpireStale` auto-aborts pending actions older than 7 days; runs on startup and hourly
- **Replay command** (`cmd/kai/main.go`, `core/runner.go`): `kai replay <run-id>` re-runs an agent against cached tool outputs for read-only post-mortem analysis
- **FTS5 memory search** (`memory/db.go`, `core/runner.go`): `SearchSummaries` searches briefing text via SQLite FTS5; memory context now includes both recent runs and FTS-matched past runs based on watched repo names
- **Token budget enforcement** (`transport/runner.go`): `RunToCompletion` checks daily budget at each iteration and returns an error when the budget is exhausted
- **KAI_RELEVANCE scoring** (`core/runner.go`): agent injects a `KAI_RELEVANCE: N` score into briefings; `extractRelevance` strips the line and surfaces the score in briefing stats
- **Briefing stats footer** (`core/runner.go`): when `briefing_feedback = true`, briefings include a stats block with tokens used, relevance score, and daily budget remaining
- **launchd/systemd install** (`cmd/kai/main.go`): `kai install` generates and loads a `com.kai.daemon` LaunchAgent (macOS) or `kai.service` user unit (Linux) for autostart on login
- **`runStatus` next-fire display**: `kai status` now shows when the next scheduled briefing fires using `robfig/cron` parser
- **Homebrew tap** (`.goreleaser.yaml`, `.github/workflows/release.yml`): GoReleaser now publishes to `c4xx/homebrew-kai` tap on each release

### Tests

- `cmd/kai/main_test.go`: `TestParseCronFromTime`, `TestFormatAge`, `TestTruncate`, `TestBlastRadiusColor`
- `internal/core/runner_test.go`: `TestExtractRelevance`, `TestBuildFTSQuery`, `TestFormatTokens`, `TestDeliverBriefing_NoBriefingFeedback`, `TestDeliverBriefing_WithBriefingFeedback`
- `internal/scheduler/daemon_test.go`: `TestExpireStale`, `TestExpireStale_NoPendingDir`, `TestBuildFTSQuery_ViaClassifyCache`, `TestBuildFTSQuery_IdleCache`, `TestBuildFTSQuery_ExpiredCache`

## [0.1.0] - 2026-04-02

Initial implementation: config loading, SQLite schema with WAL + write-serializer, blast-radius safety gate, AgentRunner transport wrapping Anthropic SDK beta, five tools (bash_exec, file_read, file_write, github_summary, memory_store), agent loop, cron scheduler, and CLI subcommands (init, daemon, run, status, briefing, log, confirm, reject, pending, why).
