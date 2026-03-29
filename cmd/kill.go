package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	killForce  bool
	killDryRun bool
)

var killCmd = &cobra.Command{
	Use:   "kill <session>",
	Short: "Kill a zellij session and clean up its worktree if applicable",
	Long: `Kill a zellij session by name. If the session was spawned with a worktree,
the worktree is also removed. Supports substring matching like other commands.

Safety:
  - If the worktree has uncommitted changes, the worktree is NOT removed (session is still killed).
  - If the worktree has unpushed commits, the worktree is NOT removed (session is still killed).
  - If the branch has no upstream (never pushed), the worktree is NOT removed.
  - Use --force to skip ALL safety checks and remove the worktree regardless.
  - Use --dry-run to see what would happen without actually doing anything.

Examples:
  agentctl kill my-session
  agentctl kill myproject-fix-bug --dry-run
  agentctl kill myproject-fix-bug
  agentctl kill myproject-fix-bug --force`,
	Args: cobra.ExactArgs(1),
	RunE: runKill,
}

func init() {
	rootCmd.AddCommand(killCmd)
	killCmd.Flags().BoolVar(&killForce, "force", false, "Force worktree removal even with unpushed commits")
	killCmd.Flags().BoolVar(&killDryRun, "dry-run", false, "Show what would be done without actually doing it")
}

func runKill(cmd *cobra.Command, args []string) error {
	query := args[0]

	// List sessions and find match
	out, err := exec.Command("env", "-u", "ZELLIJ", "zellij", "list-sessions", "--short").Output()
	if err != nil {
		return fmt.Errorf("failed to list zellij sessions: %w", err)
	}

	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			sessions = append(sessions, line)
		}
	}

	// Exact match first
	for _, s := range sessions {
		if s == query {
			if err := killSessionAndWorktree(s); err != nil {
				return err
			}
			logKillAction(s)
			return nil
		}
	}

	// Substring match
	q := strings.ToLower(query)
	var candidates []string
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s), q) {
			candidates = append(candidates, s)
		}
	}

	if len(candidates) == 0 {
		return fmt.Errorf("no zellij session matching %q", query)
	}
	if len(candidates) > 1 {
		return fmt.Errorf("ambiguous query %q, matches: %s", query, strings.Join(candidates, ", "))
	}

	if err := killSessionAndWorktree(candidates[0]); err != nil {
		return err
	}
	logKillAction(candidates[0])
	return nil
}

func killSessionAndWorktree(name string) error {
	// 0. Graceful exit: send /exit to trigger Stop hook (e.g. sui-memory)
	if !killDryRun {
		exitCmd := exec.Command("env", "-u", "ZELLIJ", "zellij", "--session", name, "action", "write-chars", "/exit\n")
		if err := exitCmd.Run(); err == nil {
			fmt.Printf("Sent /exit to session %q, waiting for Stop hook...\n", name)
			time.Sleep(10 * time.Second)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: failed to send /exit to session %q: %v\n", name, err)
		}
	}

	// 1. Kill the zellij session
	// Try kill-session first (for active sessions), then delete-session (for EXITED sessions)
	if killDryRun {
		fmt.Printf("[dry-run] Would kill zellij session %q\n", name)
	} else {
		killExec := exec.Command("env", "-u", "ZELLIJ", "zellij", "kill-session", name)
		output, err := killExec.CombinedOutput()
		if err != nil {
			// kill-session fails on EXITED sessions — try delete-session as fallback
			delExec := exec.Command("env", "-u", "ZELLIJ", "zellij", "delete-session", name)
			delOutput, delErr := delExec.CombinedOutput()
			if delErr != nil {
				return fmt.Errorf("failed to kill/delete session %q: kill: %s, delete: %s",
					name, strings.TrimSpace(string(output)), strings.TrimSpace(string(delOutput)))
			}
			fmt.Printf("Deleted exited zellij session %q\n", name)
		} else {
			fmt.Printf("Killed zellij session %q\n", name)
			// kill-session transitions the session to EXITED state but does not remove it.
			// Always run delete-session afterward so the session name is freed for reuse.
			delExec := exec.Command("env", "-u", "ZELLIJ", "zellij", "delete-session", name)
			delOutput, delErr := delExec.CombinedOutput()
			if delErr != nil {
				// Non-fatal: session is dead, but the name may linger until zellij GCs it.
				fmt.Fprintf(os.Stderr, "Warning: failed to delete session %q after kill: %s\n",
					name, strings.TrimSpace(string(delOutput)))
			}
		}
	}

	// 2. Find and remove associated worktree
	// Session naming convention: {RepoName}-{branch} maps to worktree-{branch}
	// Scan GOPATH for worktree directories matching the session name
	home, err := os.UserHomeDir()
	if err != nil {
		return nil // Session killed, worktree cleanup is best-effort
	}
	base := filepath.Join(home, "go", "src", "github.com")

	// Look for worktree directories that match the session name pattern
	// Session "myproject-fix-bug" could map to worktree at:
	//   .../org/worktree-fix-bug (inside a parent that has myproject)
	orgs, err := os.ReadDir(base)
	if err != nil {
		return nil
	}

	for _, org := range orgs {
		if !org.IsDir() {
			continue
		}
		orgPath := filepath.Join(base, org.Name())
		repos, err := os.ReadDir(orgPath)
		if err != nil {
			continue
		}
		for _, repo := range repos {
			if !repo.IsDir() || strings.HasPrefix(repo.Name(), "worktree-") {
				continue
			}
			repoPath := filepath.Join(orgPath, repo.Name())
			repoEntries, err := os.ReadDir(repoPath)
			if err != nil {
				continue
			}
			for _, entry := range repoEntries {
				if !entry.IsDir() {
					continue
				}
				dirName := entry.Name()
				if !strings.HasPrefix(dirName, "worktree-") {
					continue
				}
				worktreePath := filepath.Join(repoPath, dirName)
				branchPart := strings.TrimPrefix(dirName, "worktree-")
				expectedSessionName := repo.Name() + "-" + branchPart

				if expectedSessionName == name {
					if killDryRun {
						fmt.Printf("[dry-run] Would remove worktree: %s\n", worktreePath)
						return checkWorktreeSafety(worktreePath)
					}
					return removeWorktree(repoPath, repo.Name(), worktreePath, killForce)
				}
			}
		}
	}

	return nil
}


