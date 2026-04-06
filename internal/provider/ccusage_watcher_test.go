package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCCUsageTestCache(t *testing.T, home string, cache ccusageCache) {
	t.Helper()
	path := filepath.Join(home, ".agentctl", "ccusage-cache.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
}

func TestReadCCUsageCacheWithMaxAgeFresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	block := &ccusageBlock{ID: "fresh"}
	writeCCUsageTestCache(t, home, ccusageCache{
		Block:    block,
		CachedAt: time.Now().Add(-2 * time.Minute).Unix(),
	})

	got, ok := readCCUsageCacheWithMaxAge(5 * time.Minute)
	if !ok {
		t.Fatal("readCCUsageCacheWithMaxAge() = not ok, want ok")
	}
	if got == nil || got.ID != block.ID {
		t.Fatalf("readCCUsageCacheWithMaxAge() block = %#v, want ID %q", got, block.ID)
	}
}

func TestReadCCUsageCacheWithMaxAgeAllowsStaleFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	block := &ccusageBlock{ID: "stale-ok"}
	writeCCUsageTestCache(t, home, ccusageCache{
		Block:    block,
		CachedAt: time.Now().Add(-10 * time.Minute).Unix(),
	})

	got, ok := readCCUsageCacheWithMaxAge(24 * time.Hour)
	if !ok {
		t.Fatal("readCCUsageCacheWithMaxAge() = not ok, want ok for stale fallback")
	}
	if got == nil || got.ID != block.ID {
		t.Fatalf("readCCUsageCacheWithMaxAge() block = %#v, want ID %q", got, block.ID)
	}
}

func TestReadCCUsageCacheWithMaxAgeRejectsExpiredCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeCCUsageTestCache(t, home, ccusageCache{
		Block:    &ccusageBlock{ID: "expired"},
		CachedAt: time.Now().Add(-26 * time.Hour).Unix(),
	})

	if got, ok := readCCUsageCacheWithMaxAge(24 * time.Hour); ok || got != nil {
		t.Fatalf("readCCUsageCacheWithMaxAge() = (%#v, %v), want (nil, false)", got, ok)
	}
}
