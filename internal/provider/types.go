package provider

import "time"

type Agent string

const (
	AgentClaude Agent = "claude"
	AgentCodex  Agent = "codex"
)

type SessionInfo struct {
	Agent       Agent
	Repository string
	ModTime     time.Time
	SessionID   string
	CWD         string
	GitBranch   string
	LastMessage     string
	LastFullMessage string
	LastRole        string
	FilePath        string
	Status          string // "blocked", "error", "idle", "active", "dead"
	ErrorType       string // error type from JSONL metadata (e.g. "rate_limit")
	IsAPIError      bool   // true if isApiErrorMessage flag is set in JSONL
}

type RateInfo struct {
	Agent        Agent
	Summary      string
	Details      string
	UpdatedAt    time.Time
	BurnRate     string // active sessions and estimated time to limit
	RemainingPct int    // estimated remaining capacity (0-100), -1 = unknown
}
