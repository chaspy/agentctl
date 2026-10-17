package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/controlplane"
	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
)

var (
	spawnBranch   string
	spawnName     string
	spawnMessage  string
	spawnSummary  string
	spawnLoop     bool
	spawnAgent    string
	spawnTaskType string
)

const versionBumpReminder = "※ commit & push 前に必ず VERSION ファイルのパッチバージョンを +1 すること（CI の check-version-bump が fail するため）。"

var spawnCmd = &cobra.Command{
	Use:   "spawn <repo>",
	Short: "Create a new mux session (zellij or herdr) with Claude or Codex in the specified repo",
	Long: `Resolves a repository short name (e.g. "org/myproject" or "myproject"),
optionally creates a git worktree for the specified branch, creates a new mux session
(zellij by default, herdr when AGENTCTL_MUX=herdr),
and starts the selected agent in it.

Examples:
  agentctl spawn owner/repo
  agentctl spawn myproject --branch fix/bug-123
  agentctl spawn myproject --branch fix/bug-123 --agent codex --message "issue #123 を修正して"`,
	Args: cobra.ExactArgs(1),
	RunE: runSpawn,
}

func init() {
	rootCmd.AddCommand(spawnCmd)
	spawnCmd.Flags().StringVar(&spawnBranch, "branch", "", "Create a worktree with this branch name")
	spawnCmd.Flags().StringVar(&spawnName, "name", "", "Mux session name (auto-generated if not set)")
	spawnCmd.Flags().StringVar(&spawnMessage, "message", "", "Initial instruction to send after the agent starts")
	spawnCmd.Flags().StringVar(&spawnSummary, "summary", "", "Task summary for this session (skips LLM generation)")
	spawnCmd.Flags().BoolVar(&spawnLoop, "loop", false, "Mark this session as a loop session (is_loop=1 in DB)")
	spawnCmd.Flags().StringVar(&spawnAgent, "agent", "", `Worker agent: "auto", "claude", or "codex" (defaults to repo config, then auto)`)
	spawnCmd.Flags().StringVar(&spawnTaskType, "task-type", "general", `Task type hint for auto routing: "general", "implementation", "research", "docs", or "review"`)
}

func runSpawn(cmd *cobra.Command, args []string) error {
	repo, err := ResolveRepoPath(args[0])
	if err != nil {
		return err
	}

	sessionName := spawnName
	repoMode := "branch"
	repoAgent := spawnAgentAuto
	profileSource := controlplane.RepoProfileSourceDefault

	if db, err := store.Open(""); err == nil {
		if profile, err := controlplane.GetRepoProfile(db, repo.ShortName); err == nil && profile != nil {
			if profile.Mode != "" {
				repoMode = profile.Mode
			}
			if profile.Agent != "" {
				repoAgent = profile.Agent
			}
			profileSource = profile.PrimarySource
			fmt.Fprintf(os.Stderr, "Using repo profile %s: mode=%s(%s) agent=%s(%s)\n",
				profile.PrimarySource,
				profile.Mode,
				profile.ModeSource,
				profile.Agent,
				profile.AgentSource,
			)
		}
		db.Close()
	}
	if profileSource == controlplane.RepoProfileSourceDefault {
		fmt.Fprintf(os.Stderr, "Using default repo profile: mode=%s(default) agent=%s(default)\n", repoMode, repoAgent)
	}

	agentPref := spawnAgent
	if agentPref == "" {
		agentPref = repoAgent
	}
	selectedAgent, reason, err := chooseSpawnAgent(agentPref, spawnTaskType)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Selected agent %q (%s)\n", selectedAgent, reason)

	_, err = executeSpawnRequest(spawnExecutionRequest{
		Repo:          repo,
		RepoMode:      repoMode,
		Branch:        spawnBranch,
		SessionName:   sessionName,
		Message:       spawnMessage,
		Summary:       spawnSummary,
		Loop:          spawnLoop,
		SelectedAgent: selectedAgent,
	})
	return err
}

func waitForSession(name string, timeout time.Duration) error {
	backend, err := resolveMuxBackend()
	if err != nil {
		return err
	}
	return mux.WaitForSession(backend, name, timeout)
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

func withVersionBumpReminder(message string) string {
	if strings.Contains(message, "VERSION") {
		return message
	}
	if strings.HasSuffix(message, "\n") {
		return message + versionBumpReminder
	}
	return message + "\n\n" + versionBumpReminder
}
