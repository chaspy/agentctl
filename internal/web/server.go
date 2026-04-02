package web

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/process"
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/session"
	"github.com/chaspy/agentctl/internal/store"
)

//go:embed static/*
var staticFiles embed.FS

// Server serves the PWA dashboard and API endpoints.
type Server struct {
	db *sql.DB
}

// New creates a new Server with the given database connection.
func New(db *sql.DB) *Server {
	return &Server{db: db}
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/summary", s.handleSessionSummary)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/api/actions", s.handleActions)
	mux.HandleFunc("/api/rate", s.handleRate)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/sync", s.handleSync)
	mux.HandleFunc("/api/resume", s.handleResume)
	mux.HandleFunc("/api/sessions/messages", s.handleSessionMessages)

	// Static files (PWA)
	staticFS, _ := fs.Sub(staticFiles, "static")
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	return mux
}

type sessionJSON struct {
	ID            string `json:"id"`
	Agent         string `json:"agent"`
	Repository    string `json:"repository"`
	GitBranch     string `json:"git_branch"`
	Status        string `json:"status"`
	Alive         bool   `json:"alive"`
	LastMessage   string `json:"last_message"`
	LastActive    string `json:"last_active"`
	TaskSummary   string `json:"task_summary"`
	Role          string `json:"role"`
	Archived      bool   `json:"archived"`
	PRNumber      int    `json:"pr_number,omitempty"`
	PRURL         string `json:"pr_url,omitempty"`
	PRState       string `json:"pr_state,omitempty"`
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	showAll := r.URL.Query().Get("all") == "true"

	var sessions []store.Session
	var err error
	if showAll {
		sessions, err = store.ListAllSessionsWithArchive(s.db)
	} else {
		sessions, err = store.ListActiveSessions(s.db)
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	out := make([]sessionJSON, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, sessionJSON{
			ID:          sess.ID,
			Agent:       sess.Agent,
			Repository:  sess.Repository,
			GitBranch:   sess.GitBranch,
			Status:      sess.Status,
			Alive:       sess.Alive,
			LastMessage: sess.LastMessage,
			LastActive:  sess.LastActive.Format(time.RFC3339),
			TaskSummary: sess.TaskSummary,
			Role:        sess.Role,
			Archived:    sess.Archived,
			PRNumber:    sess.PRNumber,
			PRURL:       sess.PRURL,
			PRState:     sess.PRState,
		})
	}
	writeJSON(w, out)
}

type summaryRequest struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

func (s *Server) handleSessionSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req summaryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "id and summary required", http.StatusBadRequest)
		return
	}

	_, err := s.db.Exec("UPDATE sessions SET task_summary = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", req.Summary, req.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	writeJSON(w, map[string]string{"ok": "updated"})
}

type taskJSON struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	Description string `json:"description"`
	Status      string `json:"status"`
	AssignedAt  string `json:"assigned_at"`
	Result      string `json:"result,omitempty"`
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := store.GetActiveTasks(s.db)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	out := make([]taskJSON, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskJSON{
			ID:          t.ID,
			SessionID:   t.SessionID,
			Description: t.Description,
			Status:      t.Status,
			AssignedAt:  t.AssignedAt.Format(time.RFC3339),
			Result:      t.Result,
		})
	}
	writeJSON(w, out)
}

