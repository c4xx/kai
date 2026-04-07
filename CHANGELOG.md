# Changelog

All notable changes to kai are documented here.

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
