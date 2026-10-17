package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/mux"
	"github.com/chaspy/agentctl/internal/provider"
	"github.com/chaspy/agentctl/internal/store"
)

type spawnExecutionRequest struct {
	Repo          RepoEntry
	RepoMode      string
	Branch        string
	SessionName   string
	Message       string
	Summary       string
	Loop          bool
	SelectedAgent provider.Agent
}

type spawnExecutionResult struct {
	SessionDBID   string
	SessionName   string
	WorkDir       string
	GitBranch     string
	LaunchCommand string
}

func executeSpawnRequest(req spawnExecutionRequest) (*spawnExecutionResult, error) {
	workDir := req.Repo.FullPath
	sessionName := req.SessionName
	gitBranch := req.Branch
	repoBaseName := filepath.Base(req.Repo.FullPath)

	if req.RepoMode == "main" && gitBranch == "" {
		if sessionName == "" {
			sessionName = defaultSpawnSessionName(repoBaseName, "", "")
		}
	} else if gitBranch != "" {
		existingPath, err := findExistingWorktree(req.Repo.FullPath, gitBranch)
		if err != nil {
			return nil, err
		}
		if existingPath != "" {
			fmt.Fprintf(os.Stderr, "Reusing existing checkout: %s\n", existingPath)
			workDir = existingPath
		} else {
			worktreePath := filepath.Join(req.Repo.FullPath, defaultWorktreeName(gitBranch))
			if _, err := os.Stat(worktreePath); err == nil {
				fmt.Fprintf(os.Stderr, "Reusing existing worktree: %s\n", worktreePath)
			} else {
				gitCmd := exec.Command("git", "-C", req.Repo.FullPath, "worktree", "add", worktreePath, "-b", gitBranch)
				output, err := gitCmd.CombinedOutput()
				if err != nil {
					gitCmd = exec.Command("git", "-C", req.Repo.FullPath, "worktree", "add", worktreePath, gitBranch)
					output, err = gitCmd.CombinedOutput()
					if err != nil {
						return nil, fmt.Errorf("git worktree add failed: %w\n%s", err, strings.TrimSpace(string(output)))
					}
				}
				fmt.Fprintf(os.Stderr, "Created worktree: %s\n", worktreePath)
			}
			workDir = worktreePath
		}

		if sessionName == "" {
			sessionName = defaultSpawnSessionName(repoBaseName, gitBranch, "")
		}
	} else if sessionName == "" {
		sessionName = defaultSpawnSessionName(repoBaseName, "", "")
	}

	backend, err := resolveMuxBackend()
	if err != nil {
		return nil, err
	}
	if muxSessionExists(sessionName) {
		return nil, fmt.Errorf("%s session %q already exists", backend.Name(), sessionName)
	}

	sessionID := fmt.Sprintf("%s:%s:%s%s", req.SelectedAgent, req.Repo.ShortName, muxSessionIDPrefix(), sessionName)
	if db, err := store.Open(""); err == nil {
		_ = store.UpsertSession(db, &store.Session{
			ID:             sessionID,
			Agent:          string(req.SelectedAgent),
			Repository:     req.Repo.ShortName,
			SessionID:      muxSessionIDPrefix() + sessionName,
			CWD:            workDir,
			GitBranch:      gitBranch,
			ZellijSession:  sessionName,
			Status:         "active",
			DesiredState:   store.DesiredStateRunning,
			LifecycleState: store.LifecycleStateSpawning,
			Role:           "worker",
			IsLoop:         req.Loop,
		})
		db.Close()
	}

	launchCommand := agentLaunchCommand(req.SelectedAgent)
	spawned, err := backend.Spawn(mux.SpawnOptions{
		Name:    sessionName,
		CWD:     workDir,
		Command: launchCommand,
		Agent:   string(req.SelectedAgent),
	})
	if err != nil {
		return nil, err
	}
	runtimePID, runtimePGID, runtimeStartedAt := spawned.RuntimePID, spawned.RuntimePGID, spawned.StartedAt
	if db, err := store.Open(""); err == nil {
		_, _ = store.UpdateSessionRuntimeProcessByZellijSession(db, sessionName, runtimePID, runtimePGID, runtimeStartedAt)
		db.Close()
	}

	fmt.Fprintf(os.Stderr, "Spawned session %q in %s\n", sessionName, workDir)

	if db, err := store.Open(""); err == nil {
		defer db.Close()
		_ = store.LogAction(db, &store.Action{
			SessionID:  sessionName,
			ActionType: "spawn",
			Content:    fmt.Sprintf("Spawned %s in %s (branch: %s)", sessionName, workDir, gitBranch),
		})

		_ = store.UpsertSession(db, &store.Session{
			ID:               sessionID,
			Agent:            string(req.SelectedAgent),
			Repository:       req.Repo.ShortName,
			SessionID:        muxSessionIDPrefix() + sessionName,
			CWD:              workDir,
			GitBranch:        gitBranch,
			ZellijSession:    sessionName,
			Status:           "active",
			DesiredState:     store.DesiredStateRunning,
			LifecycleState:   store.LifecycleStateRunning,
			Role:             "worker",
			IsLoop:           req.Loop,
			RuntimePID:       runtimePID,
			RuntimePGID:      runtimePGID,
			RuntimeStartedAt: runtimeStartedAt,
		})

		if req.Loop {
			_ = store.SetState(db, "loop:cwd:"+workDir, "1")
		}
		if req.Summary != "" {
			_ = store.SetState(db, "spawn_summary:cwd:"+workDir, req.Summary)
		}
	}

	if req.SelectedAgent == provider.AgentCodex {
		fmt.Fprintf(os.Stderr, "Waiting for codex to become ready...\n")
		if err := waitForCodexReady(sessionName, 20*time.Second); err != nil {
			return nil, err
		}
	}

	if req.Message != "" {
		initialMessage := withVersionBumpReminder(req.Message)
		if req.SelectedAgent != provider.AgentCodex {
			if state, _ := backend.Status(sessionName); state == mux.StateNone {
				fmt.Fprintf(os.Stderr, "Waiting for %s to start...\n", req.SelectedAgent)
				time.Sleep(5 * time.Second)
			}
		}
		if err := backend.Send(sessionName, initialMessage); err != nil {
			return nil, fmt.Errorf("failed to send initial message: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Sent initial message to %q\n", sessionName)
	}

	return &spawnExecutionResult{
		SessionDBID:   sessionID,
		SessionName:   sessionName,
		WorkDir:       workDir,
		GitBranch:     gitBranch,
		LaunchCommand: launchCommand,
	}, nil
}

func defaultSpawnSessionName(repoBaseName, branch, override string) string {
	if override != "" {
		return override
	}
	if branch != "" {
		return repoBaseName + "-" + sanitizeBranchName(branch)
	}
	return repoBaseName
}

func defaultWorktreeName(branch string) string {
	return "worktree-" + sanitizeBranchName(branch)
}
