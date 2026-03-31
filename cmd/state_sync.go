package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/mux"
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

	// Build sets from state KV store
	allState, _ := store.AllState(db)
	loopCWDs := make(map[string]bool)
	spawnSummaries := make(map[string]string)
	for k, v := range allState {
		if len(k) > 9 && k[:9] == "loop:cwd:" && v == "1" {
			loopCWDs[k[9:]] = true
		}
		if strings.HasPrefix(k, "spawn_summary:cwd:") && v != "" {
			spawnSummaries[k[len("spawn_summary:cwd:"):]] = v
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
		// Skip preview worktree sessions (defense in depth; scanner also filters)
		if strings.Contains(s.CWD, "worktree-preview-") {
			continue
		}
		if s.CWD != "" {
			seen[s.CWD] = true
		}
		deduped = append(deduped, s)
	}

	// Build mux session set for alive validation
	muxSessionSet := buildMuxSessionSet()

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

		// Validate alive against actual mux sessions and infer zellij_session.
		// This prevents ghost sessions from JSONL files that have no corresponding mux session.
		zellijSession := ""
		if alive {
			alive, zellijSession = validateAliveWithMux(db, s, muxSessionSet)
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

		// Normalize repository name before building session ID
		repoName := s.Repository
		if corrected, ok := knownRepoCorrections[repoName]; ok {
			repoName = corrected
		}

		id := fmt.Sprintf("%s:%s:%s", s.Agent, repoName, s.SessionID)
		scannedIDs = append(scannedIDs, id)

		if err := store.UpsertSession(db, &store.Session{
			ID:            id,
			Agent:         string(s.Agent),
			Repository:    repoName,
			SessionID:     s.SessionID,
			CWD:           s.CWD,
			GitBranch:     s.GitBranch,
			ZellijSession: zellijSession,
			Status:        status,
			Alive:         alive,
			LastMessage:   s.LastMessage,
			LastRole:      s.LastRole,
			LastActive:    s.ModTime,
			Role:          role,
			IsLoop:        loopCWDs[s.CWD],
		}); err != nil {
			fmt.Printf("warning: could not upsert session %s: %v\n", id, err)
			continue
		}

		// Apply task_summary:
		// 1. spawn --summary: use the pre-set value (one-shot, consumed after use)
		// 2. --regenerate-summaries: always regenerate via claude -p
		// 3. normal sync: generate only if DB has empty task_summary
		if summary, ok := spawnSummaries[s.CWD]; ok {
			_ = store.UpdateTaskSummary(db, id, summary)
			_ = store.DeleteState(db, "spawn_summary:cwd:"+s.CWD)
		} else if regenerateSummaries && s.FilePath != "" {
			if title := session.GenerateTaskTitle(s.FilePath); title != "" {
				_ = store.UpdateTaskSummary(db, id, title)
			}
		} else if s.FilePath != "" {
			existing, err := store.GetSession(db, id)
			if err == nil && existing.TaskSummary == "" {
				if title := session.GenerateTaskTitle(s.FilePath); title != "" {
					_ = store.UpdateTaskSummary(db, id, title)
				}
			}
		}

		upserted++
	}

	// Mark sessions in DB but not found in scan as dead
	_ = store.MarkStaleSessionsDead(db, scannedIDs)

	// Cross-check alive sessions against actual mux sessions
	markOrphanedSessionsDead(db)

	// Normalize known incorrect repository names in existing records
	normalizeExistingRepoNames(db)

	// Auto-archive dead/error sessions to sessions_archive table
	if archived, err := store.ArchiveDeadSessions(db); err == nil && archived > 0 {
		fmt.Printf("Auto-archived %d dead/error session(s)\n", archived)
	}

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
		// Skip if we recently checked and found no PR (avoid repeated API calls)
		// But always allow the first check (when no cache key exists yet)
		noPRKey := "no_pr_checked:" + repo + ":" + s.GitBranch
		if lastChecked, _ := store.GetState(db, noPRKey); lastChecked != "" {
			if t, err := time.Parse(time.RFC3339, lastChecked); err == nil && time.Since(t) < 5*time.Minute {
				continue
			}
		}
		if prURL := lookupPRURL(repo, s.GitBranch); prURL != "" {
			db.Exec("UPDATE sessions SET pr_url = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", prURL, id)
		} else {
			// Cache the negative result to avoid re-querying every 30 seconds
			_ = store.SetState(db, noPRKey, time.Now().Format(time.RFC3339))
		}
	}

	// Check PR conflicts and send rebase instructions (throttled to every 5 minutes)
	conflictCheckKey := "last_conflict_check"
	if lastCheck, _ := store.GetState(db, conflictCheckKey); lastCheck != "" {
		if t, err := time.Parse(time.RFC3339, lastCheck); err == nil && time.Since(t) < 5*time.Minute {
			return upserted, nil
		}
	}
	checkPRConflicts(db)
	_ = store.SetState(db, conflictCheckKey, time.Now().Format(time.RFC3339))

	return upserted, nil
}

