package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	codexResumeName       string
	codexResumeProtected  bool
	codexResumeOpenDB     = store.Open
	codexResumeScanCodex  = provider.ScanCodexSessions
	codexResumeWait       = waitForSession
	codexResumeStart      = startCodexResumeSession
	codexResumeListZellij = listZellijSessionsShort
)

var codexResumeCmd = &cobra.Command{
	Use:   "codex-resume <session-id>",
	Short: "Resume a protected Codex session in a new zellij session",
	Long: `Finds a protected Codex session by session ID, resolves its cwd if possible,
creates a new zellij session, and starts "codex --search resume <session-id>" in it.

Examples:
  agentctl codex-resume 123e4567-e89b-12d3-a456-426614174000 --name my-resume --protected`,
	Args: cobra.ExactArgs(1),
	RunE: runCodexResume,
}

func init() {
	rootCmd.AddCommand(codexResumeCmd)
	codexResumeCmd.Flags().StringVar(&codexResumeName, "name", "", "Zellij session name (auto-generated if not set)")
	codexResumeCmd.Flags().BoolVar(&codexResumeProtected, "protected", false, "Mark the resumed session as protected in the database")
}

type codexResumeTarget struct {
	SourceSessionID string
	Repository      string
	CWD             string
	GitBranch       string
	SourceAgent     string
}

func runCodexResume(cmd *cobra.Command, args []string) error {
	if !codexResumeProtected {
		return fmt.Errorf("--protected is required for codex-resume")
	}

	db, err := codexResumeOpenDB("")
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	target, err := resolveCodexResumeTarget(db, args[0])
	if err != nil {
		return err
	}
	if strings.TrimSpace(target.CWD) == "" {
		return fmt.Errorf("could not resolve cwd for codex session %q", args[0])
	}

	sessionName := strings.TrimSpace(codexResumeName)
	if sessionName == "" {
		sessionName = defaultCodexResumeSessionName(target.Repository, target.SourceSessionID)
	}
	if sessionName == "" {
		sessionName = "codex-resume"
	}

	existing, err := codexResumeListZellij()
	if err != nil {
		return fmt.Errorf("listing zellij sessions: %w", err)
	}
	for _, line := range existing {
		if strings.TrimSpace(line) == sessionName {
			return fmt.Errorf("zellij session %q already exists", sessionName)
		}
	}

	sessionID := "zellij-" + sessionName
	sessionRecord := &store.Session{
		ID:             codexResumeSessionDBID(target.Repository, sessionID),
		Agent:          string(provider.AgentCodex),
		Repository:     target.Repository,
		SessionID:      sessionID,
		CWD:            target.CWD,
		GitBranch:      target.GitBranch,
		ZellijSession:  sessionName,
		Status:         "active",
		DesiredState:   store.DesiredStateRunning,
		LifecycleState: store.LifecycleStateSpawning,
		Role:           "worker",
		IsProtected:    true,
	}
	if err := store.UpsertSession(db, sessionRecord); err != nil {
		return fmt.Errorf("registering session: %w", err)
	}

	if err := codexResumeStart(sessionName, target.CWD, target.SourceSessionID); err != nil {
		return err
	}

	if err := store.UpsertSession(db, &store.Session{
		ID:             sessionRecord.ID,
		Agent:          string(provider.AgentCodex),
		Repository:     target.Repository,
		SessionID:      sessionID,
		CWD:            target.CWD,
		GitBranch:      target.GitBranch,
		ZellijSession:  sessionName,
		Status:         "active",
		DesiredState:   store.DesiredStateRunning,
		LifecycleState: store.LifecycleStateRunning,
		RuntimeStatus:  "running",
		Role:           "worker",
		IsProtected:    true,
	}); err != nil {
		return fmt.Errorf("finalizing session registration: %w", err)
	}

	_ = store.LogAction(db, &store.Action{
		SessionID:  sessionRecord.ID,
		ActionType: "codex-resume",
		Content:    fmt.Sprintf("Resumed codex session %s in %s", target.SourceSessionID, target.CWD),
	})

	fmt.Fprintf(cmd.OutOrStdout(), "Resumed protected codex session %s in zellij session %q\n", target.SourceSessionID, sessionName)
	return nil
}

