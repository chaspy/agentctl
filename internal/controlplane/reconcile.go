package controlplane

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chaspy/agentctl/internal/store"
)

type ManagedRepoSelfHostingSpec struct {
	CanModifyDocs                    bool     `json:"canModifyDocs,omitempty"`
	CanModifyOpsSpecs                bool     `json:"canModifyOpsSpecs,omitempty"`
	CanModifyRuntimeCode             bool     `json:"canModifyRuntimeCode,omitempty"`
	CanModifyApprovalPolicy          bool     `json:"canModifyApprovalPolicy,omitempty"`
	RequiresHumanApprovalBeforeMerge bool     `json:"requiresHumanApprovalBeforeMerge,omitempty"`
	RequiresExtraReviewFor           []string `json:"requiresExtraReviewFor,omitempty"`
}

type ManagedRepoView struct {
	Name                      string                     `json:"name"`
	Repository                string                     `json:"repository"`
	SourceRepoRef             string                     `json:"sourceRepoRef,omitempty"`
	Tier                      string                     `json:"tier,omitempty"`
	Role                      string                     `json:"role"`
	Visibility                string                     `json:"visibility"`
	RepoContractPath          string                     `json:"repoContractPath,omitempty"`
	DefaultRoutingPolicyRef   string                     `json:"defaultRoutingPolicyRef,omitempty"`
	DefaultReviewPolicyRef    string                     `json:"defaultReviewPolicyRef,omitempty"`
	DefaultApprovalPolicyRef  string                     `json:"defaultApprovalPolicyRef,omitempty"`
	DefaultBenchmarkPolicyRef string                     `json:"defaultBenchmarkPolicyRef,omitempty"`
	DefaultReleaseGateRef     string                     `json:"defaultReleaseGateRef,omitempty"`
	SelfHosting               ManagedRepoSelfHostingSpec `json:"selfHosting,omitempty"`
	Notes                     string                     `json:"notes,omitempty"`
	SourcePath                string                     `json:"sourcePath,omitempty"`
	SourceCommit              string                     `json:"sourceCommit,omitempty"`
	SpecHash                  string                     `json:"specHash,omitempty"`
	CreatedAt                 string                     `json:"createdAt,omitempty"`
	UpdatedAt                 string                     `json:"updatedAt,omitempty"`
	SpecParseError            string                     `json:"specParseError,omitempty"`
}

type ReconcileSummary struct {
	ManagedRepos       int `json:"managedRepos"`
	LocalClonesFound   int `json:"localClonesFound"`
	RepoContractsFound int `json:"repoContractsFound"`
	NeedsAttention     int `json:"needsAttention"`
}

type ReconcileManagedRepoStatus struct {
	Name             string   `json:"name"`
	Repository       string   `json:"repository"`
	Tier             string   `json:"tier,omitempty"`
	Role             string   `json:"role,omitempty"`
	LocalPath        string   `json:"localPath,omitempty"`
	LocalCloneFound  bool     `json:"localCloneFound"`
	RepoContractPath string   `json:"repoContractPath,omitempty"`
	HasRepoContract  bool     `json:"hasRepoContract"`
	NeedsAttention   bool     `json:"needsAttention"`
	Issues           []string `json:"issues,omitempty"`
}

type ReconcileReport struct {
	Mode    string                       `json:"mode"`
	Summary ReconcileSummary             `json:"summary"`
	Repos   []ReconcileManagedRepoStatus `json:"repos"`
}

type managedRepoSpecSnapshot struct {
	Repo                      string                     `json:"repo"`
	Tier                      string                     `json:"tier,omitempty"`
	Role                      string                     `json:"role"`
	Visibility                string                     `json:"visibility"`
	RepoContractPath          string                     `json:"repoContractPath,omitempty"`
	DefaultRoutingPolicyRef   string                     `json:"defaultRoutingPolicyRef,omitempty"`
	DefaultReviewPolicyRef    string                     `json:"defaultReviewPolicyRef,omitempty"`
	DefaultApprovalPolicyRef  string                     `json:"defaultApprovalPolicyRef,omitempty"`
	DefaultBenchmarkPolicyRef string                     `json:"defaultBenchmarkPolicyRef,omitempty"`
	DefaultReleaseGateRef     string                     `json:"defaultReleaseGateRef,omitempty"`
	SelfHosting               ManagedRepoSelfHostingSpec `json:"selfHosting,omitempty"`
	Notes                     string                     `json:"notes,omitempty"`
}

