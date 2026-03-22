package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

const (
	previewWorktreePrefix = "worktree-preview-"
	previewPIDPrefix      = "/tmp/agentctl-preview-"
	previewBasePort       = 3100
	previewTailscaleHost  = "takeshis-mac-studio-1.tailb74fce.ts.net"
)

var previewCmd = &cobra.Command{
	Use:   "preview <pr-number>",
	Short: "Start a preview environment for a PR",
	Long: `Build and serve the dashboard from a PR branch for preview.

Creates a git worktree from the PR branch, builds the binary, starts the
dashboard server on port 3100+PR, and exposes it via tailscale serve.

Examples:
  agentctl preview 5
  agentctl preview stop 5
  agentctl preview list`,
	Args: cobra.ExactArgs(1),
	RunE: runPreviewStart,
}

var previewStopCmd = &cobra.Command{
	Use:   "stop <pr-number>",
	Short: "Stop a running preview environment",
	Args:  cobra.ExactArgs(1),
	RunE:  runPreviewStop,
}

var previewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List running preview environments",
	Args:  cobra.NoArgs,
	RunE:  runPreviewList,
}

func init() {
	rootCmd.AddCommand(previewCmd)
	previewCmd.AddCommand(previewStopCmd)
	previewCmd.AddCommand(previewListCmd)
}

func runPreviewStart(cmd *cobra.Command, args []string) error {
	prNumber, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid PR number: %w", err)
	}

	repoRoot, err := getRepoRoot()
	if err != nil {
		return err
	}

	// Get PR branch name via gh CLI
	branch, err := getPRBranch(prNumber)
	if err != nil {
		return err
	}

	worktreeName := previewWorktreePrefix + strconv.Itoa(prNumber)
	worktreePath := filepath.Join(repoRoot, worktreeName)
	port := previewBasePort + prNumber
	pidFile := previewPIDPrefix + strconv.Itoa(prNumber) + ".pid"

	// Check if already running
	if _, err := os.Stat(pidFile); err == nil {
		return fmt.Errorf("preview for PR #%d is already running (PID file exists: %s). Use 'preview stop %d' first", prNumber, pidFile, prNumber)
	}

	// Create worktree
	if _, err := os.Stat(worktreePath); err == nil {
		fmt.Fprintf(os.Stderr, "Reusing existing worktree: %s\n", worktreePath)
	} else {
		// Fetch latest from remote first
		fetchCmd := exec.Command("git", "-C", repoRoot, "fetch", "origin", branch)
		fetchCmd.Stderr = os.Stderr
		_ = fetchCmd.Run()

		gitCmd := exec.Command("git", "-C", repoRoot, "worktree", "add", worktreePath, "origin/"+branch)
		output, err := gitCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git worktree add failed: %w\n%s", err, strings.TrimSpace(string(output)))
		}
		fmt.Fprintf(os.Stderr, "Created worktree: %s (branch: %s)\n", worktreePath, branch)
	}

	// Build in the worktree
	fmt.Fprintf(os.Stderr, "Building in worktree...\n")
	binaryPath := filepath.Join(worktreePath, "agentctl-preview")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, "./...")
	buildCmd.Dir = worktreePath
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %w\n%s", err, strings.TrimSpace(string(buildOut)))
	}

	// Start serve in background
	fmt.Fprintf(os.Stderr, "Starting preview server on port %d...\n", port)
	serveCmd := exec.Command(binaryPath, "serve", "--port", strconv.Itoa(port))
	serveCmd.Dir = worktreePath
	serveCmd.Stdout = nil
	serveCmd.Stderr = nil
	serveCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := serveCmd.Start(); err != nil {
		return fmt.Errorf("failed to start preview server: %w", err)
	}

	pid := serveCmd.Process.Pid

	// Save PID file
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Set up tailscale serve
	tsPath := fmt.Sprintf("/preview/%d", prNumber)
	tsCmd := exec.Command("tailscale", "serve", "--set-path", tsPath, strconv.Itoa(port))
	tsOut, err := tsCmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: tailscale serve failed (preview still accessible on localhost): %s\n", strings.TrimSpace(string(tsOut)))
	}

	url := fmt.Sprintf("https://%s/preview/%d", previewTailscaleHost, prNumber)
	fmt.Printf("Preview for PR #%d started\n", prNumber)
	fmt.Printf("  Branch:    %s\n", branch)
	fmt.Printf("  Port:      %d\n", port)
	fmt.Printf("  PID:       %d\n", pid)
	fmt.Printf("  Local:     http://localhost:%d\n", port)
	fmt.Printf("  URL:       %s\n", url)

	return nil
}

