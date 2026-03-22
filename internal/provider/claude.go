package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chaspy/agentctl/internal/session"
)

type claudeTelemetryEvent struct {
	EventData struct {
		EventName          string `json:"event_name"`
		ClientTimestamp    string `json:"client_timestamp"`
		Model              string `json:"model"`
		AdditionalMetadata string `json:"additional_metadata"`
	} `json:"event_data"`
}

type claudeLimitMetadata struct {
	Status                            string `json:"status"`
	UnifiedRateLimitFallbackAvailable bool   `json:"unifiedRateLimitFallbackAvailable"`
	HoursTillReset                    int    `json:"hoursTillReset"`
}

type claudeLimitEvent struct {
	Timestamp time.Time
	Model     string
	Metadata  claudeLimitMetadata
}

type claudeStatsCache struct {
	LastComputedDate string `json:"lastComputedDate"`
	DailyModelTokens []struct {
		Date          string           `json:"date"`
		TokensByModel map[string]int64 `json:"tokensByModel"`
	} `json:"dailyModelTokens"`
}

func ScanClaudeSessions(maxAge time.Duration) ([]SessionInfo, error) {
	rawSessions, err := session.ScanSessions(maxAge)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	sessions := make([]SessionInfo, 0, len(rawSessions))
	for i := range rawSessions {
		_ = session.EnrichSession(&rawSessions[i])
		sessions = append(sessions, SessionInfo{
			Agent:           AgentClaude,
			Repository:     rawSessions[i].Repository,
			ModTime:         rawSessions[i].ModTime,
			SessionID:       rawSessions[i].SessionID,
			CWD:             rawSessions[i].CWD,
			GitBranch:       rawSessions[i].GitBranch,
			LastMessage:     rawSessions[i].LastMessage,
			LastFullMessage: rawSessions[i].LastFullMessage,
			LastRole:        rawSessions[i].LastRole,
			FilePath:        rawSessions[i].FilePath,
			ErrorType:       rawSessions[i].ErrorType,
			IsAPIError:      rawSessions[i].IsAPIError,
		})
	}

	return sessions, nil
}

