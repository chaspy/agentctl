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
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/session"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	syncAgent               string
	syncHours               int
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

// syncSessionsToDB performs a minimal sync:
//  1. Update runtime_status from zellij sessions (alive=1 DB records only)
//  2. Enrich CWD/repo/branch for alive sessions with empty CWD via dump-layout
//  3. Read JSONL to update LastMessage for alive sessions with CWD (UPDATE only, no INSERT)
//
// DB is the sole source of truth. Only spawn and kill write new records.
// sync only updates existing records.
func syncSessionsToDB(db *sql.DB, agentFilter string, hours int, regenerateSummaries bool) (int, error) {
	// ── Step 1+2: Runtime Status + dump-layout enrichment ──
	syncRuntimeStatus(db)

	// ── Step 3: JSONL Enrichment (UPDATE only, no INSERT) ──
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

	// Deduplicate JSONL by CWD: keep only the most recent session per CWD
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})
	seen := make(map[string]bool)
	var deduped []provider.SessionInfo
	for _, s := range sessions {
		if s.CWD != "" && seen[s.CWD] {
			continue
		}
		if strings.Contains(s.CWD, "worktree-preview-") {
			continue
		}
		if s.CWD != "" {
			seen[s.CWD] = true
		}
		deduped = append(deduped, s)
	}

	// Build CWD -> JSONL index for quick lookup
	jsonlByCWD := make(map[string]provider.SessionInfo)
	for _, s := range deduped {
		if s.CWD != "" {
			jsonlByCWD[s.CWD] = s
		}
	}

	// Enrich existing alive DB sessions with JSONL metadata (UPDATE only).
	aliveSessions, _ := store.ListSessionsByAlive(db, true)
	var enriched int
	for _, dbSess := range aliveSessions {
		if dbSess.CWD == "" {
			continue // no CWD → can't match JSONL
		}
		jsonl, found := jsonlByCWD[dbSess.CWD]
		if !found {
			continue
		}

		statusMsg := jsonl.LastFullMessage
		if statusMsg == "" {
			statusMsg = jsonl.LastMessage
		}
		status := session.DetectStatus(statusMsg, jsonl.LastRole, dbSess.Alive, jsonl.ErrorType, jsonl.IsAPIError)

		role := dbSess.Role
		if role == "" {
			role = "worker"
		}
		if managerName != "" && dbSess.Repository == "agentctl" {
			role = "manager"
		}

		// UPDATE only — never creates new records
		_ = store.UpdateSessionMetadata(db, &store.Session{
			ID:         dbSess.ID,
			Status:     status,
			GitBranch:  jsonl.GitBranch,
			LastMessage: jsonl.LastMessage,
			LastRole:   jsonl.LastRole,
			LastActive: jsonl.ModTime,
			Role:       role,
			IsLoop:     loopCWDs[dbSess.CWD],
		})

		// Apply task_summary
		if summary, ok := spawnSummaries[dbSess.CWD]; ok {
			_ = store.UpdateTaskSummary(db, dbSess.ID, summary)
			_ = store.DeleteState(db, "spawn_summary:cwd:"+dbSess.CWD)
		} else if regenerateSummaries && jsonl.FilePath != "" {
			if title := session.GenerateTaskTitle(jsonl.FilePath); title != "" {
				_ = store.UpdateTaskSummary(db, dbSess.ID, title)
			}
		} else if jsonl.FilePath != "" {
			if dbSess.TaskSummary == "" {
				if title := session.GenerateTaskTitle(jsonl.FilePath); title != "" {
					_ = store.UpdateTaskSummary(db, dbSess.ID, title)
				}
			}
		}

		enriched++
	}

	// Normalize known incorrect repository names in existing records
	normalizeExistingRepoNames(db)

	// Auto-archive dead/error sessions to sessions_archive table
	if archived, err := store.ArchiveDeadSessions(db); err == nil && archived > 0 {
		fmt.Printf("Auto-archived %d dead/error session(s)\n", archived)
	}

	// Fetch PR URLs for alive sessions that don't have one yet
	aliveAfterSync, _ := store.ListSessionsByAlive(db, true)
	for _, s := range aliveAfterSync {
		if s.GitBranch == "" || s.GitBranch == "main" || s.GitBranch == "master" {
			continue
		}
		if s.PRURL != "" {
			continue
		}
		repo := repoFromRepository(s.Repository)
		if repo == "" {
			continue
		}
		noPRKey := "no_pr_checked:" + repo + ":" + s.GitBranch
		if lastChecked, _ := store.GetState(db, noPRKey); lastChecked != "" {
			if t, err := time.Parse(time.RFC3339, lastChecked); err == nil && time.Since(t) < 5*time.Minute {
				continue
			}
		}
		if prURL := lookupPRURL(repo, s.GitBranch); prURL != "" {
			db.Exec("UPDATE sessions SET pr_url = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", prURL, s.ID)
		} else {
			_ = store.SetState(db, noPRKey, time.Now().Format(time.RFC3339))
		}
	}

	// Check PR conflicts and send rebase instructions (throttled to every 5 minutes)
	conflictCheckKey := "last_conflict_check"
	if lastCheck, _ := store.GetState(db, conflictCheckKey); lastCheck != "" {
		if t, err := time.Parse(time.RFC3339, lastCheck); err == nil && time.Since(t) < 5*time.Minute {
			return enriched, nil
		}
	}
	checkPRConflicts(db)
	_ = store.SetState(db, conflictCheckKey, time.Now().Format(time.RFC3339))

	return enriched, nil
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

		_ = store.SetState(db, stateKey, time.Now().Format(time.RFC3339))
		_ = store.LogAction(db, &store.Action{
			SessionID:  s.ID,
			ActionType: "rebase_instruction",
			Content:    rebaseMsg,
			Result:     fmt.Sprintf("PR #%s CONFLICTING", prNum),
		})
	}
}

