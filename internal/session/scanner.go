package session

import (
	"os"
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
func decodeRepository(encoded string) string {
	// Split by "-" and reconstruct
	parts := strings.Split(encoded, "-")

	// Find "github-com" or similar pattern and extract owner/repo
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "github" && i+1 < len(parts) && parts[i+1] == "com" {
			// Everything after "github-com" is the project path
			if i+2 < len(parts) {
				remaining := parts[i+2:]
				// Group into owner/repo (first two segments)
				if len(remaining) >= 2 {
					return remaining[0] + "/" + strings.Join(remaining[1:], "-")
				}
				return strings.Join(remaining, "/")
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
