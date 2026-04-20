package controlplane

import (
	"database/sql"
	"sort"

	"github.com/chaspy/agentctl/internal/store"
)

const (
	RepoProfileSourceManagedRepo = "managed_repo"
	RepoProfileSourceRepoConfig  = "repo_config"
	RepoProfileSourceDefault     = "default"
)

type RepoProfile struct {
	Repo              string           `json:"repo"`
	PrimarySource     string           `json:"primarySource"`
	HasManagedRepo    bool             `json:"hasManagedRepo"`
	HasRepoConfig     bool             `json:"hasRepoConfig"`
	Mode              string           `json:"mode"`
	ModeSource        string           `json:"modeSource"`
	Agent             string           `json:"agent"`
	AgentSource       string           `json:"agentSource"`
	Description       string           `json:"description,omitempty"`
	DescriptionSource string           `json:"descriptionSource,omitempty"`
	UpdatedAt         string           `json:"updatedAt,omitempty"`
	ManagedRepo       *ManagedRepoView `json:"managedRepo,omitempty"`
}

func GetRepoProfile(db *sql.DB, repo string) (*RepoProfile, error) {
	managedRepo, err := store.GetManagedRepoByRepository(db, repo)
	if err != nil {
		return nil, err
	}
	repoConfig, err := store.GetRepoFullConfig(db, repo)
	if err != nil {
		return nil, err
	}
	if managedRepo == nil && repoConfig == nil {
		return nil, nil
	}
	return buildRepoProfile(repo, managedRepo, repoConfig), nil
}

func ListRepoProfiles(db *sql.DB) ([]RepoProfile, error) {
	managedRepos, err := store.ListManagedRepos(db)
	if err != nil {
		return nil, err
	}
	repoConfigs, err := store.ListRepoConfigs(db)
	if err != nil {
		return nil, err
	}

	repoSet := make(map[string]struct{}, len(managedRepos)+len(repoConfigs))
	managedByRepo := make(map[string]*store.ManagedRepo, len(managedRepos))
	configByRepo := make(map[string]*store.RepoConfig, len(repoConfigs))

	for i := range managedRepos {
		repo := managedRepos[i].Repository
		repoSet[repo] = struct{}{}
		repoCopy := managedRepos[i]
		managedByRepo[repo] = &repoCopy
	}
	for i := range repoConfigs {
		repo := repoConfigs[i].Repo
		repoSet[repo] = struct{}{}
		cfgCopy := repoConfigs[i]
		configByRepo[repo] = &cfgCopy
	}

	repos := make([]string, 0, len(repoSet))
	for repo := range repoSet {
		repos = append(repos, repo)
	}
	sort.Strings(repos)

	profiles := make([]RepoProfile, 0, len(repos))
	for _, repo := range repos {
		profiles = append(profiles, *buildRepoProfile(repo, managedByRepo[repo], configByRepo[repo]))
	}
	return profiles, nil
}

func buildRepoProfile(repo string, managedRepo *store.ManagedRepo, repoConfig *store.RepoConfig) *RepoProfile {
	profile := &RepoProfile{
		Repo:              repo,
		PrimarySource:     RepoProfileSourceDefault,
		Mode:              "branch",
		ModeSource:        RepoProfileSourceDefault,
		Agent:             "auto",
		AgentSource:       RepoProfileSourceDefault,
		DescriptionSource: RepoProfileSourceDefault,
	}

	if managedRepo != nil {
		profile.HasManagedRepo = true
		profile.PrimarySource = RepoProfileSourceManagedRepo
		view := BuildManagedRepoView(*managedRepo)
		profile.ManagedRepo = &view
		if view.Notes != "" {
			profile.Description = view.Notes
			profile.DescriptionSource = RepoProfileSourceManagedRepo
		}
		profile.UpdatedAt = managedRepo.UpdatedAt
	}

	if repoConfig != nil {
		profile.HasRepoConfig = true
		if !profile.HasManagedRepo {
			profile.PrimarySource = RepoProfileSourceRepoConfig
		}
		if repoConfig.Mode != "" {
			profile.Mode = repoConfig.Mode
			profile.ModeSource = RepoProfileSourceRepoConfig
		}
		if repoConfig.Agent != "" {
			profile.Agent = repoConfig.Agent
			profile.AgentSource = RepoProfileSourceRepoConfig
		}
		if repoConfig.Description != "" {
			profile.Description = repoConfig.Description
			profile.DescriptionSource = RepoProfileSourceRepoConfig
		}
		if profile.UpdatedAt == "" || repoConfig.UpdatedAt > profile.UpdatedAt {
			profile.UpdatedAt = repoConfig.UpdatedAt
		}
	}

	return profile
}
