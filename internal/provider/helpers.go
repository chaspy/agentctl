package provider

import (
	"net/url"
	"path/filepath"
	"strings"
)

const maxJSONLLineBytes = 64 * 1024 * 1024

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)

	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

func projectNameFromRepositoryURL(repoURL string, cwd string) string {
	if repoURL != "" {
		if u, err := url.Parse(repoURL); err == nil {
			path := strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/")
			if path != "" {
				return path
			}
		}
	}
	return projectNameFromCWD(cwd)
}

func projectNameFromCWD(cwd string) string {
	cleaned := filepath.ToSlash(cwd)
	parts := strings.Split(cleaned, "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "github.com" {
			return parts[i+1] + "/" + parts[i+2]
		}
	}

	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return cwd
}
