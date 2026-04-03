# kai

kai (数字伙伴) — an always-on Go daemon that delivers morning GitHub briefings and remembers context across days.

GitHub: https://github.com/c4xx/kai
Binary: `kai`
Module: `github.com/c4xx/kai`

## Project context

Design doc: `~/.gstack/projects/ass/wangjie-unknown-design-20260402-190548.md`
CEO plan: `~/.gstack/projects/ass/ceo-plans/2026-04-03-devagent.md`
TODOS: `~/.gstack/projects/ass/` (use gstack learnings-search for prior decisions)

Read the CEO plan before making any architecture decisions. All key decisions (SQLite driver, safety gate, confirmation UX, retention metric, etc.) are already resolved there.

## Tech stack

- Go 1.22+
- SQLite: `modernc.org/sqlite` (pure Go, no CGo)
- Scheduler: `robfig/cron/v3`
- GitHub API: `google/go-github/v60`
- Anthropic SDK: `anthropic-sdk-go` (beta, pinned commit)
- Notifications: `gen2brain/beeep`
- Config: TOML via `BurntSushi/toml`

## Architecture

```
cmd/kai/        — main binary, CLI subcommands
internal/
  config/       — TOML loading, DEVAGENT_DATA_DIR env override
  core/         — agent loop (wraps AgentRunner)
  memory/       — SQLite schema, write-serializer goroutine, FTS5
  safety/       — blast-radius classifier, per-action confirmation goroutines
  scheduler/    — robfig/cron v3, GitHub polling loop
  tools/        — Tool interface + 5 tools (bash_exec, file_read, file_write, github_summary, memory_store)
  transport/    — AgentRunner interface wrapping anthropic-sdk-go beta (ONLY file importing beta package)
  notify/       — beeep wrapper
```

## Key decisions (already made — don't relitigate)

- SQLite driver: `modernc.org/sqlite` from day 1 (cross-compilation)
- Confirmation: per-action goroutines poll pending files; write-serializer handles DB only
- No timeout on pending actions; `kai confirm <id>` shows age, requires --force if >2h
- Startup: `reconcilePending()` auto-aborts stale pending if run not in_progress
- Retention gate (Month 2 chat UI): >= 20 active days AND >= 10 briefings read
- Prompt injection: sanitize `</external_content>` before wrapping external content
- Data paths: `~/.local/share/kai/` default, overridable via `KAI_DATA_DIR` env var

## Build & test

```bash
go build ./...
go test ./...
```

## Current build order (Week 1-2)

1. `internal/config` — TOML loading, path resolution
2. `internal/memory` — SQLite schema + write-serializer + WAL mode
3. `internal/safety` — blast-radius classifier + confirmation goroutines
4. `internal/transport` — AgentRunner interface + BetaToolRunner wrapper
5. `internal/tools` — Tool interface + 5 tools
6. `internal/core` — agent loop
7. `cmd/kai` — CLI subcommands: init, install, status, log, run

## gstack skills

Use the `/browse` skill for all web browsing.

Available gstack skills:
- `/office-hours` — product/strategy brainstorming
- `/plan-eng-review` — architecture review
- `/investigate` — debug bugs
- `/ship` — create PR
- `/review` — code review

## Skill routing

When the user's request matches an available skill, ALWAYS invoke it using the Skill
tool as your FIRST action.

Key routing rules:
- Bugs, errors, "why is this broken" → invoke investigate
- Ship, deploy, push, create PR → invoke ship
- Code review, check my diff → invoke review
- Architecture review → invoke plan-eng-review