// checkPRConflicts checks mergeable state for alive sessions with PRs
// and sends rebase instructions to sessions with conflicting PRs.
func checkPRConflicts(db *sql.DB) {
	sessions, err := store.ListAliveSessionsWithPR(db)
	if err != nil || len(sessions) == 0 {
		return
	}

	adapter, muxErr := mux.Resolve("auto")

	for _, s := range sessions {
		prURL := s.PRURL
		prNum := extractPRNumber(prURL)
		if prNum == "" {
			continue
		}

		repo := repoFromRepository(s.Repository)
		if repo == "" {
			continue
		}

		// Check if we already sent a rebase instruction recently (within 1 hour)
		stateKey := "rebase_sent:" + prURL
		if lastSent, _ := store.GetState(db, stateKey); lastSent != "" {
			t, err := time.Parse(time.RFC3339, lastSent)
			if err == nil && time.Since(t) < time.Hour {
				continue
			}
		}

		// Check cached mergeable state (5 min TTL) to avoid redundant API calls
		mergeableCacheKey := "mergeable_cache:" + prURL
		mergeable := ""
		if cached, _ := store.GetState(db, mergeableCacheKey); cached != "" {
			// Format: "STATE|RFC3339_TIME"
			if parts := strings.SplitN(cached, "|", 2); len(parts) == 2 {
				if t, err := time.Parse(time.RFC3339, parts[1]); err == nil && time.Since(t) < 5*time.Minute {
					mergeable = parts[0]
				}
			}
		}
		if mergeable == "" {
			mergeable = checkPRMergeable(repo, prNum)
			if mergeable != "" {
				_ = store.SetState(db, mergeableCacheKey, mergeable+"|"+time.Now().Format(time.RFC3339))
			}
		}
		if mergeable != "CONFLICTING" {
			continue
		}

		if muxErr != nil {
			fmt.Printf("[sync] PR #%s is CONFLICTING but no mux available: %v\n", prNum, muxErr)
			continue
		}

		// Find the mux session name to send to
		sessionName := resolveMuxSessionName(s, adapter)
		if sessionName == "" {
			fmt.Printf("[sync] PR #%s is CONFLICTING but could not resolve mux session for %s\n", prNum, s.Repository)
			continue
		}

		fmt.Printf("[sync] PR #%s is CONFLICTING, sending rebase instruction to session %s\n", prNum, sessionName)

		rebaseMsg := "PR がコンフリクトしています。git fetch origin && git rebase origin/main でコンフリクトを解消して git push --force-with-lease してください。"
		if err := adapter.SendKeys(sessionName, rebaseMsg); err != nil {
			fmt.Printf("[sync] failed to send rebase instruction to %s: %v\n", sessionName, err)
			continue
		}

		// Record the time we sent the rebase instruction
		_ = store.SetState(db, stateKey, time.Now().Format(time.RFC3339))

		// Log the action
		_ = store.LogAction(db, &store.Action{
			SessionID:  s.ID,
			ActionType: "rebase_instruction",
			Content:    rebaseMsg,
			Result:     fmt.Sprintf("PR #%s CONFLICTING", prNum),
		})
	}
}

