package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunStateManagedRepoList(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	specJSON := `{
		"repo":"github.com/chaspy/myassistant",
		"tier":"control-plane",
		"role":"personal-ops-console",
		"visibility":"private",
		"defaultRoutingPolicyRef":"control-plane-default",
		"defaultReviewPolicyRef":"control-plane-review",
		"defaultApprovalPolicyRef":"self-hosting-approval",
		"defaultReleaseGateRef":"control-plane-release",
		"selfHosting":{
			"canModifyDocs":true,
			"requiresHumanApprovalBeforeMerge":true
		}
	}`
	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:                     "myassistant",
		Repository:               "chaspy/myassistant",
		Role:                     "personal-ops-console",
		Visibility:               "private",
		DefaultRoutingPolicyRef:  "control-plane-default",
		DefaultReviewPolicyRef:   "control-plane-review",
		DefaultApprovalPolicyRef: "self-hosting-approval",
		RawSpecJSON:              specJSON,
		SpecHash:                 "abc123",
		SourcePath:               "/tmp/myassistant/ops/repos/myassistant.yaml",
		SourceCommit:             "deadbeef",
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateManagedRepoJSON
	var out bytes.Buffer
	stateManagedRepoListCmd.SetOut(&out)
	t.Cleanup(func() {
		stateManagedRepoJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateManagedRepoJSON = false

	if err := runStateManagedRepoList(stateManagedRepoListCmd, nil); err != nil {
		t.Fatalf("runStateManagedRepoList: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "myassistant") {
		t.Fatalf("expected list output to contain repo name, got %q", got)
	}
	if !strings.Contains(got, "control-plane") {
		t.Fatalf("expected list output to contain derived tier, got %q", got)
	}
	if !strings.Contains(got, "control-plane-release") {
		t.Fatalf("expected list output to contain derived release gate ref, got %q", got)
	}
}

func TestRunStateManagedRepoGetJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:        "agentctl",
		Repository:  "chaspy/agentctl",
		Role:        "agentops-control-plane",
		Visibility:  "public",
		RawSpecJSON: `{"repo":"github.com/chaspy/agentctl","tier":"control-plane","role":"agentops-control-plane","visibility":"public","selfHosting":{"canModifyRuntimeCode":true,"requiresHumanApprovalBeforeMerge":true}}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateManagedRepoJSON
	var out bytes.Buffer
	stateManagedRepoGetCmd.SetOut(&out)
	t.Cleanup(func() {
		stateManagedRepoJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateManagedRepoJSON = true

	if err := runStateManagedRepoGet(stateManagedRepoGetCmd, []string{"agentctl"}); err != nil {
		t.Fatalf("runStateManagedRepoGet: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"name": "agentctl"`) {
		t.Fatalf("expected JSON output to contain repo name, got %q", got)
	}
	if !strings.Contains(got, `"tier": "control-plane"`) {
		t.Fatalf("expected JSON output to contain derived tier, got %q", got)
	}
	if !strings.Contains(got, `"requiresHumanApprovalBeforeMerge": true`) {
		t.Fatalf("expected JSON output to contain self-hosting policy, got %q", got)
	}
}
