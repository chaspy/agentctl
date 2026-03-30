package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/chaspy/agentctl/internal/process"
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/session"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	listAgent string
	listHours int
	listSync  bool
	listLive  bool
	listAll   bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List Claude Code / Codex CLI sessions",
	Long:  "By default reads from SQLite. Use --sync to scan JSONL and update DB, or --live for legacy direct scan.",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().StringVar(&listAgent, "agent", "all", "Filter by agent: all, claude, codex")
	listCmd.Flags().IntVar(&listHours, "hours", 24, "Show sessions active within the last N hours")
	listCmd.Flags().BoolVar(&listSync, "sync", false, "Scan JSONL, sync to SQLite, then display from DB")
	listCmd.Flags().BoolVar(&listLive, "live", false, "Legacy mode: scan JSONL directly (no DB)")
	listCmd.Flags().BoolVar(&listAll, "all", false, "Include archived sessions")
}

func runList(cmd *cobra.Command, args []string) error {
	if listSync && listLive {
		return fmt.Errorf("--sync and --live are mutually exclusive")
	}

	if listLive {
		return runListLive()
	}

	if listSync {
		if err := runSyncToDB(); err != nil {
			return err
		}
	}

	return runListFromDB()
}

// runListFromDB displays sessions from SQLite.
func runListFromDB() error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	var sessions []store.Session
	if listAll {
		sessions, err = store.ListAllSessionsWithArchive(db)
	} else {
		sessions, err = store.ListActiveSessions(db)
	}
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions in database. Run 'list --sync' to scan and import sessions.")
		return nil
	}

	// Filter by hours
	cutoff := time.Now().Add(-time.Duration(listHours) * time.Hour)
	var filtered []store.Session
	for _, s := range sessions {
		if s.LastActive.After(cutoff) {
			filtered = append(filtered, s)
		}
	}

	// Filter by agent
	if listAgent != "" && listAgent != "all" {
		var agentFiltered []store.Session
		for _, s := range filtered {
			if s.Agent == listAgent {
				agentFiltered = append(agentFiltered, s)
			}
		}
		filtered = agentFiltered
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tREPOSITORY\tBRANCH\tLAST ACTIVE\tALIVE\tSTATUS\tROLE\tPR\tLAST MESSAGE")

	for _, s := range filtered {
		age := formatAge(time.Since(s.LastActive))
		msg := s.LastMessage
		if msg == "" {
			msg = "-"
		}
		branch := s.GitBranch
		if branch == "" {
			branch = "-"
		}
		alive := "no"
		if s.Alive {
			alive = "yes"
		}
		role := s.Role
		if role == "" {
			role = "worker"
		}
		status := s.Status
		if s.BlockedReason != "" {
			status = s.Status + "(" + s.BlockedReason + ")"
		}
		if s.IsLoop {
			status = status + " 🔁"
		}
		pr := s.PRURL
		if pr == "" {
			pr = "-"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Agent,
			s.Repository,
			branch,
			age,
			alive,
			status,
			role,
			pr,
			msg,
		)
	}

	return w.Flush()
}