func ClaudeRate() (RateInfo, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return RateInfo{}, err
	}

	telemetryPath := filepath.Join(homeDir, ".claude", "telemetry")
	statsPath := filepath.Join(homeDir, ".claude", "stats-cache.json")

	event, eventErr := latestClaudeLimitEvent(telemetryPath)
	stats, statsErr := readClaudeStatsCache(statsPath)
	if eventErr != nil && statsErr != nil {
		return RateInfo{}, fmt.Errorf("claude rate info not found")
	}

	info := RateInfo{
		Agent:        AgentClaude,
		Summary:      "unknown",
		RemainingPct: -1, // unknown until calculated
	}

	var details []string
	now := time.Now()

	// Get token usage from ccusage (most accurate source for 5h window)
	tokenUsageStr := ""
	remainingPct := -1 // track remaining percentage alongside token usage
	if block := ccusageActiveBlock(); block != nil {
		windowEnd := block.EndTime
		windowStart := block.StartTime
		remaining := time.Until(windowEnd)
		costStr := fmt.Sprintf("$%.1f", block.CostUSD)
		if remaining > 0 {
			tokenUsageStr = fmt.Sprintf("%s spent, window ends %s (in %s)",
				costStr, windowEnd.Local().Format("15:04"),
				remaining.Truncate(time.Minute))
			if block.Projection != nil && block.Projection.TotalCost > 0 {
				tokenUsageStr += fmt.Sprintf(", projected $%.0f", block.Projection.TotalCost)
			}
			// Estimate remaining percentage from window time elapsed
			totalWindow := windowEnd.Sub(windowStart)
			if totalWindow > 0 {
				pct := float64(remaining) / float64(totalWindow) * 100
				if pct > 100 {
					pct = 100
				}
				if pct < 0 {
					pct = 0
				}
				remainingPct = int(pct)
			}
		} else {
			tokenUsageStr = fmt.Sprintf("%s spent (window ended)", costStr)
			remainingPct = 0
		}
	} else {
		// Fallback: use internal session token scanning
		usage5h, _, _ := session.ScanAllTokenUsage(5 * time.Hour)
		outK := usage5h.OutputTokens / 1000
		capacity := readRateLimitCapacity()
		if capacity > 0 && usage5h.OutputTokens > 0 {
			pct := float64(usage5h.OutputTokens) / float64(capacity) * 100
			if pct > 100 {
				pct = 100
			}
			rem := 100 - pct
			remainingPct = int(rem)
			tokenUsageStr = fmt.Sprintf("%.0f%% left (%dK/%dK out)", rem, outK, capacity/1000)
		} else if outK > 0 {
			tokenUsageStr = fmt.Sprintf("%dK out/5h", outK)
		}
	}

	// Check observed rate limit from session logs (most reliable source)
	if observed := latestClaudeObservedLimit(24 * time.Hour); observed != nil {
		info.UpdatedAt = observed.Session.ModTime
		if !observed.ResetTime.IsZero() {
			if now.Before(observed.ResetTime) {
				remaining := observed.ResetTime.Sub(now).Truncate(time.Minute)
				info.Summary = fmt.Sprintf("RATE LIMITED (resets %s, in %s)",
					observed.ResetTime.Local().Format("15:04"), remaining)
				remainingPct = 0
			} else {
				windowEnd := observed.ResetTime.Add(5 * time.Hour)
				if now.Before(windowEnd) {
					remainingTime := windowEnd.Sub(now).Truncate(time.Minute)
					if tokenUsageStr != "" {
						info.Summary = fmt.Sprintf("allowed (%s, window ends %s in %s)",
							tokenUsageStr, windowEnd.Local().Format("15:04"), remainingTime)
					} else {
						// Fallback to time-based estimate
						elapsed := now.Sub(observed.ResetTime)
						pct := 100 - (elapsed.Minutes()/(5*60))*100
						if pct < 0 {
							pct = 0
						}
						remainingPct = int(pct)
						info.Summary = fmt.Sprintf("allowed (~%.0f%% left, window ends %s in %s)",
							pct, windowEnd.Local().Format("15:04"), remainingTime)
					}
				} else {
					// Window expired — show token usage if available
					if tokenUsageStr != "" {
						info.Summary = fmt.Sprintf("allowed (%s)", tokenUsageStr)
					} else {
						info.Summary = "allowed (no usage data)"
					}
				}
			}
		} else {
			info.Summary = "hit limit (reset time unknown)"
			remainingPct = 0
		}
	} else if event != nil {
		// Fallback to telemetry event — but flag if stale
		info.UpdatedAt = event.Timestamp
		staleHours := now.Sub(event.Timestamp).Hours()
		if staleHours > 6 {
			if tokenUsageStr != "" {
				info.Summary = fmt.Sprintf("allowed (%s)", tokenUsageStr)
			} else {
				info.Summary = fmt.Sprintf("allowed (no limit hit, telemetry %.0fh old)", staleHours)
			}
		} else if event.Metadata.Status != "" {
			info.Summary = event.Metadata.Status
			if event.Metadata.HoursTillReset > 0 {
				resetAt := event.Timestamp.Add(time.Duration(event.Metadata.HoursTillReset) * time.Hour)
				if now.Before(resetAt) {
					remaining := resetAt.Sub(now).Truncate(time.Minute)
					details = append(details, fmt.Sprintf("resets at %s (in %s)", resetAt.Local().Format("15:04"), remaining))
					remainingPct = 0
				}
			}
		}
	} else {
		if tokenUsageStr != "" {
			info.Summary = fmt.Sprintf("allowed (%s)", tokenUsageStr)
		} else {
			info.Summary = "allowed (no usage data)"
		}
	}

	if event != nil && event.Model != "" {
		details = append(details, "model="+event.Model)
	}

	if stats != nil {
		if info.UpdatedAt.IsZero() {
			if t, err := time.Parse("2006-01-02", stats.LastComputedDate); err == nil {
				info.UpdatedAt = t
			}
		}
		if usage := latestClaudeUsageSummary(stats); usage != "" {
			details = append(details, usage)
		}
	}

	if len(details) == 0 {
		info.Details = "-"
	} else {
		info.Details = strings.Join(details, ", ")
	}

	// Burn rate: count active sessions only (no fake % when data is stale)
	info.BurnRate = claudeBurnRate(now)

	info.RemainingPct = remainingPct
	return info, nil
}