// extractPRNumber extracts the PR number from a GitHub PR URL.
func extractPRNumber(prURL string) string {
	parts := strings.Split(prURL, "/")
	if len(parts) < 2 || parts[len(parts)-2] != "pull" {
		return ""
	}
	return parts[len(parts)-1]
}

type prMergeableResult struct {
	Mergeable string `json:"mergeable"`
}

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

func resolveMuxSessionName(s store.Session, adapter mux.Adapter) string {
	if s.ZellijSession != "" {
		if resolved, err := adapter.ResolveSession(s.ZellijSession); err == nil {
			return resolved
		}
	}
	if resolved, err := adapter.ResolveSession(s.Repository); err == nil {
		return resolved
	}
	return ""
}

var worktreeSuffix = regexp.MustCompile(`[/-]worktree-.+$`)

var knownRepoCorrections = map[string]string{
	"chaspy/myassistant-server": "chaspy/myassistant",
	"studiuos/jp-Studious-JP":  "studiuos-jp/Studious_JP",
}

func repoFromRepository(repository string) string {
	cleaned := worktreeSuffix.ReplaceAllString(repository, "")
	parts := strings.Split(cleaned, "/")
	var repo string
	if len(parts) >= 2 {
		repo = parts[0] + "/" + parts[1]
	}
	if repo == "" {
		return ""
	}
	if corrected, ok := knownRepoCorrections[repo]; ok {
		return corrected
	}
	return repo
}

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
		db.Exec(
			"UPDATE sessions_archive SET repository = ?, updated_at = CURRENT_TIMESTAMP WHERE repository = ?",
			correct, incorrect)
	}
}

