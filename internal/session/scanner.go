package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SessionInfo holds metadata about a Claude Code session.
type SessionInfo struct {
	ProjectDir  string    // directory name under ~/.claude/projects/
	Repository string    // human-readable project name
	FilePath    string    // path to the .jsonl file
	ModTime     time.Time // last modification time of the .jsonl file
	SessionID   string    // UUID extracted from the filename
	CWD         string    // working directory from the session
	GitBranch   string    // git branch from the session
	LastMessage     string    // last user or assistant message content (truncated for display)
	LastFullMessage string    // last user or assistant message content (full text for analysis)
	LastRole        string    // "user" or "assistant"
	ErrorType       string    // error type from JSONL metadata (e.g. "rate_limit", "invalid_request")
	IsAPIError      bool      // true if isApiErrorMessage flag is set in JSONL
}

// ScanSessions walks ~/.claude/projects/ and returns sessions updated within maxAge.
func ScanSessions(maxAge time.Duration) ([]SessionInfo, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	projectsDir := filepath.Join(homeDir, ".claude", "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-maxAge)
	var sessions []SessionInfo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip preview worktree directories — these are temporary Python
		// servers started by preview-start.sh and are not real Claude sessions.
		if strings.Contains(entry.Name(), "worktree-preview-") {
			continue
		}

		dirPath := filepath.Join(projectsDir, entry.Name())
		jsonlFiles, err := filepath.Glob(filepath.Join(dirPath, "*.jsonl"))
		if err != nil {
			continue
		}

		for _, f := range jsonlFiles {
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				continue
			}

			sessionID := strings.TrimSuffix(filepath.Base(f), ".jsonl")

			s := SessionInfo{
				ProjectDir:  entry.Name(),
				Repository: decodeRepository(entry.Name()),
				FilePath:    f,
				ModTime:     info.ModTime(),
				SessionID:   sessionID,
			}

			sessions = append(sessions, s)
		}
	}

	return sessions, nil
}

// decodeRepository converts the directory-encoded name back to a readable path.
// e.g. "-Users-01045513-go-src-github-com-chaspy-agentctl"
// becomes "chaspy/agentctl"
//
// Note: This is a best-effort heuristic based on directory names. It only takes
// owner + repo (2 segments) after "github-com" to avoid misidentifying
// subdirectories as part of the repo name. The accurate repository name is
// determined later by RepoNameFromCWD using `git remote get-url origin`.
func decodeRepository(encoded string) string {
	// Split by "-" and reconstruct
	parts := strings.Split(encoded, "-")

	// Find "github-com" or similar pattern and extract owner/repo
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "github" && i+1 < len(parts) && parts[i+1] == "com" {
			// Take only owner + repo (2 segments) after "github-com"
			if i+3 < len(parts) {
				return parts[i+2] + "/" + parts[i+3]
			}
			if i+2 < len(parts) {
				return parts[i+2]
			}
		}
	}

	// Fallback: try to extract meaningful parts from the path
	// Remove leading empty parts from the initial "-"
	cleaned := strings.TrimPrefix(encoded, "-")
	pathParts := strings.Split(cleaned, "-")

	// Take last two meaningful segments as project name
	if len(pathParts) >= 2 {
		return pathParts[len(pathParts)-2] + "/" + pathParts[len(pathParts)-1]
	}

	return encoded
}

// RepoNameFromCWD determines the repository name from the working directory
// by running `git remote get-url origin`. Falls back to path-based heuristic
// if git command fails.
func RepoNameFromCWD(cwd string) string {
	if cwd == "" {
		return ""
	}
	return repoNameFromGitRemote(cwd)
}

// repoNameFromGitRemote runs `git remote get-url origin` in the given directory
// and extracts "owner/repo" from the URL.
var repoNameFromGitRemote = func(cwd string) string {
	cmd := exec.Command("git", "-C", cwd, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseRepoFromRemoteURL(strings.TrimSpace(string(out)))
}

// parseRepoFromRemoteURL extracts "owner/repo" from a git remote URL.
// Supports both HTTPS and SSH formats:
//   - https://github.com/owner/repo.git
//   - git@github.com:owner/repo.git
func parseRepoFromRemoteURL(remoteURL string) string {
	remoteURL = strings.TrimSuffix(remoteURL, ".git")

	// SSH format: git@github.com:owner/repo
	if strings.Contains(remoteURL, ":") && strings.Contains(remoteURL, "@") {
		parts := strings.SplitN(remoteURL, ":", 2)
		if len(parts) == 2 {
			path := strings.Trim(parts[1], "/")
			segments := strings.Split(path, "/")
			if len(segments) >= 2 {
				return segments[len(segments)-2] + "/" + segments[len(segments)-1]
			}
		}
	}

	// HTTPS format: https://github.com/owner/repo
	path := remoteURL
	if idx := strings.Index(remoteURL, "://"); idx >= 0 {
		path = remoteURL[idx+3:]
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) >= 3 {
		return segments[len(segments)-2] + "/" + segments[len(segments)-1]
	}

	return ""
}
