package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chaspy/agentctl/internal/provider"
)

func TestBuildDeliveryBaselines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	baselines, err := buildDeliveryBaselines([]provider.SessionInfo{{FilePath: path}})
	if err != nil {
		t.Fatalf("buildDeliveryBaselines: %v", err)
	}
	if len(baselines) != 1 {
		t.Fatalf("len(baselines) = %d, want 1", len(baselines))
	}
}

func TestWaitForDeliveryUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	modTime, err := fileModTime(path)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = os.WriteFile(path, []byte("{\"type\":\"user\"}\n"), 0o644)
	}()

	if err := waitForDeliveryUpdate(map[string]time.Time{path: modTime}, 300*time.Millisecond, 10*time.Millisecond); err != nil {
		t.Fatalf("waitForDeliveryUpdate: %v", err)
	}
}

func TestWaitForDeliveryUpdateTimeout(t *testing.T) {
	err := waitForDeliveryUpdate(map[string]time.Time{"/nonexistent/session.jsonl": time.Now()}, 30*time.Millisecond, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
