# agentctl

A CLI tool for managing multiple coding agent sessions (Claude Code, Codex CLI) running in [zellij](https://zellij.dev/) or [tmux](https://github.com/tmux/tmux).

## Features

- **Session listing** - Scan `~/.claude/projects/` and `~/.codex/sessions/` to display all active sessions with status detection
- **Session communication** - Send messages to sessions via terminal multiplexer and wait for responses
- **Session lifecycle** - Spawn new sessions with git worktree isolation, kill finished ones with safety checks
- **Monitoring** - Watch for session changes, auto-notify on new assistant responses
- **Rate limit tracking** - Check Claude Code and Codex CLI rate limit status
- **State persistence** - SQLite-backed state with session sync, task tracking, and action logging
- **Protected adoption queue** - Stage unmanaged Codex sessions for later safe adoption without touching the live session immediately
- **Handoff telemetry** - Persist route reason, handoff summary, and token burn for completed worker sessions
- **PWA dashboard** - Web-based dashboard for mobile monitoring, including control-plane observed state for applied ManagedRepo resources
- **Integrated observability exporter** - Built-in Prometheus `/metrics` and MCP `/mcp` surfaces for agent activity

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

# Send a message and wait up to 30 seconds for response
agentctl send <session-name> "your message"

# Verify delivery 20 seconds after send and retry automatically if needed
agentctl send <session-name> "your message" --verify

# Check rate limits
agentctl rate

# Spawn a new session
agentctl spawn owner/repo --branch feature/foo --message "implement feature X"

# Let agentctl choose Claude/Codex automatically from task type + current rate state
agentctl spawn owner/repo --branch feature/foo --task-type research --agent auto --message "analyze PR #123"

# Pin a repository to Codex workers by default
agentctl config set owner/repo --agent codex

# Import a ManagedRepo manifest into the local DB
agentctl apply -f ~/go/src/github.com/chaspy/myassistant/ops/repos/book-assistant.yaml

# Validate a self-hosting spec in read-only mode
agentctl validate -f ~/go/src/github.com/chaspy/myassistant/ops/system/ecosystem.yaml --json

# Read applied ManagedRepo desired state from the local DB
agentctl state managed-repo list --json

# Compare applied desired state against observed local checkouts
agentctl reconcile once --read-only --json

# Kill a session (with safety checks)
agentctl kill <session-name>

# Monitor all sessions for changes
agentctl monitor --target <your-session> --interval 30

# Start PWA dashboard (Dashboard / Control Plane / Database tabs)
agentctl serve

# Start the integrated Prometheus / MCP exporter
agentctl exporter
```

## Commands

| Command | Description |
|---|---|
| `list` | List all active Claude Code / Codex sessions |
| `read <name>` | Read the latest response from a session |
| `send <name> <msg>` | Send a message and wait up to 30 seconds for response |
| `watch <name>` | Watch a session until its response changes |
| `monitor` | Poll all sessions and notify a target session on changes |
| `rate` | Show rate limit status for Claude Code and Codex |
| `spawn <repo>` | Create a new zellij session with optional worktree |
| `kill <name>` | Terminate a session and clean up its worktree |
| `resume <name>` | Resume a stopped session |
| `preview <PR>` | Preview a pull request in a temporary worktree |
| `serve` | Start PWA dashboard (default: port 8080) |
| `exporter` | Start the integrated Prometheus / MCP observability exporter |
| `state sync` | Sync live session data to SQLite and back up the DB |
| `state adopt <zellij-session>` | Queue a protected adoption plan for an unmanaged Codex session |
| `state import-from-zellij` | Rebuild DB session records from current zellij sessions |
| `state show` | Show saved state from SQLite |
| `state managed-repo` | Inspect applied ManagedRepo desired state from SQLite |
| `state log` | Record or view action logs |
| `state log handoff <session-id>` | Record route reason, handoff summary, and token burn |
| `state task` | Manage tasks (add, complete, list) |
| `config` | Manage per-repository configuration |
| `apply` | Import a desired-state manifest into the local control-plane DB |
| `validate` | Validate desired-state manifests without mutating runtime state |
| `reconcile once --read-only` | Compare applied ManagedRepo desired state against observed local clones and repo contracts |
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

agentctl uses SQLite for persistent state. The database is stored at `~/.agentctl/manager.db` by default, and can be overridden with the `AGENTCTL_DB_PATH` environment variable. On first run, if the new location doesn't exist but `.claude/manager.db` does, the old database is automatically copied over. Each successful sync also writes a backup to `~/.agentctl/manager.db.bak` (or `<AGENTCTL_DB_PATH>.bak`).

- **Sessions**: Synced from live scans, preserving status history
- **Queued adoptions**: Record protected adoption plans for unmanaged Codex sessions before any live import is attempted
- **Tasks**: Track work items per session
- **Actions**: Log decisions and events for auditability
- **Handoffs**: Persist worker route reason, completion summary, and token burn in the action log
- **Repo configs**: Per-repository settings (branching mode, preferred agent, descriptions)

See [docs/protected-codex-adoption.md](docs/protected-codex-adoption.md) for the current protected-adoption workflow and its non-goals.

## Architecture

```
cmd/              CLI commands (cobra)
internal/
  mux/            tmux/zellij abstraction
  observability/  Prometheus collector, exporter server, MCP server
  process/        Process detection (PID, CWD matching)
  provider/       Claude/Codex session scanning, rate limits
  session/        JSONL parser, status detection
  store/          SQLite persistence
  web/            PWA dashboard (embedded static files)
```

## License

MIT