type actionJSON struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"session_id"`
	ActionType string `json:"action_type"`
	Content    string `json:"content"`
	Result     string `json:"result,omitempty"`
	CreatedAt  string `json:"created_at"`
}

func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	actions, err := store.GetRecentActions(s.db, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	out := make([]actionJSON, 0, len(actions))
	for _, a := range actions {
		out = append(out, actionJSON{
			ID:         a.ID,
			SessionID:  a.SessionID,
			ActionType: a.ActionType,
			Content:    a.Content,
			Result:     a.Result,
			CreatedAt:  a.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, out)
}

type rateJSON struct {
	Agent   string `json:"agent"`
	Percent int    `json:"percent"` // remaining capacity (0-100), -1 = unknown
	Detail  string `json:"detail"`
	Resets  string `json:"resets,omitempty"` // reset time like "02:00"
}

func (s *Server) handleRate(w http.ResponseWriter, r *http.Request) {
	var rates []rateJSON

	claudeRate, err := provider.ClaudeRate()
	if err == nil {
		rates = append(rates, buildRateJSON("claude", claudeRate))
	}

	codexRate, err := provider.CodexRate()
	if err == nil {
		rates = append(rates, buildRateJSON("codex", codexRate))
	}

	writeJSON(w, rates)
}

func buildRateJSON(agent string, info provider.RateInfo) rateJSON {
	r := rateJSON{
		Agent:   agent,
		Percent: info.RemainingPct,
	}

	// Build a short, useful detail string
	summary := info.Summary
	if info.RemainingPct >= 0 {
		r.Detail = fmt.Sprintf("%d%% left", info.RemainingPct)
	} else {
		// Use the summary (e.g. "allowed ($18.6 spent, window ends 02:00 ...)")
		// instead of just "unknown"
		r.Detail = summary
	}

	// Extract reset time if present in summary
	if idx := strings.Index(summary, "window ends "); idx >= 0 {
		rest := summary[idx+len("window ends "):]
		if len(rest) >= 5 {
			r.Resets = rest[:5] // "02:00"
		}
	} else if idx := strings.Index(summary, "resets "); idx >= 0 {
		rest := summary[idx+len("resets "):]
		if len(rest) >= 5 {
			r.Resets = rest[:5]
		}
	}

	// Add burn rate from BurnRate field
	if info.BurnRate != "" && info.BurnRate != "-" {
		r.Detail += " | " + info.BurnRate
	}

	return r
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	state, err := store.AllState(s.db)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, state)
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	// ── Stage 1: Zellij Truth ──
	// Mark DB sessions dead if their zellij session no longer exists.
	muxSet := buildWebMuxSessionSet()
	if muxSet != nil {
		aliveSessions, _ := store.ListSessionsByAlive(s.db, true)
		for _, sess := range aliveSessions {
			if sess.ZellijSession == "" {
				s.db.Exec("UPDATE sessions SET alive = 0, status = 'dead', updated_at = CURRENT_TIMESTAMP WHERE id = ?", sess.ID)
				continue
			}
			if !muxSet[strings.ToLower(sess.ZellijSession)] {
				s.db.Exec("UPDATE sessions SET alive = 0, status = 'dead', updated_at = CURRENT_TIMESTAMP WHERE id = ?", sess.ID)
			}
		}
	}

	// ── Stage 2: JSONL Enrichment ──
	// Only enrich existing DB sessions — do NOT create new sessions from JSONL.
	maxAge := 24 * time.Hour
	sessions, _ := provider.ScanClaudeSessions(maxAge)
	codexSessions, _ := provider.ScanCodexSessions(maxAge)
	sessions = append(sessions, codexSessions...)

	claudeProcs, _ := process.FindClaudeProcesses()
	codexProcs, _ := process.FindCodexProcesses()

	// Build CWD -> JSONL index
	jsonlByCWD := make(map[string]provider.SessionInfo)
	for _, sess := range sessions {
		if sess.CWD != "" {
			// Keep most recent per CWD
			if existing, ok := jsonlByCWD[sess.CWD]; !ok || sess.ModTime.After(existing.ModTime) {
				jsonlByCWD[sess.CWD] = sess
			}
		}
	}

	// Enrich alive DB sessions with JSONL metadata
	aliveSessions, _ := store.ListSessionsByAlive(s.db, true)
	var count int
	for _, dbSess := range aliveSessions {
		jsonl, found := jsonlByCWD[dbSess.CWD]
		if !found {
			continue
		}

		alive := false
		switch provider.Agent(dbSess.Agent) {
		case provider.AgentClaude:
			alive = process.IsAliveForCWD(claudeProcs, dbSess.CWD)
		case provider.AgentCodex:
			alive = process.IsAliveForCWD(codexProcs, dbSess.CWD)
		}

		// Validate alive against mux
		zellijSession := dbSess.ZellijSession
		if alive && muxSet != nil {
			if zellijSession == "" || !muxSet[strings.ToLower(zellijSession)] {
				alive = false
			}
		} else if alive && muxSet == nil {
			alive = false
		}

		statusMsg := jsonl.LastFullMessage
		if statusMsg == "" {
			statusMsg = jsonl.LastMessage
		}
		status := session.DetectStatus(statusMsg, jsonl.LastRole, alive, jsonl.ErrorType, jsonl.IsAPIError)

		role := dbSess.Role
		if role == "" {
			role = "worker"
		}

		_ = store.UpsertSession(s.db, &store.Session{
			ID:            dbSess.ID,
			Agent:         dbSess.Agent,
			Repository:    dbSess.Repository,
			SessionID:     dbSess.SessionID,
			CWD:           dbSess.CWD,
			GitBranch:     jsonl.GitBranch,
			ZellijSession: zellijSession,
			Status:        status,
			Alive:         alive,
			LastMessage:   jsonl.LastMessage,
			LastRole:      jsonl.LastRole,
			LastActive:    jsonl.ModTime,
			Role:          role,
		})
		count++
	}

	writeJSON(w, map[string]int{"synced": count})
}

type resumeRequest struct {
	SessionID string `json:"session_id"`
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var req resumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}

	// Find the session in scanned data
	maxAge := 7 * 24 * time.Hour
	sessions, err := provider.ScanClaudeSessions(maxAge)
	if err != nil {
		http.Error(w, "scan error: "+err.Error(), 500)
		return
	}

	var target *provider.SessionInfo
	for i := range sessions {
		if sessions[i].SessionID == req.SessionID {
			target = &sessions[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Build zellij session name
	sessionName := fmt.Sprintf("resume-%s", req.SessionID[:8])

	// Check if zellij session already exists
	existing, _ := exec.Command("env", "-u", "ZELLIJ", "zellij", "list-sessions", "--short").Output()
	for _, line := range splitLines(string(existing)) {
		if line == sessionName {
			writeJSON(w, map[string]string{"error": "zellij session already exists", "session": sessionName})
			return
		}
	}

	// Run the resume command in background
	cmd := exec.Command("go", "run", ".", "resume", req.SessionID, "--name", sessionName)
	cmd.Dir = findProjectRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		http.Error(w, fmt.Sprintf("resume failed: %s\n%s", err, string(out)), 500)
		return
	}

	_ = store.LogAction(s.db, &store.Action{
		SessionID:  sessionName,
		ActionType: "resume",
		Content:    fmt.Sprintf("Resumed session %s via dashboard", req.SessionID),
	})

	writeJSON(w, map[string]string{"ok": "resumed", "session": sessionName})
}

func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	compositeID := r.URL.Query().Get("id")
	if compositeID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	// Extract UUID from composite ID (format: "agent:project:uuid")
	parts := strings.SplitN(compositeID, ":", 3)
	if len(parts) < 3 {
		http.Error(w, "invalid id format", http.StatusBadRequest)
		return
	}
	uuid := parts[2]

	// Scan sessions to find the JSONL file path
	maxAge := 7 * 24 * time.Hour
	sessions, _ := provider.ScanClaudeSessions(maxAge)
	codexSessions, _ := provider.ScanCodexSessions(maxAge)
	sessions = append(sessions, codexSessions...)

	var filePath string
	for _, sess := range sessions {
		if sess.SessionID == uuid {
			filePath = sess.FilePath
			break
		}
	}

	if filePath == "" {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	messages, err := session.RecentMessages(filePath, limit)
	if err != nil {
		http.Error(w, "read error: "+err.Error(), 500)
		return
	}

	writeJSON(w, messages)
}

// buildWebMuxSessionSet returns a set of lowercased mux session names.
func buildWebMuxSessionSet() map[string]bool {
	out, err := exec.Command("env", "-u", "ZELLIJ", "zellij", "list-sessions", "--short").Output()
	if err != nil {
		return nil
	}
	lines := splitLines(string(out))
	if len(lines) == 0 {
		return nil
	}
	set := make(map[string]bool, len(lines))
	for _, l := range lines {
		set[strings.ToLower(l)] = true
	}
	return set
}

// inferWebZellijSession derives the expected zellij session name from a CWD path.
func inferWebZellijSession(cwd string, muxSet map[string]bool) string {
	if cwd == "" || muxSet == nil {
		return ""
	}
	parts := strings.Split(strings.TrimRight(cwd, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	lastPart := parts[len(parts)-1]
	if strings.HasPrefix(lastPart, "worktree-") && len(parts) >= 2 {
		repoBase := parts[len(parts)-2]
		branch := strings.TrimPrefix(lastPart, "worktree-")
		candidate := repoBase + "-" + branch
		if muxSet[strings.ToLower(candidate)] {
			return candidate
		}
	}
	if muxSet[strings.ToLower(lastPart)] {
		return lastPart
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range splitByNewline(s) {
		t := trimSpace(line)
		if t != "" {
			lines = append(lines, t)
		}
	}
	return lines
}

func splitByNewline(s string) []string {
	result := make([]string, 0)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r' || s[j-1] == '\n') {
		j--
	}
	return s[i:j]
}

func findProjectRoot() string {
	// The binary should be running from the project directory
	cmd := exec.Command("go", "env", "GOMOD")
	out, err := cmd.Output()
	if err != nil {
		return "."
	}
	mod := trimSpace(string(out))
	if mod == "" {
		return "."
	}
	// go.mod path -> directory
	for i := len(mod) - 1; i >= 0; i-- {
		if mod[i] == '/' {
			return mod[:i]
		}
	}
	return "."
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(v)
}