// extractPRNumber extracts the PR number from a GitHub PR URL.
// e.g., "https://github.com/owner/repo/pull/42" -> "42"
func extractPRNumber(prURL string) string {
	parts := strings.Split(prURL, "/")
	if len(parts) < 2 || parts[len(parts)-2] != "pull" {
		return ""
	}
	return parts[len(parts)-1]
}

// prMergeableResult is used to parse gh pr view JSON output.
type prMergeableResult struct {
	Mergeable string `json:"mergeable"`
}

// checkPRMergeable returns the mergeable state of a PR.
// Returns "MERGEABLE", "CONFLICTING", "UNKNOWN", or "" on error.
var checkPRMergeable = func(repo, prNumber string) string {
	cmd := exec.Command("gh", "pr", "view", prNumber, "--repo", repo, "--json", "mergeable")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var result prMergeableResult
	if err := json.Unmarshal(out, &result); err != nil {
		return ""
	}
	return result.Mergeable
}

// resolveMuxSessionName finds the mux session name for a given DB session.
func resolveMuxSessionName(s store.Session, adapter mux.Adapter) string {
	// Try zellij_session field first
	if s.ZellijSession != "" {
		if resolved, err := adapter.ResolveSession(s.ZellijSession); err == nil {
			return resolved
		}
	}
	// Try repository name
	if resolved, err := adapter.ResolveSession(s.Repository); err == nil {
		return resolved
	}
	return ""
}

// worktreeSuffix matches "-worktree-<branch>" or "/worktree-<branch>" suffixes
// that spawn creates for git worktrees.
var worktreeSuffix = regexp.MustCompile(`[/-]worktree-.+$`)

// knownRepoCorrections maps incorrect repository names to their correct values.
// These are known data quality issues from historical sync data.
var knownRepoCorrections = map[string]string{
	"chaspy/myassistant-server": "chaspy/myassistant",
	"studiuos/jp-Studious-JP":  "studiuos-jp/Studious_JP",
}

// repoFromRepository extracts the clean GitHub "owner/repo" from the repository field.
// Handles worktree-suffixed names like "chaspy/agentctl/worktree-feat-xxx" -> "chaspy/agentctl".
// Also applies known corrections for historically incorrect repository names.
func repoFromRepository(repository string) string {
	// Strip worktree suffix if present
	cleaned := worktreeSuffix.ReplaceAllString(repository, "")
	// Expect "owner/repo" format
	parts := strings.Split(cleaned, "/")
	var repo string
	if len(parts) >= 2 {
		repo = parts[0] + "/" + parts[1]
	}
	if repo == "" {
		return ""
	}
	// Apply known corrections
	if corrected, ok := knownRepoCorrections[repo]; ok {
		return corrected
	}
	return repo
}

// normalizeExistingRepoNames updates repository names in the DB for sessions
// that have known incorrect names.
func normalizeExistingRepoNames(db *sql.DB) {
	for incorrect, correct := range knownRepoCorrections {
		result, err := db.Exec(
			"UPDATE sessions SET repository = ?, updated_at = CURRENT_TIMESTAMP WHERE repository = ?",
			correct, incorrect)
		if err != nil {
			continue
		}
		if n, _ := result.RowsAffected(); n > 0 {
			fmt.Printf("Normalized %d session(s): %s -> %s\n", n, incorrect, correct)
		}
		// Also fix in archive table
		db.Exec(
			"UPDATE sessions_archive SET repository = ?, updated_at = CURRENT_TIMESTAMP WHERE repository = ?",
			correct, incorrect)
	}
}

