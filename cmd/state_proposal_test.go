package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestRunStateProposalList(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.ReplaceTaskProposalSnapshots(db, "reconcile", []store.TaskProposalSnapshot{
		{
			ID:                "agentctl-create-repo-contract",
			Source:            "reconcile",
			ReportMode:        "read-only",
			RepoRef:           "agentctl",
			Repository:        "chaspy/agentctl",
			Category:          "create_repo_contract",
			Title:             "Create repo contract",
			Objective:         "Add .agent/repo.yaml",
			TaskType:          "docs",
			Risk:              "low",
			ReviewPolicyRef:   "control-plane-review",
			ApprovalPolicyRef: "self-hosting-approval",
			ApprovalStatus:    "not_required",
		},
	}); err != nil {
		t.Fatalf("ReplaceTaskProposalSnapshots: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateProposalJSON
	var out bytes.Buffer
	stateProposalListCmd.SetOut(&out)
	t.Cleanup(func() {
		stateProposalJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateProposalJSON = false

	if err := runStateProposalList(stateProposalListCmd, nil); err != nil {
		t.Fatalf("runStateProposalList: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "agentctl-create-repo-contract") {
		t.Fatalf("expected list output to contain proposal id, got %q", got)
	}
	if !strings.Contains(got, "not_required") {
		t.Fatalf("expected list output to contain approval status, got %q", got)
	}
}

func TestRunStateProposalGetJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agentctl.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.ReplaceTaskProposalSnapshots(db, "reconcile", []store.TaskProposalSnapshot{
		{
			ID:                "agentctl-create-repo-contract",
			Source:            "reconcile",
			ReportMode:        "read-only",
			RepoRef:           "agentctl",
			Repository:        "chaspy/agentctl",
			Category:          "create_repo_contract",
			Title:             "Create repo contract",
			Objective:         "Add .agent/repo.yaml",
			TaskType:          "docs",
			Risk:              "low",
			ReviewPolicyRef:   "control-plane-review",
			ApprovalPolicyRef: "self-hosting-approval",
			ApprovalStatus:    "not_required",
			ApprovalReason:    "docs proposal",
			DesiredOutcome:    []string{".agent/repo.yaml exists"},
			TriggerIssues:     []string{"repo contract not found: .agent/repo.yaml"},
		},
	}); err != nil {
		t.Fatalf("ReplaceTaskProposalSnapshots: %v", err)
	}
	db.Close()

	origDBPath := os.Getenv("AGENTCTL_DB_PATH")
	origJSON := stateProposalJSON
	var out bytes.Buffer
	stateProposalGetCmd.SetOut(&out)
	t.Cleanup(func() {
		stateProposalJSON = origJSON
		if origDBPath == "" {
			_ = os.Unsetenv("AGENTCTL_DB_PATH")
			return
		}
		_ = os.Setenv("AGENTCTL_DB_PATH", origDBPath)
	})
	if err := os.Setenv("AGENTCTL_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	stateProposalJSON = true

	if err := runStateProposalGet(stateProposalGetCmd, []string{"agentctl-create-repo-contract"}); err != nil {
		t.Fatalf("runStateProposalGet: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"id": "agentctl-create-repo-contract"`) {
		t.Fatalf("expected JSON output to contain proposal id, got %q", got)
	}
	if !strings.Contains(got, `"approvalStatus": "not_required"`) {
		t.Fatalf("expected JSON output to contain approval status, got %q", got)
	}
}
