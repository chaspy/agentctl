# Protected Codex Session Adoption

`agentctl state adopt <zellij-session>` adds a protected adoption plan for a live unmanaged Codex session.

## Current Scope

- Detect a live zellij session and infer `cwd` / `repository` / `git_branch` where possible
- Store the planned adoption in SQLite (`session_adoptions`)
- Default the future target permission to `suggest`
- Support `--dry-run` so operators can validate detection without changing the database

## Non-Goals

- Do not import the live session into the `sessions` table yet
- Do not send commands to the live session
- Do not mutate or restart the live zellij session
- Do not apply anything automatically to production sessions such as `atama` / `1733`

## Example

```bash
# Preview only
agentctl state adopt 1733 --dry-run

# Queue a protected adoption plan without touching the live session
agentctl state adopt 1733 --note "prepare protected Codex adoption"
```

Queued entries are visible in `agentctl state show`.
