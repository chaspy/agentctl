package web

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestHandleReconcileReturnsObservedManagedRepoState(t *testing.T) {
	tmpDir := t.TempDir()
	home := filepath.Join(tmpDir, "home")
	repoPath := filepath.Join(home, "go", "src", "github.com", "chaspy", "myassistant")
	contractPath := filepath.Join(repoPath, ".agent", "repo.yaml")
	if err := os.MkdirAll(filepath.Dir(contractPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(contractPath, []byte("kind: RepoContract\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("HOME", home)

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:             "myassistant",
		Repository:       "chaspy/myassistant",
		Role:             "personal-ops-console",
		Visibility:       "private",
		RepoContractPath: ".agent/repo.yaml",
		RawSpecJSON:      `{"repo":"github.com/chaspy/myassistant","tier":"control-plane","role":"personal-ops-console","visibility":"private","repoContractPath":".agent/repo.yaml"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}

	server := New(db, func(*sql.DB, string, int, bool) (int, error) { return 0, nil })
	req := httptest.NewRequest(http.MethodGet, "/api/reconcile", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Summary struct {
			ManagedRepos       int `json:"managedRepos"`
			LocalClonesFound   int `json:"localClonesFound"`
			RepoContractsFound int `json:"repoContractsFound"`
			TaskProposals      int `json:"taskProposals"`
			NeedsAttention     int `json:"needsAttention"`
		} `json:"summary"`
		Repos []struct {
			Name            string `json:"name"`
			LocalCloneFound bool   `json:"localCloneFound"`
			HasRepoContract bool   `json:"hasRepoContract"`
			NeedsAttention  bool   `json:"needsAttention"`
		} `json:"repos"`
		Proposals []struct {
			ID string `json:"id"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if payload.Summary.ManagedRepos != 1 {
		t.Fatalf("managedRepos = %d, want 1", payload.Summary.ManagedRepos)
	}
	if payload.Summary.LocalClonesFound != 1 {
		t.Fatalf("localClonesFound = %d, want 1", payload.Summary.LocalClonesFound)
	}
	if payload.Summary.RepoContractsFound != 1 {
		t.Fatalf("repoContractsFound = %d, want 1", payload.Summary.RepoContractsFound)
	}
	if payload.Summary.TaskProposals != 0 {
		t.Fatalf("taskProposals = %d, want 0", payload.Summary.TaskProposals)
	}
	if payload.Summary.NeedsAttention != 0 {
		t.Fatalf("needsAttention = %d, want 0", payload.Summary.NeedsAttention)
	}
	if len(payload.Repos) != 1 {
		t.Fatalf("repos len = %d, want 1", len(payload.Repos))
	}
	if payload.Repos[0].Name != "myassistant" {
		t.Fatalf("name = %q, want myassistant", payload.Repos[0].Name)
	}
	if !payload.Repos[0].LocalCloneFound || !payload.Repos[0].HasRepoContract {
		t.Fatalf("unexpected observed state: %+v", payload.Repos[0])
	}
	if payload.Repos[0].NeedsAttention {
		t.Fatalf("expected no attention: %+v", payload.Repos[0])
	}
	if len(payload.Proposals) != 0 {
		t.Fatalf("expected no proposals: %+v", payload.Proposals)
	}
}
