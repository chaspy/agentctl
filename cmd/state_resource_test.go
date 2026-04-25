package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunStateResourceList(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertControlPlaneResource(db, &store.ControlPlaneResource{
		Kind:        resourceKindRoutingPolicy,
		Name:        "control-plane-default",
		APIVersion:  "myassistant.dev/v1alpha1",
		RawSpecJSON: `{"version":"2026-04-25","rules":[{"prefer":"codex"}]}`,
	}); err != nil {
		t.Fatalf("UpsertControlPlaneResource: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateResourceJSON
	origKind := stateResourceKind
	var out bytes.Buffer
	stateResourceListCmd.SetOut(&out)
	t.Cleanup(func() {
		stateResourceJSON = origJSON
		stateResourceKind = origKind
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateResourceJSON = false
	stateResourceKind = resourceKindRoutingPolicy

	if err := runStateResourceList(stateResourceListCmd, nil); err != nil {
		t.Fatalf("runStateResourceList: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "control-plane-default") {
		t.Fatalf("expected list output to contain resource name, got %q", got)
	}
	if !strings.Contains(got, "RoutingPolicy") {
		t.Fatalf("expected list output to contain kind, got %q", got)
	}
}

func TestRunStateResourceGetJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertControlPlaneResource(db, &store.ControlPlaneResource{
		Kind:        resourceKindApprovalPolicy,
		Name:        "self-hosting-approval",
		APIVersion:  "myassistant.dev/v1alpha1",
		RawSpecJSON: `{"version":"2026-04-25","rules":[{"requiresHumanApproval":true}]}`,
	}); err != nil {
		t.Fatalf("UpsertControlPlaneResource: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateResourceJSON
	var out bytes.Buffer
	stateResourceGetCmd.SetOut(&out)
	t.Cleanup(func() {
		stateResourceJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateResourceJSON = true

	if err := runStateResourceGet(stateResourceGetCmd, []string{resourceKindApprovalPolicy, "self-hosting-approval"}); err != nil {
		t.Fatalf("runStateResourceGet: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"kind": "ApprovalPolicy"`) {
		t.Fatalf("expected JSON output to contain kind, got %q", got)
	}
	if !strings.Contains(got, `"name": "self-hosting-approval"`) {
		t.Fatalf("expected JSON output to contain name, got %q", got)
	}
	if !strings.Contains(got, `"requiresHumanApproval": true`) {
		t.Fatalf("expected JSON output to contain parsed spec, got %q", got)
	}
}
