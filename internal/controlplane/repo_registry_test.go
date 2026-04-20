package controlplane

import (
	"testing"

	"github.com/chaspy/agentctl/internal/store"
)

func TestGetRepoProfileManagedRepoPreferredWithLegacyOverrides(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:        "myassistant",
		Repository:  "chaspy/myassistant",
		Role:        "personal-ops-console",
		Visibility:  "private",
		Notes:       "managed repo note",
		UpdatedAt:   "2026-04-20T10:00:00Z",
		RawSpecJSON: `{"repo":"github.com/chaspy/myassistant","tier":"control-plane","role":"personal-ops-console","visibility":"private","notes":"managed repo note"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}
	if err := store.SetRepoConfig(db, "chaspy/myassistant", "main"); err != nil {
		t.Fatalf("SetRepoConfig: %v", err)
	}
	if err := store.SetRepoAgent(db, "chaspy/myassistant", "codex"); err != nil {
		t.Fatalf("SetRepoAgent: %v", err)
	}
	if err := store.SetRepoDescription(db, "chaspy/myassistant", "legacy override"); err != nil {
		t.Fatalf("SetRepoDescription: %v", err)
	}

	profile, err := GetRepoProfile(db, "chaspy/myassistant")
	if err != nil {
		t.Fatalf("GetRepoProfile: %v", err)
	}
	if profile == nil {
		t.Fatal("expected profile")
	}
	if profile.PrimarySource != RepoProfileSourceManagedRepo {
		t.Fatalf("primarySource = %q", profile.PrimarySource)
	}
	if !profile.HasManagedRepo || !profile.HasRepoConfig {
		t.Fatalf("expected both managed repo and repo config sources: %+v", profile)
	}
	if profile.Mode != "main" || profile.ModeSource != RepoProfileSourceRepoConfig {
		t.Fatalf("mode = %q (%s)", profile.Mode, profile.ModeSource)
	}
	if profile.Agent != "codex" || profile.AgentSource != RepoProfileSourceRepoConfig {
		t.Fatalf("agent = %q (%s)", profile.Agent, profile.AgentSource)
	}
	if profile.Description != "legacy override" || profile.DescriptionSource != RepoProfileSourceRepoConfig {
		t.Fatalf("description = %q (%s)", profile.Description, profile.DescriptionSource)
	}
	if profile.ManagedRepo == nil || profile.ManagedRepo.Tier != "control-plane" {
		t.Fatalf("managed repo view missing tier: %+v", profile.ManagedRepo)
	}
}

func TestGetRepoProfileManagedRepoOnlyFallsBackToDefaults(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:        "agentctl",
		Repository:  "chaspy/agentctl",
		Role:        "agentops-control-plane",
		Visibility:  "public",
		Notes:       "control plane note",
		RawSpecJSON: `{"repo":"github.com/chaspy/agentctl","tier":"control-plane","role":"agentops-control-plane","visibility":"public","notes":"control plane note"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}

	profile, err := GetRepoProfile(db, "chaspy/agentctl")
	if err != nil {
		t.Fatalf("GetRepoProfile: %v", err)
	}
	if profile == nil {
		t.Fatal("expected profile")
	}
	if profile.PrimarySource != RepoProfileSourceManagedRepo {
		t.Fatalf("primarySource = %q", profile.PrimarySource)
	}
	if profile.Mode != "branch" || profile.ModeSource != RepoProfileSourceDefault {
		t.Fatalf("mode = %q (%s)", profile.Mode, profile.ModeSource)
	}
	if profile.Agent != "auto" || profile.AgentSource != RepoProfileSourceDefault {
		t.Fatalf("agent = %q (%s)", profile.Agent, profile.AgentSource)
	}
	if profile.Description != "control plane note" || profile.DescriptionSource != RepoProfileSourceManagedRepo {
		t.Fatalf("description = %q (%s)", profile.Description, profile.DescriptionSource)
	}
}

func TestListRepoProfilesUnionsManagedReposAndLegacyConfigs(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer db.Close()

	if err := store.UpsertManagedRepo(db, &store.ManagedRepo{
		Name:        "myassistant",
		Repository:  "chaspy/myassistant",
		Role:        "personal-ops-console",
		Visibility:  "private",
		RawSpecJSON: `{"repo":"github.com/chaspy/myassistant","tier":"control-plane","role":"personal-ops-console","visibility":"private"}`,
	}); err != nil {
		t.Fatalf("UpsertManagedRepo: %v", err)
	}
	if err := store.SetRepoConfig(db, "chaspy/legacy-only", "main"); err != nil {
		t.Fatalf("SetRepoConfig: %v", err)
	}

	profiles, err := ListRepoProfiles(db)
	if err != nil {
		t.Fatalf("ListRepoProfiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles len = %d, want 2", len(profiles))
	}
	if profiles[0].Repo != "chaspy/legacy-only" || profiles[1].Repo != "chaspy/myassistant" {
		t.Fatalf("unexpected repo order: %+v", profiles)
	}
	if profiles[0].PrimarySource != RepoProfileSourceRepoConfig {
		t.Fatalf("legacy-only primarySource = %q", profiles[0].PrimarySource)
	}
	if profiles[1].PrimarySource != RepoProfileSourceManagedRepo {
		t.Fatalf("managed repo primarySource = %q", profiles[1].PrimarySource)
	}
}
