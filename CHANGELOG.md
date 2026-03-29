# Changelog

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
