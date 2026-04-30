# Observability Exporter

`agent-exporter` の Prometheus / MCP observability surface は、`agentctl` monorepo に統合された。

現在は独立 binary ではなく、`agentctl exporter` で起動する。

## 目的

- `~/.agentctl/manager.db` から session / task / action / job の状態を読む
- `~/.agentctl/ccusage-cache.json` から cost-per-hour を読む
- `~/.codex/state_5.sqlite` から Codex thread usage を読む
- `~/.claude/projects/**/*.jsonl` から Claude usage を読む
- Prometheus `/metrics` と MCP `/mcp` を同時に出す

## 起動

```bash
agentctl exporter
```

config file を変える場合:

```bash
agentctl exporter --config /path/to/exporter.yaml
```

## 設定

config file が存在しない場合は、以下の built-in default を使う。

```yaml
agentctl_db: ${HOME}/.agentctl/manager.db
ccusage_cache: ${HOME}/.agentctl/ccusage-cache.json
codex_db: ${HOME}/.codex/state_5.sqlite
claude_projects_dir: ${HOME}/.claude/projects
prometheus_port: 9100
mcp_port: 9101
collect_interval: 5m
```

## Endpoint

- Prometheus metrics: `http://localhost:9100/metrics`
- MCP HTTP server: `http://localhost:9101/mcp`
- Health check: `http://localhost:9101/health`

## launchd

macOS では `launchd/com.chaspy.agentctl-exporter.plist` を使って常駐化できる。

- label: `com.chaspy.agentctl-exporter`
- binary: `/Users/chaspy/go/bin/agentctl`
- args: `exporter`
- logs:
  - `/tmp/agentctl-exporter.log`
  - `/tmp/agentctl-exporter.err`

## 互換性

- metric 名は旧 `agent-exporter` と同じまま維持する
- MCP server name も互換維持のため `agent-exporter` のまま
- repo 統合後は release / install / docs の正本を `agentctl` 側に寄せる
