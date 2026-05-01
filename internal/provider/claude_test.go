package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClaudeRateAllowedUsesFreshCCUsageCachedAt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeClaudeStatsCache(t, home)

	loc := mustLoadLocation(t, "Asia/Tokyo")
	now := time.Now().In(loc)
	resetTime := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, loc)
	if resetTime.After(now) {
		resetTime = resetTime.Add(-time.Hour)
	}
	hitTime := resetTime.Add(-2 * time.Hour)

	writeClaudeObservedLimitSession(t, home, "allowed-after-reset", hitTime, resetTime)

	cachedAt := time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	writeCCUsageTestCache(t, home, ccusageCache{
		Block: &ccusageBlock{
			ID:        "active",
			StartTime: time.Now().Add(-90 * time.Minute),
			EndTime:   time.Now().Add(3 * time.Hour),
			IsActive:  true,
			CostUSD:   9.6,
			Projection: &struct {
				TotalCost        float64 `json:"totalCost"`
				RemainingMinutes int     `json:"remainingMinutes"`
			}{
				TotalCost: 45,
			},
		},
		CachedAt: cachedAt.Unix(),
	})

	info, err := ClaudeRate()
	if err != nil {
		t.Fatalf("ClaudeRate() error = %v", err)
	}

	if !strings.HasPrefix(info.Summary, "allowed (") {
		t.Fatalf("ClaudeRate() summary = %q, want allowed", info.Summary)
	}
	if !info.UpdatedAt.Equal(cachedAt) {
		t.Fatalf("ClaudeRate() UpdatedAt = %s, want ccusage cached_at %s", info.UpdatedAt, cachedAt)
	}
	if info.UpdatedAt.Equal(hitTime) {
		t.Fatalf("ClaudeRate() UpdatedAt should not keep old hit time %s when allowed", hitTime)
	}
}

func TestClaudeRateRateLimitedKeepsObservedLimitTimestamp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeClaudeStatsCache(t, home)

	loc := mustLoadLocation(t, "Asia/Tokyo")
	now := time.Now().In(loc)
	resetTime := now.Add(time.Hour).Truncate(time.Hour)
	if !resetTime.After(now) {
		resetTime = resetTime.Add(time.Hour)
	}
	hitTime := resetTime.Add(-2 * time.Hour)

	writeClaudeObservedLimitSession(t, home, "currently-rate-limited", hitTime, resetTime)

	info, err := ClaudeRate()
	if err != nil {
		t.Fatalf("ClaudeRate() error = %v", err)
	}

	if !strings.HasPrefix(info.Summary, "RATE LIMITED (") {
		t.Fatalf("ClaudeRate() summary = %q, want RATE LIMITED", info.Summary)
	}
	if !info.UpdatedAt.Equal(hitTime) {
		t.Fatalf("ClaudeRate() UpdatedAt = %s, want observed hit time %s", info.UpdatedAt, hitTime)
	}
}

func writeClaudeStatsCache(t *testing.T, home string) {
	t.Helper()

	path := filepath.Join(home, ".claude", "stats-cache.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir stats dir: %v", err)
	}

	payload := claudeStatsCache{
		LastComputedDate: "2026-05-01",
		DailyModelTokens: []struct {
			Date          string           `json:"date"`
			TokensByModel map[string]int64 `json:"tokensByModel"`
		}{
			{
				Date:          "2026-05-01",
				TokensByModel: map[string]int64{"claude-sonnet": 112859},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal stats cache: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write stats cache: %v", err)
	}
}

func writeClaudeObservedLimitSession(t *testing.T, home, sessionID string, hitTime, resetTime time.Time) {
	t.Helper()

	projectDir := filepath.Join(home, ".claude", "projects", "-Users-test-agentctl")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}

	path := filepath.Join(projectDir, sessionID+".jsonl")
	message := "You hit your limit. Please try again later. resets " + formatResetHour(resetTime) + " (Asia/Tokyo)"
	line := `{"type":"assistant","message":{"role":"assistant","content":"` + message + `"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	if err := os.Chtimes(path, hitTime, hitTime); err != nil {
		t.Fatalf("chtimes session file: %v", err)
	}
}

func formatResetHour(t time.Time) string {
	hour := t.Hour()
	suffix := "am"
	if hour >= 12 {
		suffix = "pm"
	}
	displayHour := hour % 12
	if displayHour == 0 {
		displayHour = 12
	}
	return strconv.Itoa(displayHour) + suffix
}

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %q: %v", name, err)
	}
	return loc
}
