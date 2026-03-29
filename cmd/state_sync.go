package cmd

import (
	"database/sql"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/process"
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/session"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	syncAgent              string
	syncHours              int
	syncRegenerateSummaries bool
)

var stateSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Scan live sessions and sync to database",
	RunE:  runStateSync,
}

func init() {
	stateCmd.AddCommand(stateSyncCmd)
	stateSyncCmd.Flags().StringVar(&syncAgent, "agent", "all", "Filter by agent: all, claude, codex")
	stateSyncCmd.Flags().IntVar(&syncHours, "hours", 24, "Scan sessions active within the last N hours")
	stateSyncCmd.Flags().BoolVar(&syncRegenerateSummaries, "regenerate-summaries", false, "Regenerate task_summary for all sessions")
}

func runStateSync(cmd *cobra.Command, args []string) error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	count, err := syncSessionsToDB(db, syncAgent, syncHours, syncRegenerateSummaries)
	if err != nil {
		return err
	}

	fmt.Printf("Synced %d sessions to database\n", count)
	return nil
}

// syncSessionsToDB scans live JSONL sessions and upserts them into the database.
// It deduplicates by CWD (keeping most recent) and marks stale sessions as dead.
// Returns the number of synced sessions.
func syncSessionsToDB(db *sql.DB, agentFilter string, hours int, regenerateSummaries bool) (int, error) {
	agents, err := selectedAgents(agentFilter)
	if err != nil {
		return 0, err
	}

	maxAge := time.Duration(hours) * time.Hour
	var sessions []provider.SessionInfo
	for _, agent := range agents {
		switch agent {
		case provider.AgentClaude:
			items, err := provider.ScanClaudeSessions(maxAge)
			if err != nil {
				fmt.Printf("warning: could not scan claude sessions: %v\n", err)
				continue
			}
			sessions = append(sessions, items...)
		case provider.AgentCodex:
			items, err := provider.ScanCodexSessions(maxAge)
			if err != nil {
				fmt.Printf("warning: could not scan codex sessions: %v\n", err)
				continue
			}
			sessions = append(sessions, items...)
		}
	}

	claudeProcs, _ := process.FindClaudeProcesses()
	codexProcs, _ := process.FindCodexProcesses()
	managerName, _ := store.GetState(db, "manager_session_name")

	// Build set of CWDs marked as loop sessions
	allState, _ := store.AllState(db)
	loopCWDs := make(map[string]bool)
	for k, v := range allState {
		if len(k) > 9 && k[:9] == "loop:cwd:" && v == "1" {
			loopCWDs[k[9:]] = true
		}
	}

	// Deduplicate by CWD: keep only the most recent session per CWD
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})
	seen := make(map[string]bool)
	var deduped []provider.SessionInfo
	for _, s := range sessions {
		if s.CWD != "" && seen[s.CWD] {
			continue
		}
		if s.CWD != "" {
			seen[s.CWD] = true
		}
		deduped = append(deduped, s)
	}

	var scannedIDs []string
	var upserted int
	for _, s := range deduped {
		alive := false
		switch s.Agent {
		case provider.AgentClaude:
			alive = process.IsAliveForCWD(claudeProcs, s.CWD)
		case provider.AgentCodex:
			alive = process.IsAliveForCWD(codexProcs, s.CWD)
		}

		statusMsg := s.LastFullMessage
		if statusMsg == "" {
			statusMsg = s.LastMessage
		}
		status := session.DetectStatus(statusMsg, s.LastRole, alive, s.ErrorType, s.IsAPIError)

		role := "worker"
		if managerName != "" && s.Repository == "agentctl" {
			role = "manager"
		}

		id := fmt.Sprintf("%s:%s:%s", s.Agent, s.Repository, s.SessionID)
		scannedIDs = append(scannedIDs, id)

		if err := store.UpsertSession(db, &store.Session{
			ID:          id,
			Agent:       string(s.Agent),
			Repository:  s.Repository,
			SessionID:   s.SessionID,
			CWD:         s.CWD,
			GitBranch:   s.GitBranch,
			Status:      status,
			Alive:       alive,
			LastMessage: s.LastMessage,
			LastRole:    s.LastRole,
			LastActive:  s.ModTime,
			Role:        role,
			IsLoop:      loopCWDs[s.CWD],
		}); err != nil {
			fmt.Printf("warning: could not upsert session %s: %v\n", id, err)
			continue
		}

		// Generate task_summary only when needed:
		// --regenerate-summaries: always regenerate
		// normal sync: generate only if DB has empty task_summary
		if s.FilePath != "" {
			shouldGenerate := false
			if regenerateSummaries {
				shouldGenerate = true
			} else {
				existing, err := store.GetSession(db, id)
				if err == nil && existing.TaskSummary == "" {
					shouldGenerate = true
				}
			}
			if shouldGenerate {
				if title := session.GenerateTaskTitle(s.FilePath); title != "" {
					_ = store.UpdateTaskSummary(db, id, title)
				}
			}
		}

		upserted++
	}

	// Mark sessions in DB but not found in scan as dead
	_ = store.MarkStaleSessionsDead(db, scannedIDs)

	// Fetch PR URLs for sessions that don't have one yet
	for _, s := range deduped {
		id := fmt.Sprintf("%s:%s:%s", s.Agent, s.Repository, s.SessionID)
		if s.GitBranch == "" || s.GitBranch == "main" || s.GitBranch == "master" {
			continue
		}
		// Skip if already cached in DB
		if existing := store.GetSessionPRURL(db, id); existing != "" {
			continue
		}
		repo := repoFromRepository(s.Repository)
		if repo == "" {
			continue
		}
		if prURL := lookupPRURL(repo, s.GitBranch); prURL != "" {
			db.Exec("UPDATE sessions SET pr_url = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", prURL, id)
		}
	}

	return upserted, nil
}

// worktreeSuffix matches "-worktree-<branch>" or "/worktree-<branch>" suffixes
// that spawn creates for git worktrees.
var worktreeSuffix = regexp.MustCompile(`[/-]worktree-.+$`)

// repoFromRepository extracts the clean GitHub "owner/repo" from the repository field.
// Handles worktree-suffixed names like "chaspy/agentctl/worktree-feat-xxx" -> "chaspy/agentctl".
func repoFromRepository(repository string) string {
	// Strip worktree suffix if present
	cleaned := worktreeSuffix.ReplaceAllString(repository, "")
	// Expect "owner/repo" format
	parts := strings.Split(cleaned, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return ""
}

// lookupPRURL uses gh CLI to find the PR URL for a given branch and repo.
func lookupPRURL(repo, branch string) string {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--repo", repo, "--json", "url", "--jq", ".[0].url")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
