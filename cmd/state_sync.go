package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
//  1. Update runtime_status from zellij sessions and discover missing sessions
//  2. Enrich CWD/repo/branch for alive sessions with empty CWD via dump-layout
//  3. Read JSONL to update LastMessage for alive sessions with CWD
//
// DB remains the source of truth for alive/dead management, but sync may
// deterministically discover missing zellij sessions from runtime metadata.
func syncSessionsToDB(db *sql.DB, agentFilter string, hours int, regenerateSummaries bool) (int, error) {
	// ── Step 1+2: Runtime Status + dump-layout enrichment ──
	discovered, err := syncRuntimeStatus(db)
	if err != nil {
		return 0, err
	}

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
		status := session.DetectStatus(statusMsg, jsonl.LastRole, dbSess.WantsRunning(), jsonl.ErrorType, jsonl.IsAPIError)

		role := dbSess.Role
		if role == "" {
			role = "worker"
		}
		if managerName != "" && dbSess.Repository == "agentctl" {
			role = "manager"
		}

		// UPDATE only — never creates new records
		_ = store.UpdateSessionMetadata(db, &store.Session{
			ID:          dbSess.ID,
			Status:      status,
			GitBranch:   jsonl.GitBranch,
			LastMessage: jsonl.LastMessage,
			LastRole:    jsonl.LastRole,
			LastActive:  jsonl.ModTime,
			Role:        role,
			IsLoop:      loopCWDs[dbSess.CWD],
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

	prMetadataBySessionID, err := syncSessionPRMetadata(db)
	if err != nil {
		return 0, err
	}

	// Auto-archive dead/error sessions to sessions_archive table
	if archived, err := store.ArchiveDeadSessions(db); err == nil && archived > 0 {
		fmt.Printf("Auto-archived %d dead/error session(s)\n", archived)
	}

	if err := syncAgentTaskOutcomes(db, prMetadataBySessionID); err != nil {
		return 0, err
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

	if err := backupDatabaseFile(db, store.DefaultDBPath()); err != nil {
		return 0, err
	}

	return discovered + enriched, nil
}

type prMetadata struct {
	URL        string `json:"url"`
	State      string `json:"state"`
	HeadRefOID string `json:"headRefOid"`
}

func syncSessionPRMetadata(db *sql.DB) (map[string]prMetadata, error) {
	sessions, err := store.ListSessions(db)
	if err != nil {
		return nil, fmt.Errorf("listing sessions for pr metadata sync: %w", err)
	}

	metadataBySessionID := make(map[string]prMetadata)
	for _, s := range sessions {
		repo := repoFromRepository(s.Repository)
		if repo == "" {
			continue
		}

		prURL := s.PRURL
		prNumber := s.PRNumber
		prState := s.PRState

		if prURL == "" {
			if s.GitBranch == "" || s.GitBranch == "main" || s.GitBranch == "master" {
				continue
			}
			noPRKey := "no_pr_checked:" + repo + ":" + s.GitBranch
			if lastChecked, _ := store.GetState(db, noPRKey); lastChecked != "" {
				if t, err := time.Parse(time.RFC3339, lastChecked); err == nil && time.Since(t) < 5*time.Minute {
					continue
				}
			}

			prURL = lookupPRURL(repo, s.GitBranch)
			if prURL == "" {
				_ = store.SetState(db, noPRKey, time.Now().Format(time.RFC3339))
				continue
			}
		}

		if prNumber == 0 {
			if n, err := strconv.Atoi(extractPRNumber(prURL)); err == nil {
				prNumber = n
			}
		}

		meta := prMetadata{URL: prURL, State: prState}
		if prNumber > 0 {
			fetched := lookupPRMetadata(repo, strconv.Itoa(prNumber))
			if fetched.URL != "" {
				meta.URL = fetched.URL
			}
			if fetched.State != "" {
				meta.State = fetched.State
			}
			if fetched.HeadRefOID != "" {
				meta.HeadRefOID = fetched.HeadRefOID
			}
		}

		if meta.URL == "" {
			continue
		}
		if err := updateSessionPRTracking(db, s.ID, prNumber, meta.URL, meta.State); err != nil {
			return nil, fmt.Errorf("updating pr metadata for session %s: %w", s.ID, err)
		}
		metadataBySessionID[s.ID] = meta
	}

	return metadataBySessionID, nil
}

func syncAgentTaskOutcomes(db *sql.DB, prMetadataBySessionID map[string]prMetadata) error {
	existingOutcomes, err := store.ListAgentTaskOutcomes(db)
	if err != nil {
		return fmt.Errorf("listing outcomes for outcome sync: %w", err)
	}
	prMetadataCache := make(map[string]prMetadata)
	for i := range existingOutcomes {
		if !shouldRefreshOutcomePRMetadata(&existingOutcomes[i]) {
			continue
		}
		meta := resolveOutcomePRMetadata(&existingOutcomes[i], prMetadataBySessionID, prMetadataCache)
		if meta.URL == "" && meta.State == "" && meta.HeadRefOID == "" {
			continue
		}
		if !applyOutcomePRMetadata(&existingOutcomes[i], meta) {
			continue
		}
		if err := store.UpdateAgentTaskOutcome(db, &existingOutcomes[i]); err != nil {
			return fmt.Errorf("updating outcome #%d metadata: %w", existingOutcomes[i].ID, err)
		}
		if err := store.LogAction(db, &store.Action{
			SessionID:   existingOutcomes[i].ManagedSessionID,
			ActionType:  "outcome_refresh",
			Content:     fmt.Sprintf("Refreshed outcome #%d metadata for %s", existingOutcomes[i].ID, existingOutcomes[i].AgentTaskName),
			Result:      firstNonEmpty(existingOutcomes[i].PRState, existingOutcomes[i].PRURL, existingOutcomes[i].Status),
			RouteReason: "",
		}); err != nil {
			return fmt.Errorf("logging outcome refresh for outcome #%d: %w", existingOutcomes[i].ID, err)
		}
	}

	attempts, err := store.ListAgentTaskAttempts(db)
	if err != nil {
		return fmt.Errorf("listing attempts for outcome sync: %w", err)
	}

	for _, attempt := range attempts {
		existing, err := store.GetAgentTaskOutcomeByAttemptID(db, attempt.ID)
		if err != nil {
			return fmt.Errorf("checking outcome for attempt %d: %w", attempt.ID, err)
		}
		if existing != nil {
			continue
		}

		status, failureCategory, failureReason := inferOutcomeForAttemptAutoSync(db, &attempt)
		if status == "" {
			continue
		}

		plan, err := buildAgentTaskOutcomePlan(
			db,
			&attempt,
			status,
			"",
			0,
			"",
			"",
			"",
			failureCategory,
			failureReason,
			"sync",
		)
		if err != nil {
			return fmt.Errorf("building auto outcome for attempt %d: %w", attempt.ID, err)
		}
		meta := resolveOutcomePRMetadata(plan.Outcome, prMetadataBySessionID, prMetadataCache)
		applyOutcomePRMetadata(plan.Outcome, meta)

		if err := store.CreateAgentTaskOutcome(db, plan.Outcome); err != nil {
			return fmt.Errorf("creating auto outcome for attempt %d: %w", attempt.ID, err)
		}
		if err := store.UpdateAgentTaskDecisionStatus(db, attempt.DecisionID, plan.Outcome.Status); err != nil {
			return fmt.Errorf("marking decision %d %s: %w", attempt.DecisionID, plan.Outcome.Status, err)
		}
		if err := store.UpdateAgentTaskStatus(db, attempt.AgentTaskName, plan.Outcome.Status); err != nil {
			return fmt.Errorf("marking agent task %s %s: %w", attempt.AgentTaskName, plan.Outcome.Status, err)
		}

		routeReason := ""
		decision, err := store.GetAgentTaskDecision(db, attempt.DecisionID)
		if err == nil && decision != nil {
			routeReason = decision.RouteReason
		}
		result := plan.Outcome.Status
		if plan.Outcome.PRURL != "" {
			result = plan.Outcome.PRURL
		}
		if err := store.LogAction(db, &store.Action{
			SessionID:   plan.Outcome.ManagedSessionID,
			ActionType:  "outcome_sync",
			Content:     fmt.Sprintf("Auto-recorded outcome #%d from attempt #%d for %s", plan.Outcome.ID, attempt.ID, attempt.AgentTaskName),
			Result:      result,
			RouteReason: routeReason,
		}); err != nil {
			return fmt.Errorf("logging auto outcome for attempt %d: %w", attempt.ID, err)
		}
	}

	return nil
}

func shouldRefreshOutcomePRMetadata(outcome *store.AgentTaskOutcome) bool {
	hasPRReference := outcome.PRURL != "" || (outcome.PRNumber != 0 && repoFromRepository(outcome.Repository) != "")
	if !hasPRReference {
		return false
	}
	if outcome.PRURL == "" || outcome.PRState == "" || outcome.PRState == "OPEN" {
		return true
	}
	return outcome.CommitSHA == ""
}

func resolveOutcomePRMetadata(outcome *store.AgentTaskOutcome, prMetadataBySessionID map[string]prMetadata, cache map[string]prMetadata) prMetadata {
	if outcome.ManagedSessionID != "" {
		if meta, ok := prMetadataBySessionID[outcome.ManagedSessionID]; ok {
			return meta
		}
	}

	repo := repoFromRepository(outcome.Repository)
	if repo == "" {
		return prMetadata{}
	}
	prNumber := outcome.PRNumber
	if prNumber == 0 {
		if n, err := strconv.Atoi(extractPRNumber(outcome.PRURL)); err == nil {
			prNumber = n
		}
	}
	if prNumber == 0 {
		return prMetadata{}
	}

	cacheKey := repo + "#" + strconv.Itoa(prNumber)
	if meta, ok := cache[cacheKey]; ok {
		return meta
	}
	meta := lookupPRMetadata(repo, strconv.Itoa(prNumber))
	cache[cacheKey] = meta
	return meta
}

func applyOutcomePRMetadata(outcome *store.AgentTaskOutcome, meta prMetadata) bool {
	changed := false
	if meta.URL != "" && outcome.PRURL != meta.URL {
		outcome.PRURL = meta.URL
		changed = true
	}
	if meta.State != "" && outcome.PRState != meta.State {
		outcome.PRState = meta.State
		changed = true
	}
	if meta.HeadRefOID != "" && outcome.CommitSHA != meta.HeadRefOID {
		outcome.CommitSHA = meta.HeadRefOID
		changed = true
	}
	return changed
}

func inferOutcomeForAttemptAutoSync(db *sql.DB, attempt *store.AgentTaskAttempt) (status, failureCategory, failureReason string) {
	switch attempt.Status {
	case "failed":
		return "failed", "spawn_failed", attempt.FailureReason
	case "cancelled":
		return "cancelled", "", attempt.FailureReason
	}

	if attempt.ManagedSessionID == "" {
		return "", "", ""
	}
	session, err := store.GetSessionAny(db, attempt.ManagedSessionID)
	if err != nil || session == nil {
		return "", "", ""
	}

	if session.WantsRunning() {
		return "", "", ""
	}
	if session.PRURL != "" {
		return "completed", "", ""
	}
	switch session.Status {
	case "error":
		return "failed", "session_error", firstNonEmpty(session.LastMessage, "session ended with error status before recording PR")
	case "dead":
		return "failed", "session_dead", "session ended before recording PR"
	default:
		return "cancelled", "", "session stopped before recording PR"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func updateSessionPRTracking(db *sql.DB, sessionID string, prNumber int, prURL, prState string) error {
	_, err := db.Exec(`UPDATE sessions
		SET pr_number = ?, pr_url = ?, pr_state = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		prNumber, prURL, prState, sessionID)
	return err
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
	"studiuos/jp-Studious-JP":   "studiuos-jp/Studious_JP",
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

type discoveredSession struct {
	Session store.Session
}

// syncRuntimeStatus updates runtime_status based on zellij session state,
// discovers missing DB records from zellij, then enriches CWD/repo/branch for
// alive sessions with empty CWD via dump-layout.
func syncRuntimeStatus(db *sql.DB) (int, error) {
	zellijSessions, err := listZellijDetailed()
	if err != nil {
		return 0, nil
	}
	if zellijSessions == nil {
		zellijSessions = []mux.ZellijSessionState{}
	}

	// Fail-safe: if zellij returned 0 sessions but DB has alive sessions,
	// zellij may be in a broken state — skip dead-session detection entirely.
	if len(zellijSessions) == 0 {
		aliveSessions, _ := store.ListSessionsByAlive(db, true)
		if len(aliveSessions) > 0 {
			fmt.Fprintf(os.Stderr, "warning: zellij returned 0 sessions but DB has %d alive sessions, skipping dead-session detection\n", len(aliveSessions))
			return 0, nil
		}
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

	// Step 1: Update runtime_status for all desired_state=running DB sessions
	aliveSessions, _ := store.ListSessionsByAlive(db, true)
	existingAlive := make(map[string]store.Session, len(aliveSessions))
	for _, s := range aliveSessions {
		existingAlive[strings.ToLower(s.ZellijSession)] = s

		// Skip dead-detection for sessions that are in transition: spawning (zellij not yet
		// started) or killing (zellij session being torn down). Marking these as 'gone'
		// would be a false positive.
		if s.LifecycleState == store.LifecycleStateSpawning || s.LifecycleState == store.LifecycleStateKilling {
			continue
		}

		zellijName := s.ZellijSession
		if zellijName == "" {
			// zellij_session empty means "location unknown", not "dead" — preserve desired_state, mark unknown.
			db.Exec("UPDATE sessions SET runtime_status = 'unknown', updated_at = CURRENT_TIMESTAMP WHERE id = ?", s.ID)
			continue
		}

		if zs, found := zellijMap[strings.ToLower(zellijName)]; found {
			if zs.exited {
				db.Exec("UPDATE sessions SET runtime_status = 'exited', updated_at = CURRENT_TIMESTAMP WHERE id = ?", s.ID)
			} else {
				db.Exec("UPDATE sessions SET runtime_status = 'running', updated_at = CURRENT_TIMESTAMP WHERE id = ?", s.ID)
				_, _ = store.TouchSessionLastSeenAliveByZellijSession(db, s.ZellijSession, time.Now())
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

	return 0, nil
}

// zellijCWD extracts CWD from a zellij session's layout dump.
// Uses `zellij --session <name> action dump-layout` which outputs lines like:
//
//	cwd "/Users/chaspy/go/src/github.com/chaspy/myassistant"
var zellijCWD = func(sessionName string) string {
	out, err := zellijDumpLayout(sessionName)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "cwd \"") {
			return strings.Trim(strings.TrimPrefix(trimmed, "cwd "), "\"")
		}
	}
	return ""
}

var zellijDumpLayout = func(sessionName string) (string, error) {
	cmd := exec.Command("env", "-u", "ZELLIJ", "zellij", "--session", sessionName, "action", "dump-layout")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
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

var lookupPRURL = func(repo, branch string) string {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--repo", repo, "--json", "url", "--jq", ".[0].url")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var lookupPRMetadata = func(repo, prNumber string) prMetadata {
	cmd := exec.Command("gh", "pr", "view", prNumber, "--repo", repo, "--json", "url,state,headRefOid")
	out, err := cmd.Output()
	if err != nil {
		return prMetadata{}
	}
	var result prMetadata
	if err := json.Unmarshal(out, &result); err != nil {
		return prMetadata{}
	}
	return result
}

func discoverSessionFromZellij(zs mux.ZellijSessionState) (discoveredSession, bool) {
	layout, err := zellijDumpLayout(zs.Name)
	if err != nil {
		return discoveredSession{}, false
	}

	cwd := parseZellijLayoutCWD(layout)
	if cwd == "" || strings.Contains(cwd, "worktree-preview-") {
		return discoveredSession{}, false
	}

	agent := inferAgentFromLayout(layout, zs.Name)
	repo := gitRepoName(cwd)
	branch := gitBranchName(cwd)
	if repo == "" {
		repo = inferRepositoryFromCWD(cwd)
	}

	status := "idle"
	runtimeStatus := "running"
	if zs.Exited {
		runtimeStatus = "exited"
	}

	sessionID := fmt.Sprintf("%s:%s:zellij-%s", agent, repo, zs.Name)
	if repo == "" {
		sessionID = fmt.Sprintf("%s::zellij-%s", agent, zs.Name)
	}

	return discoveredSession{
		Session: store.Session{
			ID:              sessionID,
			Agent:           string(agent),
			Repository:      repo,
			SessionID:       "zellij-" + zs.Name,
			CWD:             cwd,
			GitBranch:       branch,
			ZellijSession:   zs.Name,
			Status:          status,
			DesiredState:    store.DesiredStateRunning,
			LastActive:      time.Now(),
			LastSeenAliveAt: time.Now(),
			Role:            "worker",
			RuntimeStatus:   runtimeStatus,
		},
	}, true
}

func parseZellijLayoutCWD(layout string) string {
	for _, line := range strings.Split(layout, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "cwd \"") {
			return strings.Trim(strings.TrimPrefix(trimmed, "cwd "), "\"")
		}
	}
	return ""
}

func inferAgentFromLayout(layout, sessionName string) provider.Agent {
	lower := strings.ToLower(layout)
	switch {
	case strings.Contains(lower, `command="claude"`),
		strings.Contains(lower, `/claude"`),
		strings.Contains(lower, `args "claude"`):
		return provider.AgentClaude
	case strings.Contains(lower, `command="codex"`),
		strings.Contains(lower, `/codex"`),
		strings.Contains(lower, `args "codex"`):
		return provider.AgentCodex
	case strings.Contains(strings.ToLower(sessionName), "codex"):
		return provider.AgentCodex
	default:
		return provider.AgentClaude
	}
}

func inferRepositoryFromCWD(cwd string) string {
	cwd = filepath.Clean(cwd)
	parts := strings.Split(cwd, string(filepath.Separator))
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return filepath.Base(cwd)
}

func backupDatabaseFile(db *sql.DB, path string) error {
	if path == "" || path == ":memory:" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("checkpointing database before backup: %w", err)
	}
	dstPath := path + ".bak"
	return copyDatabaseFile(path, dstPath)
}
