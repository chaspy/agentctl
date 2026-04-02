# Changelog

## [0.2.20] - 2026-04-02

### Changed

- **Two-stage sync architecture**: All 3 sync paths (`state sync`, `list --sync`, web `/api/sync`) now use a two-stage approach:
  - Stage 1 (Zellij Truth): Mark DB sessions dead if their zellij session no longer exists
  - Stage 2 (JSONL Enrichment): Only update metadata (LastMessage, GitBranch, status) for sessions already in DB
- **No new sessions from JSONL**: JSONL full scan no longer creates new alive sessions — sessions must be registered via `spawn`
- **Unified list --sync**: `list --sync` now delegates to `syncSessionsToDB` instead of duplicating the sync logic
- Added `FindSessionByCWD` store helper for CWD-based session lookup

## [0.2.19] - 2026-04-02

### Fixed

- **Spawn DB registration**: Sessions are now immediately registered in the DB at spawn time with `zellij_session`, eliminating the need for CWD-based inference during sync
- **Ghost session prevention**: `validateAliveWithMux` now returns `false` when mux session list is unavailable (`muxSet==nil`), preventing ghost session creation

## [0.2.18] - 2026-03-31

### Fixed

- **Ghost session prevention**: Sessions without a `zellij_session` are no longer marked `alive=true` during sync (prevents 1000+ ghost records from JSONL scan)
- **Alive validation with mux**: All 3 sync paths (state sync, list --sync, web /api/sync) now cross-check alive status against actual mux session list
- **Orphan cleanup**: `markOrphanedSessionsDead` now also marks sessions without `zellij_session` as dead (ghost sessions)
- **Preview worktree filtering**: CWD-based `worktree-preview-*` filter added as defense in depth alongside existing scanner filter
- Repo name normalization: known incorrect names (e.g. `chaspy/myassistant-server`, `studiuos/jp-Studious-JP`) are corrected during sync and in existing DB records
- PR URL lookup now always executes on first encounter (negative cache only applies after first check)
- Added `ListSessionsByAlive` store query for alive-status filtering

## [0.2.17] - 2026-03-30

### Fixed

- Fix repository name detection: CWD subdirectories (e.g. `/myassistant/server/`) were incorrectly appended to repo name (e.g. `chaspy/myassistant-server` instead of `chaspy/myassistant`)
- `decodeRepository` now takes only owner + repo (2 segments) after `github-com` in directory-encoded names
- `EnrichSession` now uses `git remote get-url origin` to accurately determine repository name from CWD

## [0.2.16] - 2026-03-30

### Fixed

- Reduce GitHub API calls to prevent rate limit exhaustion (5000/hour)
- PR conflict check (`checkPRConflicts`) now runs every 5 minutes instead of every 30 seconds
- Per-PR mergeable state cached for 5 minutes to avoid redundant `gh pr view` calls
- PR URL lookup caches negative results for 5 minutes to avoid repeated `gh pr list` calls for sessions without PRs

## [0.2.15] - 2026-03-30

### Fixed

- `ArchiveDeadSessions` now archives all sessions with `alive=0`, not just `dead`/`error` status
- Previously, blocked sessions (e.g. rate_limit, awaiting_input) with `alive=0` were not archived

## [0.2.14] - 2026-03-30

### Added

- `state sync` now auto-detects PR conflicts and sends rebase instructions to conflicting sessions
- Checks `gh pr view --json mergeable` for alive sessions with PR URLs
- Sends rebase instruction via mux (zellij/tmux) to the session
- 1-hour cooldown to prevent duplicate rebase instructions to the same PR
- Dead sessions are skipped (cannot receive instructions)

## [0.2.13] - 2026-03-30

### Added

- `sessions_archive` table: dead/error sessions are moved to a separate archive table for faster `list` queries
- `archive` command: moves dead/error sessions from `sessions` to `sessions_archive` table
- `list --all` now uses UNION of `sessions` + `sessions_archive` to show all sessions including archived
- `state sync` auto-archives dead/error sessions after marking stale sessions
- `state show` displays archived session count alongside active count
- Migration V9: creates `sessions_archive` table and migrates existing dead/error sessions

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
