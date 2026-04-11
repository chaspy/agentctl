package cmd

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	adoptZellijAgent      string
	adoptZellijBranch     string
	adoptZellijRepository string
	adoptZellijProtected  bool
	adoptZellijSessionID  string
	adoptZellijOpenDB     = store.Open
	adoptZellijList       = listZellijSessionsShort
	adoptZellijLayout     = zellijDumpLayout
)

var adoptZellijCmd = &cobra.Command{
	Use:   "adopt-zellij <zellij-session>",
	Short: "Register an existing zellij session in the database",
	Long: `Inspects an already-running zellij session and registers it in the database
without restarting or recreating it.

Examples:
  agentctl adopt-zellij atama-chrome-protected --protected
  agentctl adopt-zellij atama-local-app-protected --protected --agent codex`,
	Args: cobra.ExactArgs(1),
	RunE: runAdoptZellij,
}

func init() {
	rootCmd.AddCommand(adoptZellijCmd)
	adoptZellijCmd.Flags().StringVar(&adoptZellijAgent, "agent", "auto", "Agent type: auto, claude, codex")
	adoptZellijCmd.Flags().StringVar(&adoptZellijRepository, "repository", "", "Override repository (owner/repo)")
	adoptZellijCmd.Flags().StringVar(&adoptZellijBranch, "branch", "", "Override git branch")
	adoptZellijCmd.Flags().StringVar(&adoptZellijSessionID, "session-id", "", "Override provider session ID")
	adoptZellijCmd.Flags().BoolVar(&adoptZellijProtected, "protected", false, "Mark the adopted session as protected in the database")
}

func runAdoptZellij(cmd *cobra.Command, args []string) error {
	if !adoptZellijProtected {
		return fmt.Errorf("--protected is required for adopt-zellij")
	}

	sessionName := strings.TrimSpace(args[0])
	if sessionName == "" {
		return fmt.Errorf("zellij session name is required")
	}

	existingSessions, err := adoptZellijList()
	if err != nil {
		return fmt.Errorf("listing zellij sessions: %w", err)
	}
	if !containsExactSession(existingSessions, sessionName) {
		return fmt.Errorf("zellij session %q does not exist", sessionName)
	}

	layout, err := adoptZellijLayout(sessionName)
	if err != nil {
		return fmt.Errorf("dump-layout for %q: %w", sessionName, err)
	}

	cwd := parseZellijLayoutCWD(layout)
	if cwd == "" {
		return fmt.Errorf("could not resolve cwd from zellij session %q", sessionName)
	}

	agent, err := resolveAdoptedAgent(adoptZellijAgent, layout, sessionName)
	if err != nil {
		return err
	}

	repository := strings.TrimSpace(adoptZellijRepository)
	if repository == "" {
		repository = gitRepoName(cwd)
	}
	if repository == "" {
		repository = inferRepositoryFromCWD(cwd)
	}

	branch := strings.TrimSpace(adoptZellijBranch)
	if branch == "" {
		branch = gitBranchName(cwd)
	}

	providerSessionID := strings.TrimSpace(adoptZellijSessionID)
	if providerSessionID == "" {
		providerSessionID = parseZellijLayoutResumeSessionID(layout)
	}
	if providerSessionID == "" {
		providerSessionID = "zellij-" + sessionName
	}

	db, err := adoptZellijOpenDB("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	recordID := adoptedSessionDBID(agent, repository, providerSessionID)
	if existing := findExactZellijSession(db, sessionName); existing != nil {
		recordID = existing.ID
	}

	now := time.Now()
	sessionRecord := &store.Session{
		ID:             recordID,
		Agent:          string(agent),
		Repository:     repository,
		SessionID:      providerSessionID,
		CWD:            cwd,
		GitBranch:      branch,
		ZellijSession:  sessionName,
		Status:         "active",
		DesiredState:   store.DesiredStateRunning,
		LifecycleState: store.LifecycleStateRunning,
		RuntimeStatus:  "running",
		LastActive:     now,
		Role:           "worker",
		IsProtected:    true,
	}
	if err := store.UpsertSession(db, sessionRecord); err != nil {
		return fmt.Errorf("registering session: %w", err)
	}

	_ = store.LogAction(db, &store.Action{
		SessionID:  sessionRecord.ID,
		ActionType: "adopt-zellij",
		Content:    fmt.Sprintf("Adopted zellij session %s (provider session %s)", sessionName, providerSessionID),
	})

	fmt.Fprintf(cmd.OutOrStdout(), "Adopted protected %s session %q as %s\n", agent, sessionName, sessionRecord.ID)
	return nil
}

func containsExactSession(existing []string, sessionName string) bool {
	for _, line := range existing {
		if strings.EqualFold(strings.TrimSpace(line), sessionName) {
			return true
		}
	}
	return false
}

func resolveAdoptedAgent(flagValue, layout, sessionName string) (provider.Agent, error) {
	switch strings.TrimSpace(strings.ToLower(flagValue)) {
	case "", "auto":
		return inferAgentFromLayout(layout, sessionName), nil
	case string(provider.AgentClaude):
		return provider.AgentClaude, nil
	case string(provider.AgentCodex):
		return provider.AgentCodex, nil
	default:
		return "", fmt.Errorf("unknown agent %q (expected auto, claude, codex)", flagValue)
	}
}

var zellijResumeSessionIDPattern = regexp.MustCompile(`"resume"\s+"([^"]+)"`)

func parseZellijLayoutResumeSessionID(layout string) string {
	matches := zellijResumeSessionIDPattern.FindStringSubmatch(layout)
	if len(matches) != 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func adoptedSessionDBID(agent provider.Agent, repository, sessionID string) string {
	base := repository
	if strings.TrimSpace(base) == "" {
		base = "unknown"
	}
	key := strings.TrimSpace(sessionID)
	if key == "" {
		key = "unknown"
	}
	return fmt.Sprintf("%s:%s:%s", agent, base, key)
}

func findExactZellijSession(db *sql.DB, sessionName string) *store.Session {
	sessions, err := store.FindSessionByZellijSession(db, sessionName)
	if err != nil {
		return nil
	}
	for _, session := range sessions {
		if strings.EqualFold(strings.TrimSpace(session.ZellijSession), sessionName) {
			copy := session
			return &copy
		}
	}
	return nil
}