func claudeBurnRate(now time.Time) string {
	sessions, err := ScanClaudeSessions(1 * time.Hour)
	if err != nil {
		return "-"
	}

	// Count sessions active in last 10 minutes
	active := 0
	for _, s := range sessions {
		if now.Sub(s.ModTime) < 10*time.Minute {
			active++
		}
	}

	// Sum token usage in last 5 hours (rate limit window)
	usage, sessionCount, err := session.ScanAllTokenUsage(5 * time.Hour)
	if err != nil || sessionCount == 0 {
		if active == 0 {
			return "idle"
		}
		return fmt.Sprintf("%d active", active)
	}

	outK := usage.OutputTokens / 1000

	var parts []string
	if active > 0 {
		parts = append(parts, fmt.Sprintf("%d active", active))
	} else {
		parts = append(parts, "idle")
	}

	// Calculate tokens/hour burn rate from last 1 hour
	recentUsage, _, _ := session.ScanAllTokenUsage(1 * time.Hour)
	recentOutK := recentUsage.OutputTokens / 1000

	// Check if we know the capacity from a previous rate limit hit
	capacity := readRateLimitCapacity()
	if capacity > 0 {
		pct := float64(usage.OutputTokens) / float64(capacity) * 100
		if pct > 100 {
			pct = 100
		}
		parts = append(parts, fmt.Sprintf("%.0f%% used (%dK/%dK out), %dK/1h", pct, outK, capacity/1000, recentOutK))
	} else {
		parts = append(parts, fmt.Sprintf("%dK out/5h, %dK/1h", outK, recentOutK))
	}

	return strings.Join(parts, ", ")
}

type observedLimit struct {
	Session   SessionInfo
	ResetTime time.Time // parsed from message like "resets 6pm (Asia/Tokyo)"
}

func latestClaudeObservedLimit(maxAge time.Duration) *observedLimit {
	sessions, err := ScanClaudeSessions(maxAge)
	if err != nil {
		return nil
	}

	var latest *observedLimit
	for i := range sessions {
		message := strings.ToLower(sessions[i].LastMessage)
		if !strings.Contains(message, "hit your limit") {
			continue
		}
		if latest == nil || sessions[i].ModTime.After(latest.Session.ModTime) {
			ol := &observedLimit{Session: sessions[i]}
			ol.ResetTime = parseResetTime(sessions[i].LastMessage, sessions[i].ModTime)
			latest = ol
		}
	}
	return latest
}

// parseResetTime extracts reset time from messages like "resets 6pm (Asia/Tokyo)"
func parseResetTime(message string, hitTime time.Time) time.Time {
	lower := strings.ToLower(message)
	idx := strings.Index(lower, "resets ")
	if idx < 0 {
		return time.Time{}
	}
	rest := lower[idx+len("resets "):]

	// Extract time part like "6pm" or "11am"
	var hourStr string
	var isPM bool
	for i, c := range rest {
		if c >= '0' && c <= '9' {
			hourStr += string(c)
		} else {
			if strings.HasPrefix(rest[i:], "pm") {
				isPM = true
			} else if strings.HasPrefix(rest[i:], "am") {
				isPM = false
			}
			break
		}
	}
	if hourStr == "" {
		return time.Time{}
	}

	hour := 0
	for _, c := range hourStr {
		hour = hour*10 + int(c-'0')
	}
	if isPM && hour != 12 {
		hour += 12
	} else if !isPM && hour == 12 {
		hour = 0
	}

	// Extract timezone from "(Asia/Tokyo)" pattern
	loc := hitTime.Location()
	if tzIdx := strings.Index(rest, "("); tzIdx >= 0 {
		if tzEnd := strings.Index(rest[tzIdx:], ")"); tzEnd >= 0 {
			tzName := rest[tzIdx+1 : tzIdx+tzEnd]
			if l, err := time.LoadLocation(tzName); err == nil {
				loc = l
			}
		}
	}

	// Build reset time on the same day as hit time, in the parsed timezone
	hitInLoc := hitTime.In(loc)
	resetTime := time.Date(hitInLoc.Year(), hitInLoc.Month(), hitInLoc.Day(), hour, 0, 0, 0, loc)
	// If reset time is before hit time, it must be the next day
	if resetTime.Before(hitTime) {
		resetTime = resetTime.Add(24 * time.Hour)
	}
	return resetTime
}

