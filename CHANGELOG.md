# Changelog

## [0.2.12] - 2026-03-30

### Added

- `spawn --summary` flag to set task_summary at spawn time (skips LLM generation)

### Fixed

- Live-scan path (web server) no longer overwrites LLM-generated `task_summary` with truncated text
- `state sync` no longer passes `task_summary` via upsert; uses `UpdateTaskSummary` after checking DB emptiness
- Normal sync generates `task_summary` only once per session; `--regenerate-summaries` forces re-generation

### Removed

- `GenerateAutoSummary` function (replaced by `GenerateTaskTitle` with claude -p)

## [0.2.11] - 2026-03-30

### Added

- `state sync` now auto-generates `task_summary` for sessions with no existing summary
- Reads the first 3 user messages from the session JSONL and calls `claude -p` to produce a 20-char Japanese title
- Generation is skipped when `task_summary` is already set (preserves existing values)
- 15-second timeout; failures are silently skipped so sync is never blocked
- `state sync --regenerate-summaries` flag to force-regenerate `task_summary` for all sessions
- Fallback to first-message truncation (40 runes) when `claude` command is not found

### Changed

- `GenerateTaskTitle` now uses `exec.LookPath` to check for `claude` availability before calling it
- On `claude -p` failure (timeout, error), falls back to truncation instead of returning empty string

## [0.2.10] - 2026-03-30

### Added

- Auto-fetch PR URL from GitHub during `state sync` and `list --sync` using `gh pr list`
- Display PR URL column in `list` and `state show` output
- PR URLs are cached in DB; only fetched once per session

## [0.2.9] - 2026-03-30

### Fixed

- `state sync` no longer picks up preview worktree directories (`worktree-preview-*`) as Claude sessions

## [0.2.8] - 2026-03-29

### Fixed

- `read` command now resolves short zellij session names (e.g. `myassistant-tts`) by falling back to the DB's `zellij_session` column when no repository match is found

## [0.2.7] - 2026-03-29

### Fixed

- `kill`: send `/exit` to session before killing so Stop hooks (e.g. sui-memory) run before termination

## [0.2.6] - 2026-03-29

### Added

- `spawn --loop` flag: marks a session as a loop session (`is_loop=1` in DB)
- `is_loop` column added to sessions table via DB migration (v8)
- `list` and `state show` display 🔁 after STATUS for loop sessions

## [0.2.5] - 2026-03-29

### Added

- `blocked` sessions now show a reason in the STATUS column: `blocked(awaiting_approval)`, `blocked(awaiting_input)`, or `blocked(rate_limit)`
- Rate limit messages without API metadata are now detected as `blocked` instead of `idle`

## [0.2.4] - 2026-03-29

### Fixed

- Delete zellij session after `kill` to free the session name

## [0.2.3] - 2026-03-29

### Added

- Support mux session name as `send` command target

## [0.2.2] - 2026-03-29

### Fixed

- `spawn`: reuse existing worktree when branch is already checked out (fixes exit 128 error)

## [0.2.1] - 2026-03-25

### Added

- Verify Enter key delivery after `send` command to guarantee input reaches the session

## [0.2.0] - 2026-03-25

### Added

- VERSION file for version tracking
- `--version` flag support via cobra
- `validate-version` CI workflow to enforce version bump in PRs

## [0.1.0] - 2026-03-24

### Added

- Initial release
- Session management (list, read, send, watch, monitor, spawn, kill, resume, preview)
- Rate limit status display
- SQLite state persistence (sync, show, log, tasks)
- Repository config management
- PWA dashboard (serve)
- Homebrew tap support
