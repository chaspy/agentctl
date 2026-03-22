# CLAUDE.md

## Overview

`agentctl` is a Go CLI tool for managing multiple coding agent sessions (Claude Code, Codex CLI) running in zellij or tmux terminal multiplexers.

## Commands

### Session Management

```bash
agentctl list              # List all active sessions
agentctl list --sync       # List + sync to SQLite
agentctl read <name>       # Read latest response from a session
agentctl send <name> <msg> # Send message and wait for response
agentctl watch <name>      # Watch until response changes
agentctl monitor           # Poll all sessions, notify on changes
agentctl spawn <repo>      # Create new zellij session with worktree
agentctl kill <name>       # Terminate session + cleanup worktree
agentctl resume <name>     # Resume a stopped session
agentctl preview <PR>      # Preview a PR in temporary worktree
```

### Rate Limits

```bash
agentctl rate              # Show rate limit status
```

### State Persistence (SQLite)

```bash
agentctl state sync                    # Live scan -> DB sync
agentctl state show                    # Show saved state
agentctl state log "memo"              # Record action log
agentctl state log --since 1h          # View recent actions
agentctl state task list               # List active tasks
agentctl state task add <session> "X"  # Add task
agentctl state task complete <id>      # Complete task
```

### Repository Config

```bash
agentctl config list                   # List all repo configs
agentctl config get <repo>             # Get config for a repo
agentctl config set <repo> --mode branch --desc "description"
agentctl config delete <repo>          # Delete config
```

### Other

```bash
agentctl repos <query>     # Search for repositories on disk
agentctl serve             # Start PWA dashboard (default: port 8080)
```

## Session Status

Sessions are classified into 5 states:

| Status | Detection |
|---|---|
| `blocked` | Last assistant message contains keywords like "approve", "confirm", "please" |
| `error` | API Error, rate limit detected in messages |
| `active` | Last message is from user and process is alive |
| `idle` | Process alive, none of the above |
| `dead` | Process not running |

## Development

### Project Structure

```
cmd/              CLI commands (cobra)
internal/
  mux/            tmux/zellij abstraction
  process/        Process detection (PID, CWD matching)
  provider/       Claude/Codex session scanning, rate limits
  session/        JSONL parser, status detection
  store/          SQLite persistence (sessions, tasks, actions, repo_config)
  web/            PWA dashboard (API + embedded static files)
```

### Dependencies

- `github.com/spf13/cobra` - CLI framework
- `modernc.org/sqlite` - Pure Go SQLite driver

### Build & Test

```bash
go build ./...
go test ./...
```

### DB Location

SQLite database is stored at `.claude/manager.db` (gitignored).