// inferZellijSession derives the expected zellij session name from a CWD path
// by matching spawn's naming convention, then checks if it exists in the mux session list.
// Returns the matched session name or "".
//
// Spawn naming convention:
//   - /path/to/repo                          -> "repo"
//   - /path/to/repo/worktree-fix-bug         -> "repo-fix-bug"
func inferZellijSession(cwd string, muxSet map[string]bool) string {
	if cwd == "" || muxSet == nil {
		return ""
	}

	// Determine repo base name and optional worktree branch
	parts := strings.Split(strings.TrimRight(cwd, "/"), "/")
	if len(parts) == 0 {
		return ""
	}

	lastPart := parts[len(parts)-1]

	// Check if last part is a worktree directory (worktree-<branch>)
	if strings.HasPrefix(lastPart, "worktree-") && len(parts) >= 2 {
		repoBase := parts[len(parts)-2]
		branch := strings.TrimPrefix(lastPart, "worktree-")
		candidate := repoBase + "-" + branch
		if muxSet[strings.ToLower(candidate)] {
			return candidate
		}
		// Also try exact case match
		if muxSet[candidate] {
			return candidate
		}
	}

	// Non-worktree: session name = directory name
	if muxSet[strings.ToLower(lastPart)] {
		return lastPart
	}

	return ""
}

// buildMuxSessionSet returns a set of lowercased mux session names, or nil if mux unavailable.
func buildMuxSessionSet() map[string]bool {
	sessions := listMuxSessions()
	if sessions == nil {
		return nil
	}
	set := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		set[strings.ToLower(s)] = true
	}
	return set
}

// validateAliveWithMux checks whether a session should be considered alive
// by verifying it has a zellij_session that exists in the mux session list.
// If no zellij_session is stored, tries to infer one from CWD.
// Returns (alive bool, zellijSession string to set on the record).
func validateAliveWithMux(db *sql.DB, s provider.SessionInfo, muxSet map[string]bool) (bool, string) {
	if muxSet == nil {
		// No mux available; fall back to process-only detection
		return true, ""
	}

	// Check if DB already has a zellij_session for this session
	id := fmt.Sprintf("%s:%s:%s", s.Agent, s.Repository, s.SessionID)
	existing, err := store.GetSession(db, id)
	if err == nil && existing.ZellijSession != "" {
		// Verify the zellij session actually exists
		if muxSet[strings.ToLower(existing.ZellijSession)] {
			return true, existing.ZellijSession
		}
		return false, existing.ZellijSession
	}

	// No zellij_session in DB — try to infer from CWD
	if inferred := inferZellijSession(s.CWD, muxSet); inferred != "" {
		return true, inferred
	}

	// No zellij_session known or inferable -> not alive
	return false, ""
}

// listMuxSessions returns the list of active mux session names.
// Extracted as a variable for testability.
var listMuxSessions = func() []string {
	adapter, err := mux.Resolve("auto")
	if err != nil {
		return nil
	}
	sessions, err := adapter.ListSessions()
	if err != nil {
		return nil
	}
	return sessions
}

// markOrphanedSessionsDead marks sessions as dead when they are alive in DB
// but have no corresponding mux session. This handles cases where a zellij
// session was killed externally without going through agentctl.
// Sessions without a zellij_session are also marked dead (ghost sessions).
func markOrphanedSessionsDead(db *sql.DB) {
	muxSessions := listMuxSessions()
	if muxSessions == nil {
		return // no mux available, skip check
	}

	muxSet := make(map[string]bool)
	for _, s := range muxSessions {
		muxSet[strings.ToLower(s)] = true
	}

	aliveSessions, err := store.ListSessionsByAlive(db, true)
	if err != nil || len(aliveSessions) == 0 {
		return
	}

	for _, s := range aliveSessions {
		if s.ZellijSession == "" {
			// No zellij_session -> ghost session, mark dead
			db.Exec("UPDATE sessions SET alive = 0, status = 'dead', updated_at = CURRENT_TIMESTAMP WHERE id = ?", s.ID)
			continue
		}
		if !muxSet[strings.ToLower(s.ZellijSession)] {
			db.Exec("UPDATE sessions SET alive = 0, status = 'dead', updated_at = CURRENT_TIMESTAMP WHERE id = ?", s.ID)
			fmt.Printf("Marked orphaned session as dead: %s (zellij session %q not found)\n", s.ID, s.ZellijSession)
		}
	}
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
