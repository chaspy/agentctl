package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	spawnBranch  string
	spawnName    string
	spawnMessage string
	spawnLoop    bool
)

var spawnCmd = &cobra.Command{
	Use:   "spawn <repo>",
	Short: "Create a new zellij session with claude in the specified repo",
	Long: `Resolves a repository short name (e.g. "org/myproject" or "myproject"),
optionally creates a git worktree for the specified branch, creates a new zellij session,
and starts claude in it.

Examples:
  agentctl spawn owner/repo
  agentctl spawn myproject --branch fix/bug-123
  agentctl spawn myproject --branch fix/bug-123 --message "issue #123 を修正して"`,
	Args: cobra.ExactArgs(1),
	RunE: runSpawn,
}

func init() {
	rootCmd.AddCommand(spawnCmd)
	spawnCmd.Flags().StringVar(&spawnBranch, "branch", "", "Create a worktree with this branch name")
	spawnCmd.Flags().StringVar(&spawnName, "name", "", "Zellij session name (auto-generated if not set)")
	spawnCmd.Flags().StringVar(&spawnMessage, "message", "", "Initial instruction to send after claude starts")
	spawnCmd.Flags().BoolVar(&spawnLoop, "loop", false, "Mark this session as a loop session (is_loop=1 in DB)")
}

