# agentctl

A CLI tool for managing multiple coding agent sessions (Claude Code, Codex CLI) running in [zellij](https://zellij.dev/) or [tmux](https://github.com/tmux/tmux).

## Features

- **Session listing** - Scan `~/.claude/projects/` and `~/.codex/sessions/` to display all active sessions with status detection
- **Session communication** - Send messages to sessions via terminal multiplexer and wait for responses
- **Session lifecycle** - Spawn new sessions with git worktree isolation, kill finished ones with safety checks
- **Monitoring** - Watch for session changes, auto-notify on new assistant responses
- **Rate limit tracking** - Check Claude Code and Codex CLI rate limit status
- **State persistence** - SQLite-backed state with session sync, task tracking, and action logging
- **PWA dashboard** - Web-based dashboard for mobile monitoring

## Requirements

- Go 1.24+
- [zellij](https://zellij.dev/) or [tmux](https://github.com/tmux/tmux) (terminal multiplexer)
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code) (`claude` CLI) and/or [Codex CLI](https://github.com/openai/codex) installed

## Installation

```bash
go install github.com/chaspy/agentctl@latest
```

## Quick Start

```bash
# List all sessions
agentctl list

# List with SQLite sync
agentctl list --sync

# Read the latest response from a session
agentctl read <session-name>

# Send a message and wait for response
agentctl send <session-name> "your message"

# Check rate limits
agentctl rate

# Spawn a new session
agentctl spawn owner/repo --branch feature/foo --message "implement feature X"

# Kill a session (with safety checks)
agentctl kill <session-name>

# Monitor all sessions for changes
agentctl monitor --target <your-session> --interval 30

# Start PWA dashboard
agentctl serve
```

## Commands

| Command | Description |
|---|---|
| `list` | List all active Claude Code / Codex sessions |
| `read <name>` | Read the latest response from a session |
| `send <name> <msg>` | Send a message and wait for response |
| `watch <name>` | Watch a session until its response changes |
| `monitor` | Poll all sessions and notify a target session on changes |
| `rate` | Show rate limit status for Claude Code and Codex |
| `spawn <repo>` | Create a new zellij session with optional worktree |
| `kill <name>` | Terminate a session and clean up its worktree |
| `resume <name>` | Resume a stopped session |
| `preview <PR>` | Preview a pull request in a temporary worktree |
| `serve` | Start PWA dashboard (default: port 8080) |
| `state sync` | Sync live session data to SQLite |
| `state show` | Show saved state from SQLite |
| `state log` | Record or view action logs |
| `state task` | Manage tasks (add, complete, list) |
| `config` | Manage per-repository configuration |
| `repos <query>` | Search for repositories on disk |

## Session Status Detection

agentctl detects session status by analyzing the last JSONL message and process state:

| Status | Meaning |
|---|---|
| `active` | Responding - last message is from user and process is alive |
| `idle` | Waiting - process alive, not in other states |
| `blocked` | Waiting for human action (keywords like "approve", "confirm" detected) |
| `error` | API error or rate limit hit |
| `dead` | Process not running |

## State Management

agentctl uses SQLite (stored at `.claude/manager.db`) for persistent state:

- **Sessions**: Synced from live scans, preserving status history
- **Tasks**: Track work items per session
- **Actions**: Log decisions and events for auditability
- **Repo configs**: Per-repository settings (branching mode, descriptions)

## Architecture

```
cmd/              CLI commands (cobra)
internal/
  mux/            tmux/zellij abstraction
  process/        Process detection (PID, CWD matching)
  provider/       Claude/Codex session scanning, rate limits
  session/        JSONL parser, status detection
  store/          SQLite persistence
  web/            PWA dashboard (embedded static files)
```

## License

MIT