func resolveCodexResumeTarget(db *sql.DB, sessionID string) (*codexResumeTarget, error) {
	target, dbErr := resolveCodexResumeTargetFromDB(db, sessionID)
	if dbErr == nil && target.CWD != "" {
		return target, nil
	}

	jsonlTarget, jsonlErr := resolveCodexResumeTargetFromJSONL(sessionID)
	if jsonlErr == nil {
		if target == nil {
			return jsonlTarget, nil
		}
		if jsonlTarget.Repository == "" {
			jsonlTarget.Repository = target.Repository
		}
		if jsonlTarget.CWD == "" {
			jsonlTarget.CWD = target.CWD
		}
		if jsonlTarget.GitBranch == "" {
			jsonlTarget.GitBranch = target.GitBranch
		}
		if jsonlTarget.SourceAgent == "" {
			jsonlTarget.SourceAgent = target.SourceAgent
		}
		return jsonlTarget, nil
	}

	if dbErr == nil && target != nil {
		return target, nil
	}
	if dbErr != nil {
		return nil, dbErr
	}
	return nil, jsonlErr
}

func resolveCodexResumeTargetFromDB(db *sql.DB, sessionID string) (*codexResumeTarget, error) {
	s, err := store.GetSessionBySessionID(db, sessionID)
	if err != nil {
		return nil, err
	}
	if s.Agent != "" && s.Agent != string(provider.AgentCodex) {
		return nil, fmt.Errorf("session %q is not a codex session", sessionID)
	}
	target := &codexResumeTarget{
		SourceSessionID: sessionID,
		Repository:      s.Repository,
		CWD:             s.CWD,
		GitBranch:       s.GitBranch,
		SourceAgent:     s.Agent,
	}
	if target.Repository == "" && target.CWD != "" {
		target.Repository = inferRepositoryFromCWD(target.CWD)
	}
	return target, nil
}

func resolveCodexResumeTargetFromJSONL(sessionID string) (*codexResumeTarget, error) {
	sessions, err := codexResumeScanCodex(30 * 24 * time.Hour)
	if err != nil {
		return nil, fmt.Errorf("scanning codex sessions: %w", err)
	}

	var candidates []provider.SessionInfo
	for _, s := range sessions {
		if s.SessionID == sessionID {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no codex session found matching %q", sessionID)
	}

	best := candidates[0]
	for _, s := range candidates[1:] {
		if s.ModTime.After(best.ModTime) {
			best = s
		}
	}

	repo := best.Repository
	if repo == "" && best.CWD != "" {
		repo = inferRepositoryFromCWD(best.CWD)
	}
	return &codexResumeTarget{
		SourceSessionID: sessionID,
		Repository:      repo,
		CWD:             best.CWD,
		GitBranch:       best.GitBranch,
		SourceAgent:     string(best.Agent),
	}, nil
}

func defaultCodexResumeSessionName(repository, sessionID string) string {
	base := strings.TrimSpace(filepath.Base(repository))
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "codex"
	}
	trimmed := sanitizeSessionName(sessionID)
	if len(trimmed) > 12 {
		trimmed = trimmed[:12]
	}
	if trimmed == "" {
		return base + "-codex-resume"
	}
	return fmt.Sprintf("%s-codex-resume-%s", base, trimmed)
}

func sanitizeSessionName(value string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-", "_", "-")
	return strings.Trim(replacer.Replace(strings.ToLower(value)), "-")
}

func codexResumeSessionDBID(repository, sessionID string) string {
	if repository == "" {
		repository = "codex"
	}
	return fmt.Sprintf("codex:%s:%s", repository, sessionID)
}

func startCodexResumeSession(sessionName, cwd, sourceSessionID string) error {
	bgCmd := exec.Command("script", "-q", "/dev/null",
		"env", "-u", "ZELLIJ", "-u", "CLAUDECODE",
		"zellij", "-s", sessionName)
	bgCmd.Dir = cwd
	bgCmd.Stdin = nil
	bgCmd.Stdout = nil
	bgCmd.Stderr = nil
	bgCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := bgCmd.Start(); err != nil {
		return fmt.Errorf("failed to create zellij session: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Creating zellij session %q in %s...\n", sessionName, cwd)
	if err := codexResumeWait(sessionName, 10*time.Second); err != nil {
		return err
	}

	dismissTip := exec.Command("env", "-u", "ZELLIJ",
		"zellij", "-s", sessionName, "action", "write", "27")
	_ = dismissTip.Run()
	time.Sleep(500 * time.Millisecond)

	resumeCommand := fmt.Sprintf("codex --search resume %s", sourceSessionID)
	writeChars := exec.Command("env", "-u", "ZELLIJ",
		"zellij", "-s", sessionName, "action", "write-chars", resumeCommand)
	if out, err := writeChars.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send resume command: %w\n%s", err, string(out))
	}
	writeEnter := exec.Command("env", "-u", "ZELLIJ",
		"zellij", "-s", sessionName, "action", "write", "13")
	if out, err := writeEnter.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send enter: %w\n%s", err, string(out))
	}
	return nil
}

func listZellijSessionsShort() ([]string, error) {
	out, err := exec.Command("env", "-u", "ZELLIJ", "zellij", "list-sessions", "--short").Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result, nil
}