// runSyncToDB scans JSONL sessions and syncs them to SQLite.
func runSyncToDB() error {
	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	agents, err := selectedAgents(listAgent)
	if err != nil {
		return err
	}

	maxAge := time.Duration(listHours) * time.Hour
	var sessions []provider.SessionInfo
	for _, agent := range agents {
		switch agent {
		case provider.AgentClaude:
			items, err := provider.ScanClaudeSessions(maxAge)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not scan claude sessions: %v\n", err)
				continue
			}
			sessions = append(sessions, items...)
		case provider.AgentCodex:
			items, err := provider.ScanCodexSessions(maxAge)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not scan codex sessions: %v\n", err)
				continue
			}
			sessions = append(sessions, items...)
		}
	}

	claudeProcs, _ := process.FindClaudeProcesses()
	codexProcs, _ := process.FindCodexProcesses()

	// Build set of CWDs marked as loop sessions
	allState, _ := store.AllState(db)
	loopCWDs := make(map[string]bool)
	for k, v := range allState {
		if len(k) > 9 && k[:9] == "loop:cwd:" && v == "1" {
			loopCWDs[k[9:]] = true
		}
	}

	var scannedIDs []string
	for _, s := range sessions {
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
		blockedReason := ""
		if status == session.StatusBlocked {
			blockedReason = session.DetectBlockedReason(statusMsg)
		}

		// Role is "worker" by default; manager role is preserved in DB via UpsertSession.
		role := "worker"

		id := fmt.Sprintf("%s:%s:%s", s.Agent, s.Repository, s.SessionID)
		scannedIDs = append(scannedIDs, id)

		_ = store.UpsertSession(db, &store.Session{
			ID:            id,
			Agent:         string(s.Agent),
			Repository:    s.Repository,
			SessionID:     s.SessionID,
			CWD:           s.CWD,
			GitBranch:     s.GitBranch,
			Status:        status,
			BlockedReason: blockedReason,
			Alive:         alive,
			LastMessage:   s.LastMessage,
			LastRole:      s.LastRole,
			LastActive:    s.ModTime,
			Role:          role,
			IsLoop:        loopCWDs[s.CWD],
		})
	}

	// Mark sessions in DB but not found in scan as dead
	_ = store.MarkStaleSessionsDead(db, scannedIDs)

	// Fetch PR URLs for sessions that don't have one yet
	for _, s := range sessions {
		id := fmt.Sprintf("%s:%s:%s", s.Agent, s.Repository, s.SessionID)
		if s.GitBranch == "" || s.GitBranch == "main" || s.GitBranch == "master" {
			continue
		}
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

	fmt.Fprintf(os.Stderr, "Synced %d sessions to database\n", len(sessions))
	return nil
}

// runListLive is the legacy mode: scan JSONL directly and display without DB.
func runListLive() error {
	agents, err := selectedAgents(listAgent)
	if err != nil {
		return err
	}

	maxAge := time.Duration(listHours) * time.Hour
	var sessions []provider.SessionInfo
	for _, agent := range agents {
		switch agent {
		case provider.AgentClaude:
			items, err := provider.ScanClaudeSessions(maxAge)
			if err != nil {
				if len(agents) == 1 {
					return fmt.Errorf("listing claude sessions: %w", err)
				}
				fmt.Fprintf(os.Stderr, "warning: could not list claude sessions: %v\n", err)
				continue
			}
			sessions = append(sessions, items...)
		case provider.AgentCodex:
			items, err := provider.ScanCodexSessions(maxAge)
			if err != nil {
				if len(agents) == 1 {
					return fmt.Errorf("listing codex sessions: %w", err)
				}
				fmt.Fprintf(os.Stderr, "warning: could not list codex sessions: %v\n", err)
				continue
			}
			sessions = append(sessions, items...)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	claudeProcs, _ := process.FindClaudeProcesses()
	codexProcs, _ := process.FindCodexProcesses()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tREPOSITORY\tBRANCH\tLAST ACTIVE\tALIVE\tSTATUS\tLAST MESSAGE")

	for _, s := range sessions {
		age := formatAge(time.Since(s.ModTime))
		msg := s.LastMessage
		if msg == "" {
			msg = "-"
		}
		branch := s.GitBranch
		if branch == "" {
			branch = "-"
		}

		alive := "no"
		switch s.Agent {
		case provider.AgentClaude:
			if process.IsAliveForCWD(claudeProcs, s.CWD) {
				alive = "yes"
			}
		case provider.AgentCodex:
			if process.IsAliveForCWD(codexProcs, s.CWD) {
				alive = "yes"
			}
		}

		statusMsg := s.LastFullMessage
		if statusMsg == "" {
			statusMsg = s.LastMessage
		}
		status := session.DetectStatus(statusMsg, s.LastRole, alive == "yes", s.ErrorType, s.IsAPIError)
		if status == session.StatusBlocked {
			if reason := session.DetectBlockedReason(statusMsg); reason != "" {
				status = status + "(" + reason + ")"
			}
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Agent,
			s.Repository,
			branch,
			age,
			alive,
			status,
			msg,
		)
	}

	return w.Flush()
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