func runPreviewStop(cmd *cobra.Command, args []string) error {
	prNumber, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid PR number: %w", err)
	}

	repoRoot, err := getRepoRoot()
	if err != nil {
		return err
	}

	pidFile := previewPIDPrefix + strconv.Itoa(prNumber) + ".pid"
	worktreeName := previewWorktreePrefix + strconv.Itoa(prNumber)
	worktreePath := filepath.Join(repoRoot, worktreeName)

	// Kill the serve process
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "No PID file found for PR #%d, skipping process kill\n", prNumber)
	} else {
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if err == nil {
			proc, err := os.FindProcess(pid)
			if err == nil {
				if err := proc.Signal(syscall.SIGTERM); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to kill process %d: %v\n", pid, err)
				} else {
					fmt.Printf("Killed preview server (PID %d)\n", pid)
				}
			}
		}
	}

	// Remove tailscale serve path
	tsPath := fmt.Sprintf("/preview/%d", prNumber)
	tsCmd := exec.Command("tailscale", "serve", "--set-path", tsPath, "off")
	tsOut, err := tsCmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: tailscale serve cleanup failed: %s\n", strings.TrimSpace(string(tsOut)))
	} else {
		fmt.Printf("Removed tailscale serve path %s\n", tsPath)
	}

	// Remove worktree
	if _, err := os.Stat(worktreePath); err == nil {
		gitCmd := exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", worktreePath)
		output, err := gitCmd.CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove worktree: %s\n", strings.TrimSpace(string(output)))
		} else {
			fmt.Printf("Removed worktree: %s\n", worktreePath)
		}
	}

	// Remove PID file
	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: failed to remove PID file: %v\n", err)
	}

	// Clean up built binary if it exists
	binaryPath := filepath.Join(worktreePath, "agentctl-preview")
	_ = os.Remove(binaryPath)

	fmt.Printf("Preview for PR #%d stopped\n", prNumber)
	return nil
}

func runPreviewList(cmd *cobra.Command, args []string) error {
	repoRoot, err := getRepoRoot()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to read repo directory: %w", err)
	}

	found := false
	fmt.Printf("%-8s %-30s %-8s %-6s %s\n", "PR", "BRANCH", "PORT", "PID", "URL")
	fmt.Printf("%-8s %-30s %-8s %-6s %s\n", "---", "---", "---", "---", "---")

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), previewWorktreePrefix) {
			continue
		}
		prStr := strings.TrimPrefix(entry.Name(), previewWorktreePrefix)
		prNum, err := strconv.Atoi(prStr)
		if err != nil {
			continue
		}

		port := previewBasePort + prNum
		pidFile := previewPIDPrefix + prStr + ".pid"
		url := fmt.Sprintf("https://%s/preview/%d", previewTailscaleHost, prNum)

		// Get branch name from worktree
		worktreePath := filepath.Join(repoRoot, entry.Name())
		branchCmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
		branchOut, err := branchCmd.Output()
		branch := "(unknown)"
		if err == nil {
			branch = strings.TrimSpace(string(branchOut))
		}

		// Get PID and check if alive
		pidStr := "-"
		if pidData, err := os.ReadFile(pidFile); err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
			if err == nil {
				proc, err := os.FindProcess(pid)
				if err == nil && proc.Signal(syscall.Signal(0)) == nil {
					pidStr = strconv.Itoa(pid)
				} else {
					pidStr = strconv.Itoa(pid) + " (dead)"
				}
			}
		}

		fmt.Printf("%-8d %-30s %-8d %-6s %s\n", prNum, branch, port, pidStr, url)
		found = true
	}

	if !found {
		fmt.Println("No preview environments found.")
	}

	return nil
}

func getRepoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func getPRBranch(prNumber int) (string, error) {
	out, err := exec.Command("gh", "pr", "view", strconv.Itoa(prNumber), "--json", "headRefName").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get PR #%d info (is gh CLI installed and authenticated?): %w", prNumber, err)
	}

	var result struct {
		HeadRefName string `json:"headRefName"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("failed to parse PR info: %w", err)
	}
	if result.HeadRefName == "" {
		return "", fmt.Errorf("PR #%d has no branch name", prNumber)
	}
	return result.HeadRefName, nil
}
