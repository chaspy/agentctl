package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	ManagedRepos                  int `json:"managedRepos"`
	LocalClonesFound              int `json:"localClonesFound"`
	RepoContractsFound            int `json:"repoContractsFound"`
	RemoteURLsResolved            int `json:"remoteURLsResolved"`
	RemoteReachable               int `json:"remoteReachable"`
	RemoteDefaultBranchesResolved int `json:"remoteDefaultBranchesResolved"`
	NeedsAttention                int `json:"needsAttention"`
}

type ReconcileManagedRepoStatus struct {
	Name                string   `json:"name"`
	Repository          string   `json:"repository"`
	Tier                string   `json:"tier,omitempty"`
	Role                string   `json:"role,omitempty"`
	LocalPath           string   `json:"localPath,omitempty"`
	LocalCloneFound     bool     `json:"localCloneFound"`
	RemoteSource        string   `json:"remoteSource,omitempty"`
	RemoteURL           string   `json:"remoteURL,omitempty"`
	ObservedRemoteRepo  string   `json:"observedRemoteRepository,omitempty"`
	RemoteReachable     bool     `json:"remoteReachable"`
	RemoteDefaultBranch string   `json:"remoteDefaultBranch,omitempty"`
	RepoContractPath    string   `json:"repoContractPath,omitempty"`
	HasRepoContract     bool     `json:"hasRepoContract"`
	NeedsAttention      bool     `json:"needsAttention"`
	Issues              []string `json:"issues,omitempty"`
}

type ReconcileReport struct {
	Mode    string                       `json:"mode"`
	Summary ReconcileSummary             `json:"summary"`
	Repos   []ReconcileManagedRepoStatus `json:"repos"`
}

type remoteObservation struct {
	source        string
	url           string
	repository    string
	reachable     bool
	defaultBranch string
	issues        []string
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
		if status.RemoteURL != "" {
			report.Summary.RemoteURLsResolved++
		}
		if status.RemoteReachable {
			report.Summary.RemoteReachable++
		}
		if status.RemoteDefaultBranch != "" {
			report.Summary.RemoteDefaultBranchesResolved++
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

	remote := observeRemoteState(view.Repository, status.LocalPath)
	status.RemoteSource = remote.source
	status.RemoteURL = remote.url
	status.ObservedRemoteRepo = remote.repository
	status.RemoteReachable = remote.reachable
	status.RemoteDefaultBranch = remote.defaultBranch
	status.Issues = append(status.Issues, remote.issues...)

	contractPath := strings.TrimSpace(view.RepoContractPath)
	if contractPath == "" {
		status.Issues = append(status.Issues, "repoContractPath is not configured")
	} else if status.LocalCloneFound {
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
	}

	status.NeedsAttention = len(status.Issues) > 0
	return status
}

func observeRemoteState(expectedRepo, localPath string) remoteObservation {
	if strings.TrimSpace(localPath) == "" || !isGitRepository(localPath) {
		return remoteObservation{}
	}

	remoteURL, err := gitRemoteURL(localPath, "origin")
	if err != nil {
		return remoteObservation{
			issues: []string{fmt.Sprintf("origin remote check failed: %v", err)},
		}
	}

	observation := remoteObservation{
		source: "origin",
		url:    remoteURL,
	}

	if observedRepo := parseGitHubRepoFromRemoteURL(remoteURL); observedRepo != "" {
		observation.repository = observedRepo
		if expectedRepo != "" && observedRepo != expectedRepo {
			observation.issues = append(observation.issues,
				fmt.Sprintf("origin remote points to %s, expected %s", observedRepo, expectedRepo))
		}
	}

	defaultBranch, err := probeRemoteDefaultBranch(remoteURL)
	if err != nil {
		observation.issues = append(observation.issues,
			fmt.Sprintf("remote reachability check failed: %v", err))
		return observation
	}

	observation.reachable = true
	observation.defaultBranch = defaultBranch
	return observation
}

func isGitRepository(localPath string) bool {
	out, err := runGitCommand(localPath, 2*time.Second, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func gitRemoteURL(localPath, remoteName string) (string, error) {
	out, err := runGitCommand(localPath, 3*time.Second, "remote", "get-url", remoteName)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func probeRemoteDefaultBranch(remoteURL string) (string, error) {
	out, err := runGitCommand("", 5*time.Second, "ls-remote", "--symref", remoteURL, "HEAD")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ref: refs/heads/") && strings.HasSuffix(line, "\tHEAD") {
			branch := strings.TrimPrefix(line, "ref: refs/heads/")
			branch = strings.TrimSuffix(branch, "\tHEAD")
			return branch, nil
		}
	}
	return "", nil
}

func runGitCommand(dir string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return out, nil
}

func parseGitHubRepoFromRemoteURL(remoteURL string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(remoteURL), ".git")
	switch {
	case strings.HasPrefix(trimmed, "git@github.com:"):
		trimmed = strings.TrimPrefix(trimmed, "git@github.com:")
	case strings.HasPrefix(trimmed, "ssh://git@github.com/"):
		trimmed = strings.TrimPrefix(trimmed, "ssh://git@github.com/")
	case strings.HasPrefix(trimmed, "https://github.com/"):
		trimmed = strings.TrimPrefix(trimmed, "https://github.com/")
	case strings.HasPrefix(trimmed, "http://github.com/"):
		trimmed = strings.TrimPrefix(trimmed, "http://github.com/")
	default:
		return ""
	}

	segments := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(segments) < 2 {
		return ""
	}
	return segments[len(segments)-2] + "/" + segments[len(segments)-1]
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