func latestClaudeLimitEvent(telemetryDir string) (*claudeLimitEvent, error) {
	entries, err := os.ReadDir(telemetryDir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		files = append(files, filepath.Join(telemetryDir, entry.Name()))
	}

	sort.Strings(files)

	var latest *claudeLimitEvent
	for _, filePath := range files {
		f, err := os.Open(filePath)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 1024*1024), maxJSONLLineBytes)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.Contains(line, "tengu_claudeai_limits_status_changed") {
				continue
			}

			var event claudeTelemetryEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}
			if event.EventData.EventName != "tengu_claudeai_limits_status_changed" {
				continue
			}

			ts, err := time.Parse(time.RFC3339Nano, event.EventData.ClientTimestamp)
			if err != nil {
				continue
			}

			var metadata claudeLimitMetadata
			if err := json.Unmarshal([]byte(event.EventData.AdditionalMetadata), &metadata); err != nil {
				continue
			}

			if latest == nil || ts.After(latest.Timestamp) {
				latest = &claudeLimitEvent{
					Timestamp: ts,
					Model:     event.EventData.Model,
					Metadata:  metadata,
				}
			}
		}

		_ = f.Close()
	}

	if latest == nil {
		return nil, fmt.Errorf("claude limit status event not found")
	}
	return latest, nil
}

func readClaudeStatsCache(statsPath string) (*claudeStatsCache, error) {
	data, err := os.ReadFile(statsPath)
	if err != nil {
		return nil, err
	}

	var stats claudeStatsCache
	if err := json.Unmarshal(data, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func latestClaudeUsageSummary(stats *claudeStatsCache) string {
	if len(stats.DailyModelTokens) == 0 {
		if stats.LastComputedDate == "" {
			return ""
		}
		return "stats updated " + stats.LastComputedDate
	}

	latest := stats.DailyModelTokens[0]
	for _, day := range stats.DailyModelTokens[1:] {
		if day.Date > latest.Date {
			latest = day
		}
	}

	var total int64
	for _, tokens := range latest.TokensByModel {
		total += tokens
	}

	return fmt.Sprintf("usage %s=%d tokens", latest.Date, total)
}

// rateLimitCapacityFile returns the path to the capacity file.
func rateLimitCapacityFile() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".claude", "rate-limit-capacity")
}

// readRateLimitCapacity reads the known rate limit capacity (output tokens).
func readRateLimitCapacity() int64 {
	data, err := os.ReadFile(rateLimitCapacityFile())
	if err != nil {
		return 0
	}
	var cap int64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &cap); err != nil {
		return 0
	}
	return cap
}

// RecordRateLimitCapacity saves the output token count at which a rate limit was hit.
func RecordRateLimitCapacity(outputTokens int64) error {
	return os.WriteFile(rateLimitCapacityFile(), []byte(fmt.Sprintf("%d\n", outputTokens)), 0644)
}

// ccusage integration — shells out to npx ccusage for accurate 5h billing window data.

type ccusageBlock struct {
	ID        string    `json:"id"`
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime"`
	IsActive  bool      `json:"isActive"`
	Entries   int       `json:"entries"`
	CostUSD   float64   `json:"costUSD"`
	BurnRate  *struct {
		CostPerHour float64 `json:"costPerHour"`
	} `json:"burnRate"`
	Projection *struct {
		TotalCost        float64 `json:"totalCost"`
		RemainingMinutes int     `json:"remainingMinutes"`
	} `json:"projection"`
}

type ccusageResponse struct {
	Blocks []ccusageBlock `json:"blocks"`
}

// ccusageActiveBlock runs ccusage and returns the active billing block, or nil on error.
func ccusageActiveBlock() *ccusageBlock {
	cmd := exec.Command("npx", "ccusage@latest", "blocks", "--json")
	cmd.Env = append(os.Environ(), "NODE_NO_WARNINGS=1")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var resp ccusageResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil
	}

	for i := range resp.Blocks {
		if resp.Blocks[i].IsActive {
			return &resp.Blocks[i]
		}
	}
	return nil
}