func BuildManagedRepoView(repo store.ManagedRepo) ManagedRepoView {
	view := ManagedRepoView{
		Name:                      repo.Name,
		Repository:                repo.Repository,
		SourceRepoRef:             repo.SourceRepoRef,
		Role:                      repo.Role,
		Visibility:                repo.Visibility,
		RepoContractPath:          repo.RepoContractPath,
		DefaultRoutingPolicyRef:   repo.DefaultRoutingPolicyRef,
		DefaultReviewPolicyRef:    repo.DefaultReviewPolicyRef,
		DefaultApprovalPolicyRef:  repo.DefaultApprovalPolicyRef,
		DefaultBenchmarkPolicyRef: repo.DefaultBenchmarkPolicyRef,
		Notes:                     repo.Notes,
		SourcePath:                repo.SourcePath,
		SourceCommit:              repo.SourceCommit,
		SpecHash:                  repo.SpecHash,
		CreatedAt:                 repo.CreatedAt,
		UpdatedAt:                 repo.UpdatedAt,
	}

	if strings.TrimSpace(repo.RawSpecJSON) == "" {
		return view
	}

	var spec managedRepoSpecSnapshot
	if err := json.Unmarshal([]byte(repo.RawSpecJSON), &spec); err != nil {
		view.SpecParseError = err.Error()
		return view
	}

	if view.Role == "" {
		view.Role = spec.Role
	}
	if view.Visibility == "" {
		view.Visibility = spec.Visibility
	}
	if view.RepoContractPath == "" {
		view.RepoContractPath = spec.RepoContractPath
	}
	if view.DefaultRoutingPolicyRef == "" {
		view.DefaultRoutingPolicyRef = spec.DefaultRoutingPolicyRef
	}
	if view.DefaultReviewPolicyRef == "" {
		view.DefaultReviewPolicyRef = spec.DefaultReviewPolicyRef
	}
	if view.DefaultApprovalPolicyRef == "" {
		view.DefaultApprovalPolicyRef = spec.DefaultApprovalPolicyRef
	}
	if view.DefaultBenchmarkPolicyRef == "" {
		view.DefaultBenchmarkPolicyRef = spec.DefaultBenchmarkPolicyRef
	}
	if view.Notes == "" {
		view.Notes = spec.Notes
	}
	view.Tier = spec.Tier
	view.DefaultReleaseGateRef = spec.DefaultReleaseGateRef
	view.SelfHosting = spec.SelfHosting
	return view
}

func ListManagedRepoViews(db *sql.DB) ([]ManagedRepoView, error) {
	repos, err := store.ListManagedRepos(db)
	if err != nil {
		return nil, err
	}
	views := make([]ManagedRepoView, 0, len(repos))
	for _, repo := range repos {
		views = append(views, BuildManagedRepoView(repo))
	}
	return views, nil
}

func BuildReconcileReport(db *sql.DB) (*ReconcileReport, error) {
	repos, err := store.ListManagedRepos(db)
	if err != nil {
		return nil, fmt.Errorf("listing managed repos: %w", err)
	}

	report := &ReconcileReport{
		Mode: "read-only",
		Summary: ReconcileSummary{
			ManagedRepos: len(repos),
		},
		Repos: make([]ReconcileManagedRepoStatus, 0, len(repos)),
	}

	for _, repo := range repos {
		status := observeManagedRepo(repo)
		if status.LocalCloneFound {
			report.Summary.LocalClonesFound++
		}
		if status.HasRepoContract {
			report.Summary.RepoContractsFound++
		}
		if status.NeedsAttention {
			report.Summary.NeedsAttention++
		}
		report.Repos = append(report.Repos, status)
	}

	return report, nil
}

func observeManagedRepo(repo store.ManagedRepo) ReconcileManagedRepoStatus {
	view := BuildManagedRepoView(repo)
	status := ReconcileManagedRepoStatus{
		Name:             view.Name,
		Repository:       view.Repository,
		Tier:             view.Tier,
		Role:             view.Role,
		RepoContractPath: view.RepoContractPath,
	}

	entry, err := resolveRepoPath(view.Repository)
	if err != nil {
		status.Issues = append(status.Issues, fmt.Sprintf("local clone not found: %v", err))
	} else {
		status.LocalCloneFound = true
		status.LocalPath = entry.fullPath
	}

	contractPath := strings.TrimSpace(view.RepoContractPath)
	if contractPath == "" {
		status.Issues = append(status.Issues, "repoContractPath is not configured")
		status.NeedsAttention = true
		return status
	}

	if !status.LocalCloneFound {
		status.NeedsAttention = true
		return status
	}

	observedContract := filepath.Join(status.LocalPath, filepath.FromSlash(contractPath))
	info, err := os.Stat(observedContract)
	switch {
	case err == nil && !info.IsDir():
		status.HasRepoContract = true
	case err == nil && info.IsDir():
		status.Issues = append(status.Issues, fmt.Sprintf("repo contract path is a directory: %s", contractPath))
	case os.IsNotExist(err):
		status.Issues = append(status.Issues, fmt.Sprintf("repo contract not found: %s", contractPath))
	default:
		status.Issues = append(status.Issues, fmt.Sprintf("repo contract check failed: %v", err))
	}

	status.NeedsAttention = len(status.Issues) > 0
	return status
}

type repoEntry struct {
	shortName string
	fullPath  string
}

func listRepos() ([]repoEntry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	base := filepath.Join(home, "go", "src", "github.com")

	orgs, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", base, err)
	}

	var repos []repoEntry
	for _, org := range orgs {
		if !org.IsDir() {
			continue
		}
		orgPath := filepath.Join(base, org.Name())
		entries, err := os.ReadDir(orgPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			repos = append(repos, repoEntry{
				shortName: org.Name() + "/" + entry.Name(),
				fullPath:  filepath.Join(orgPath, entry.Name()),
			})
		}
	}
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].shortName < repos[j].shortName
	})
	return repos, nil
}

func resolveRepoPath(query string) (repoEntry, error) {
	repos, err := listRepos()
	if err != nil {
		return repoEntry{}, err
	}

	q := strings.ToLower(strings.TrimSpace(query))
	for _, repo := range repos {
		if strings.ToLower(repo.shortName) == q {
			return repo, nil
		}
	}

	var candidates []repoEntry
	for _, repo := range repos {
		if strings.Contains(strings.ToLower(repo.shortName), q) {
			candidates = append(candidates, repo)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) > 1 {
		names := make([]string, len(candidates))
		for i, candidate := range candidates {
			names[i] = candidate.shortName
		}
		return repoEntry{}, fmt.Errorf("ambiguous query %q, matches: %s", query, strings.Join(names, ", "))
	}
	return repoEntry{}, fmt.Errorf("repository matching %q not found", query)
}