// logKillAction logs a kill action to the database (fire-and-forget).
func logKillAction(sessionName string) {
	if db, err := store.Open(""); err == nil {
		defer db.Close()
		_ = store.LogAction(db, &store.Action{
			SessionID:  sessionName,
			ActionType: "kill",
			Content:    fmt.Sprintf("Killed session %s", sessionName),
		})
	}
}

// checkWorktreeSafety reports the safety status of a worktree without modifying anything.
func checkWorktreeSafety(worktreePath string) error {
	// Check uncommitted changes
	statusCmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	statusOut, err := statusCmd.Output()
	if err == nil && strings.TrimSpace(string(statusOut)) != "" {
		lines := strings.Split(strings.TrimSpace(string(statusOut)), "\n")
		fmt.Fprintf(os.Stderr, "  UNSAFE: %d uncommitted change(s)\n", len(lines))
	} else {
		fmt.Fprintf(os.Stderr, "  OK: no uncommitted changes\n")
	}

	// Check branch and upstream
	branchCmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := branchCmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  UNKNOWN: could not determine branch\n")
		return nil
	}
	branch := strings.TrimSpace(string(branchOut))

	// Check if upstream exists
	upstreamCmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "@{upstream}")
	_, err = upstreamCmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "  UNSAFE: branch %q has no upstream (never pushed)\n", branch)
		return nil
	}

	// Check unpushed commits
	unpushedCmd := exec.Command("git", "-C", worktreePath, "log", "--oneline", "@{upstream}..HEAD")
	unpushedOut, err := unpushedCmd.Output()
	if err == nil && strings.TrimSpace(string(unpushedOut)) != "" {
		lines := strings.Split(strings.TrimSpace(string(unpushedOut)), "\n")
		fmt.Fprintf(os.Stderr, "  UNSAFE: %d unpushed commit(s) on branch %q\n", len(lines), branch)
	} else {
		fmt.Fprintf(os.Stderr, "  OK: all commits pushed on branch %q\n", branch)
	}

	return nil
}

func removeWorktree(repoPath, repoName, worktreePath string, force bool) error {

	// Safety checks (skip all if --force)
	if !force {
		branchCmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
		branchOut, err := branchCmd.Output()
		if err == nil {
			branch := strings.TrimSpace(string(branchOut))

			// Check 1: upstream exists?
			upstreamCmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "@{upstream}")
			_, upErr := upstreamCmd.Output()
			if upErr != nil {
				fmt.Fprintf(os.Stderr, "WARNING: worktree %s (branch %s) has no upstream (never pushed).\n", worktreePath, branch)
				fmt.Fprintf(os.Stderr, "Worktree NOT removed. Push first or use --force to override.\n")
				return nil
			}

			// Check 2: unpushed commits?
			unpushedCmd := exec.Command("git", "-C", worktreePath, "log", "--oneline", "@{upstream}..HEAD")
			unpushedOut, err := unpushedCmd.Output()
			if err == nil && strings.TrimSpace(string(unpushedOut)) != "" {
				lines := strings.Split(strings.TrimSpace(string(unpushedOut)), "\n")
				fmt.Fprintf(os.Stderr, "WARNING: worktree %s (branch %s) has %d unpushed commit(s):\n", worktreePath, branch, len(lines))
				for _, line := range lines {
					fmt.Fprintf(os.Stderr, "  %s\n", line)
				}
				fmt.Fprintf(os.Stderr, "Worktree NOT removed. Push first or use --force to override.\n")
				return nil
			}
		}
	}

	// git worktree remove (without --force: git itself will refuse if there are uncommitted changes)
	gitCmd := exec.Command("git", "-C", repoPath, "worktree", "remove", worktreePath)
	output, err := gitCmd.CombinedOutput()
	if err != nil {
		if force {
			gitCmd = exec.Command("git", "-C", repoPath, "worktree", "remove", "--force", worktreePath)
			output, err = gitCmd.CombinedOutput()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to remove worktree %s: %s\n", worktreePath, strings.TrimSpace(string(output)))
				return nil
			}
		} else {
			fmt.Fprintf(os.Stderr, "Worktree NOT removed (uncommitted changes?): %s\nUse --force to override.\n", strings.TrimSpace(string(output)))
			return nil
		}
	}
	fmt.Printf("Removed worktree: %s\n", worktreePath)
	return nil
}