func runSpawn(cmd *cobra.Command, args []string) error {
	repo, err := ResolveRepoPath(args[0])
	if err != nil {
		return err
	}

	workDir := repo.FullPath
	repoBaseName := filepath.Base(repo.FullPath)
	sessionName := spawnName

	// Check repo config mode (default: "branch")
	repoMode := "branch"
	if db, err := store.Open(""); err == nil {
		if mode, err := store.GetRepoConfig(db, repo.ShortName); err == nil && mode != "" {
			repoMode = mode
		}
		db.Close()
	}

	// If mode=main and no branch specified, work directly on main
	if repoMode == "main" && spawnBranch == "" {
		if sessionName == "" {
			sessionName = repoBaseName
		}
	} else if spawnBranch != "" {
		// Check if the branch is already checked out somewhere (main dir or existing worktree)
		existingPath, err := findExistingWorktree(repo.FullPath, spawnBranch)
		if err != nil {
			return err
		}
		if existingPath != "" {
			fmt.Fprintf(os.Stderr, "Reusing existing checkout: %s\n", existingPath)
			workDir = existingPath
		} else {
			worktreeName := "worktree-" + sanitizeBranchName(spawnBranch)
			worktreePath := filepath.Join(repo.FullPath, worktreeName)

			if _, err := os.Stat(worktreePath); err == nil {
				fmt.Fprintf(os.Stderr, "Reusing existing worktree: %s\n", worktreePath)
			} else {
				// Create worktree — try -b (new branch) first, then existing branch
				gitCmd := exec.Command("git", "-C", repo.FullPath, "worktree", "add", worktreePath, "-b", spawnBranch)
				output, err := gitCmd.CombinedOutput()
				if err != nil {
					gitCmd = exec.Command("git", "-C", repo.FullPath, "worktree", "add", worktreePath, spawnBranch)
					output, err = gitCmd.CombinedOutput()
					if err != nil {
						return fmt.Errorf("git worktree add failed: %w\n%s", err, strings.TrimSpace(string(output)))
					}
				}
				fmt.Fprintf(os.Stderr, "Created worktree: %s\n", worktreePath)
			}
			workDir = worktreePath
		}

		if sessionName == "" {
			sessionName = repoBaseName + "-" + sanitizeBranchName(spawnBranch)
		}
	} else {
		if sessionName == "" {
			sessionName = repoBaseName
		}
	}

	// Check if session already exists
	existing, _ := exec.Command("env", "-u", "ZELLIJ", "zellij", "list-sessions", "--short").Output()
	for _, line := range strings.Split(strings.TrimSpace(string(existing)), "\n") {
		if strings.TrimSpace(line) == sessionName {
			return fmt.Errorf("zellij session %q already exists", sessionName)
		}
	}

	// Create a new zellij session in the background.
	// Uses `script` to allocate a PTY (zellij requires one) and
	// `env -u ZELLIJ` to avoid "already inside zellij" errors.
	bgCmd := exec.Command("script", "-q", "/dev/null",
		"env", "-u", "ZELLIJ", "-u", "CLAUDECODE",
		"zellij", "-s", sessionName)
	bgCmd.Dir = workDir
	bgCmd.Stdin = nil
	bgCmd.Stdout = nil
	bgCmd.Stderr = nil
	bgCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := bgCmd.Start(); err != nil {
		return fmt.Errorf("failed to create zellij session: %w", err)
	}

	// Wait for the session to be ready
	fmt.Fprintf(os.Stderr, "Creating zellij session %q in %s...\n", sessionName, workDir)
	if err := waitForSession(sessionName, 10*time.Second); err != nil {
		return err
	}

	// Dismiss any Zellij tip overlay that may intercept keystrokes
	dismissTip := exec.Command("env", "-u", "ZELLIJ",
		"zellij", "-s", sessionName, "action", "write", "27") // ESC
	_ = dismissTip.Run()
	time.Sleep(500 * time.Millisecond)

	// Start claude in the new session with bypass permissions for unattended operation.
	// Deny rules in ~/.claude/settings.json still apply in bypass mode.
	writeChars := exec.Command("env", "-u", "ZELLIJ",
		"zellij", "-s", sessionName, "action", "write-chars", "claude --dangerously-skip-permissions")
	if out, err := writeChars.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send claude command: %w\n%s", err, string(out))
	}
	writeEnter := exec.Command("env", "-u", "ZELLIJ",
		"zellij", "-s", sessionName, "action", "write", "13")
	if out, err := writeEnter.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send enter: %w\n%s", err, string(out))
	}

	fmt.Fprintf(os.Stderr, "Spawned session %q in %s\n", sessionName, workDir)

	// Log spawn to database (fire-and-forget)
	if db, err := store.Open(""); err == nil {
		defer db.Close()
		_ = store.LogAction(db, &store.Action{
			SessionID:  sessionName,
			ActionType: "spawn",
			Content:    fmt.Sprintf("Spawned %s in %s (branch: %s)", sessionName, workDir, spawnBranch),
		})
		if spawnLoop {
			_ = store.SetState(db, "loop:cwd:"+workDir, "1")
		}
	}

	// Send initial message if specified
	if spawnMessage != "" {
		// Wait for claude to initialize
		fmt.Fprintf(os.Stderr, "Waiting for claude to start...\n")
		time.Sleep(5 * time.Second)

		writeMsg := exec.Command("env", "-u", "ZELLIJ",
			"zellij", "-s", sessionName, "action", "write-chars", spawnMessage)
		if out, err := writeMsg.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to send initial message: %w\n%s", err, string(out))
		}
		writeMsgEnter := exec.Command("env", "-u", "ZELLIJ",
			"zellij", "-s", sessionName, "action", "write", "13")
		if out, err := writeMsgEnter.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to send enter for message: %w\n%s", err, string(out))
		}
		fmt.Fprintf(os.Stderr, "Sent initial message to %q\n", sessionName)
	}

	return nil
}

func waitForSession(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("env", "-u", "ZELLIJ", "zellij", "list-sessions", "--short").Output()
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if strings.TrimSpace(line) == name {
					return nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for session %q to appear", name)
}

func sanitizeBranchName(branch string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
	return r.Replace(branch)
}

// findExistingWorktree returns the path where the given branch is already
// checked out (main dir or any worktree), or "" if not found.
func findExistingWorktree(repoPath, branch string) (string, error) {
	out, err := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return "", fmt.Errorf("git worktree list failed: %w", err)
	}
	return parseWorktreePath(string(out), branch), nil
}

// parseWorktreePath parses `git worktree list --porcelain` output and returns
// the path of the entry whose branch matches the given branch name.
func parseWorktreePath(output, branch string) string {
	var currentPath string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			currentPath = strings.TrimPrefix(line, "worktree ")
		} else if line == "branch refs/heads/"+branch {
			return currentPath
		}
	}
	return ""
}