// syncRuntimeStatus updates runtime_status based on zellij session state,
// then enriches CWD/repo/branch for alive sessions with empty CWD via dump-layout.
// Only updates existing alive=1 DB records. Does NOT create new records.
func syncRuntimeStatus(db *sql.DB) {
	zellijSessions, err := listZellijDetailed()
	if err != nil || zellijSessions == nil {
		return
	}

	// Build map: name(lower) -> state
	type zellijState struct {
		name   string
		exited bool
	}
	zellijMap := make(map[string]zellijState)
	for _, zs := range zellijSessions {
		zellijMap[strings.ToLower(zs.Name)] = zellijState{name: zs.Name, exited: zs.Exited}
	}

	// Step 1: Update runtime_status for all alive=1 DB sessions
	aliveSessions, _ := store.ListSessionsByAlive(db, true)
	for _, s := range aliveSessions {
		zellijName := s.ZellijSession
		if zellijName == "" {
			db.Exec("UPDATE sessions SET runtime_status = 'gone', updated_at = CURRENT_TIMESTAMP WHERE id = ?", s.ID)
			continue
		}

		if zs, found := zellijMap[strings.ToLower(zellijName)]; found {
			if zs.exited {
				db.Exec("UPDATE sessions SET runtime_status = 'exited', updated_at = CURRENT_TIMESTAMP WHERE id = ?", s.ID)
			} else {
				db.Exec("UPDATE sessions SET runtime_status = 'running', updated_at = CURRENT_TIMESTAMP WHERE id = ?", s.ID)
			}
		} else {
			db.Exec("UPDATE sessions SET runtime_status = 'gone', updated_at = CURRENT_TIMESTAMP WHERE id = ?", s.ID)
		}
	}

	// Step 2: Enrich CWD/repo/branch for alive sessions with empty CWD via dump-layout
	// Re-fetch to get updated runtime_status
	aliveSessions, _ = store.ListSessionsByAlive(db, true)
	for _, s := range aliveSessions {
		if s.CWD != "" {
			continue // already has CWD
		}
		if s.ZellijSession == "" {
			continue
		}

		cwd := zellijCWD(s.ZellijSession)
		if cwd == "" {
			continue
		}

		updates := []string{"cwd = ?"}
		args := []any{cwd}

		if repo := gitRepoName(cwd); repo != "" {
			updates = append(updates, "repository = ?")
			args = append(args, repo)
		}
		if branch := gitBranchName(cwd); branch != "" {
			updates = append(updates, "git_branch = ?")
			args = append(args, branch)
		}
		updates = append(updates, "updated_at = CURRENT_TIMESTAMP")
		args = append(args, s.ID)
		db.Exec("UPDATE sessions SET "+strings.Join(updates, ", ")+" WHERE id = ?", args...)
		fmt.Printf("Enriched session %s: cwd=%s\n", s.ZellijSession, cwd)
	}
}

// zellijCWD extracts CWD from a zellij session's layout dump.
// Uses `zellij --session <name> action dump-layout` which outputs lines like:
//
//	cwd "/Users/chaspy/go/src/github.com/chaspy/myassistant"
var zellijCWD = func(sessionName string) string {
	cmd := exec.Command("env", "-u", "ZELLIJ", "zellij", "--session", sessionName, "action", "dump-layout")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "cwd \"") {
			return strings.Trim(strings.TrimPrefix(trimmed, "cwd "), "\"")
		}
	}
	return ""
}

// gitRepoName extracts the GitHub "owner/repo" from a CWD by running git remote.
var gitRepoName = func(cwd string) string {
	cmd := exec.Command("git", "-C", cwd, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseGitHubRepo(strings.TrimSpace(string(out)))
}

// gitBranchName returns the current branch for a CWD.
var gitBranchName = func(cwd string) string {
	cmd := exec.Command("git", "-C", cwd, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// parseGitHubRepo extracts "owner/repo" from a git remote URL.
func parseGitHubRepo(remoteURL string) string {
	if strings.Contains(remoteURL, "git@github.com:") {
		part := strings.TrimPrefix(remoteURL, "git@github.com:")
		part = strings.TrimSuffix(part, ".git")
		return part
	}
	if strings.Contains(remoteURL, "github.com/") {
		idx := strings.Index(remoteURL, "github.com/")
		part := remoteURL[idx+len("github.com/"):]
		part = strings.TrimSuffix(part, ".git")
		return part
	}
	return ""
}

var listZellijDetailed = func() ([]mux.ZellijSessionState, error) {
	return mux.ListZellijSessionsDetailed()
}

func lookupPRURL(repo, branch string) string {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--repo", repo, "--json", "url", "--jq", ".[0].url")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
