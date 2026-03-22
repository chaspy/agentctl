package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type codexSessionMetaEnvelope struct {
	Payload codexSessionMetaPayload `json:"payload"`
}

type codexSessionMetaPayload struct {
	ID            string `json:"id"`
	CWD           string `json:"cwd"`
	Source        string `json:"source"`
	ModelProvider string `json:"model_provider"`
	Git           struct {
		Branch        string `json:"branch"`
		RepositoryURL string `json:"repository_url"`
	} `json:"git"`
}

type codexEventEnvelope struct {
	Timestamp string            `json:"timestamp"`
	Payload   codexEventPayload `json:"payload"`
}

type codexEventPayload struct {
	Type       string          `json:"type"`
	Message    string          `json:"message"`
	RateLimits codexRateLimits `json:"rate_limits"`
}

type codexRateLimits struct {
	Primary   codexRateWindow `json:"primary"`
	Secondary codexRateWindow `json:"secondary"`
}

type codexRateWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

type codexParsedSession struct {
	SessionInfo
	Rate *codexRateSnapshot
}

type codexRateSnapshot struct {
	Timestamp time.Time
	Limits    codexRateLimits
}

func ScanCodexSessions(maxAge time.Duration) ([]SessionInfo, error) {
	parsed, err := scanCodexSessionFiles(maxAge)
	if err != nil {
		return nil, err
	}

	sessions := make([]SessionInfo, 0, len(parsed))
	for _, item := range parsed {
		sessions = append(sessions, item.SessionInfo)
	}
	return sessions, nil
}

func CodexRate() (RateInfo, error) {
	parsed, err := scanCodexSessionFiles(0)
	if err != nil {
		return RateInfo{}, err
	}

	var latest *codexParsedSession
	for i := range parsed {
		if parsed[i].Rate == nil {
			continue
		}
		if latest == nil || parsed[i].Rate.Timestamp.After(latest.Rate.Timestamp) {
			latest = &parsed[i]
		}
	}
	if latest == nil {
		return RateInfo{}, fmt.Errorf("codex rate info not found")
	}

	// If all rate windows have already reset, the limit is fully available.
	allExpired := true
	if latest.Rate.Limits.Primary.WindowMinutes > 0 && time.Unix(latest.Rate.Limits.Primary.ResetsAt, 0).After(time.Now()) {
		allExpired = false
	}
	if latest.Rate.Limits.Secondary.WindowMinutes > 0 && time.Unix(latest.Rate.Limits.Secondary.ResetsAt, 0).After(time.Now()) {
		allExpired = false
	}

	if allExpired {
		return RateInfo{
			Agent:        AgentCodex,
			Summary:      "available",
			Details:      "all rate windows have reset",
			UpdatedAt:    latest.Rate.Timestamp,
			RemainingPct: 100,
		}, nil
	}

	details := []string{}
	if latest.Rate.Limits.Primary.WindowMinutes > 0 {
		if time.Unix(latest.Rate.Limits.Primary.ResetsAt, 0).Before(time.Now()) {
			details = append(details, "primary: available")
		} else {
			details = append(details, fmt.Sprintf(
				"primary resets in %s",
				formatFutureDuration(time.Unix(latest.Rate.Limits.Primary.ResetsAt, 0)),
			))
		}
	}
	if latest.Rate.Limits.Secondary.WindowMinutes > 0 {
		if time.Unix(latest.Rate.Limits.Secondary.ResetsAt, 0).Before(time.Now()) {
			details = append(details, "secondary: available")
		} else {
			details = append(details, fmt.Sprintf(
				"secondary resets in %s",
				formatFutureDuration(time.Unix(latest.Rate.Limits.Secondary.ResetsAt, 0)),
			))
		}
	}
	if latest.Repository != "" {
		details = append(details, "project="+latest.Repository)
	}

	primaryRemaining := 100 - latest.Rate.Limits.Primary.UsedPercent
	secondaryRemaining := 100 - latest.Rate.Limits.Secondary.UsedPercent

	// Use the more restrictive (lower) remaining percentage for the bar
	pctRemaining := int(primaryRemaining)
	if int(secondaryRemaining) < pctRemaining {
		pctRemaining = int(secondaryRemaining)
	}
	if pctRemaining < 0 {
		pctRemaining = 0
	}

	return RateInfo{
		Agent:        AgentCodex,
		Summary:      fmt.Sprintf("~%.0f%% left (primary) / ~%.0f%% left (secondary)", primaryRemaining, secondaryRemaining),
		Details:      strings.Join(details, ", "),
		UpdatedAt:    latest.Rate.Timestamp,
		RemainingPct: pctRemaining,
	}, nil
}

func scanCodexSessionFiles(maxAge time.Duration) ([]codexParsedSession, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	sessionsDir := filepath.Join(homeDir, ".codex", "sessions")
	if _, err := os.Stat(sessionsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}

	var sessions []codexParsedSession
	err = filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !cutoff.IsZero() && info.ModTime().Before(cutoff) {
			return nil
		}

		session, ok, err := parseCodexSessionFile(path, info.ModTime())
		if err != nil || !ok {
			return nil
		}

		sessions = append(sessions, session)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return sessions, nil
}

func parseCodexSessionFile(path string, modTime time.Time) (codexParsedSession, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return codexParsedSession{}, false, err
	}
	defer f.Close()

	session := codexParsedSession{
		SessionInfo: SessionInfo{
			Agent:   AgentCodex,
			ModTime: modTime,
		},
	}

	foundMeta := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), maxJSONLLineBytes)

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.Contains(line, "\"type\":\"session_meta\""):
			var entry codexSessionMetaEnvelope
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}
			foundMeta = true
			session.SessionID = entry.Payload.ID
			session.CWD = entry.Payload.CWD
			session.GitBranch = entry.Payload.Git.Branch
			session.Repository = projectNameFromRepositoryURL(entry.Payload.Git.RepositoryURL, entry.Payload.CWD)
		case strings.Contains(line, "\"type\":\"event_msg\""):
			var entry codexEventEnvelope
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}

			switch entry.Payload.Type {
			case "user_message":
				session.LastRole = "user"
				session.LastMessage = truncate(entry.Payload.Message, 60)
			case "agent_message":
				session.LastRole = "assistant"
				session.LastMessage = truncate(entry.Payload.Message, 60)
			case "token_count":
				if !hasCodexRateLimits(entry.Payload.RateLimits) {
					continue
				}

				ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
				if err != nil {
					ts = modTime
				}
				session.Rate = &codexRateSnapshot{
					Timestamp: ts,
					Limits:    entry.Payload.RateLimits,
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return codexParsedSession{}, false, err
	}
	if !foundMeta {
		return codexParsedSession{}, false, nil
	}
	if session.Repository == "" {
		session.Repository = projectNameFromCWD(session.CWD)
	}

	return session, true, nil
}

func hasCodexRateLimits(limits codexRateLimits) bool {
	return limits.Primary.WindowMinutes > 0 || limits.Secondary.WindowMinutes > 0
}

func formatFutureDuration(target time.Time) string {
	if target.IsZero() {
		return "-"
	}

	d := time.Until(target)
	if d <= 0 {
		return "expired"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
