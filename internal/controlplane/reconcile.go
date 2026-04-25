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
	TaskProposals                 int `json:"taskProposals"`
	ApprovalRequiredProposals     int `json:"approvalRequiredProposals"`
	ApprovalUnknownProposals      int `json:"approvalUnknownProposals"`
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
	Mode      string                       `json:"mode"`
	Summary   ReconcileSummary             `json:"summary"`
	Repos     []ReconcileManagedRepoStatus `json:"repos"`
	Proposals []ReconcileTaskProposal      `json:"proposals,omitempty"`
}

type ReconcileTaskProposal struct {
	ID                string                 `json:"id"`
	RepoRef           string                 `json:"repoRef"`
	Repository        string                 `json:"repository"`
	Tier              string                 `json:"tier,omitempty"`
	Category          string                 `json:"category"`
	Title             string                 `json:"title"`
	Objective         string                 `json:"objective"`
	TaskType          string                 `json:"taskType"`
	Risk              string                 `json:"risk"`
	ReviewPolicyRef   string                 `json:"reviewPolicyRef,omitempty"`
	ApprovalPolicyRef string                 `json:"approvalPolicyRef,omitempty"`
	Approval          ProposalApprovalStatus `json:"approval"`
	DesiredOutcome    []string               `json:"desiredOutcome,omitempty"`
	TriggerIssues     []string               `json:"triggerIssues,omitempty"`
}

type ProposalApprovalStatus struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

const (
	proposalApprovalRequired    = "required"
	proposalApprovalNotRequired = "not_required"
	proposalApprovalUnknown     = "unknown"
)

type remoteObservation struct {
	source        string
	url           string
	repository    string
	reachable     bool
	defaultBranch string
	issues        []string
}

type proposalBuilder struct {
	db     *sql.DB
	view   ManagedRepoView
	status ReconcileManagedRepoStatus
	items  []ReconcileTaskProposal
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
		Repos:     make([]ReconcileManagedRepoStatus, 0, len(repos)),
		Proposals: make([]ReconcileTaskProposal, 0),
	}

	for _, repo := range repos {
		view := BuildManagedRepoView(repo)
		status := observeManagedRepoView(view)
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

		proposals := buildTaskProposals(db, view, status)
		report.Summary.TaskProposals += len(proposals)
		for _, proposal := range proposals {
			switch proposal.Approval.Status {
			case proposalApprovalRequired:
				report.Summary.ApprovalRequiredProposals++
			case proposalApprovalUnknown:
				report.Summary.ApprovalUnknownProposals++
			}
			report.Proposals = append(report.Proposals, proposal)
		}
	}

	return report, nil
}

func observeManagedRepo(repo store.ManagedRepo) ReconcileManagedRepoStatus {
	return observeManagedRepoView(BuildManagedRepoView(repo))
}

func observeManagedRepoView(view ManagedRepoView) ReconcileManagedRepoStatus {
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

	remote := observeRemoteState(view, status.LocalPath)
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

func buildTaskProposals(db *sql.DB, view ManagedRepoView, status ReconcileManagedRepoStatus) []ReconcileTaskProposal {
	builder := proposalBuilder{db: db, view: view, status: status}

	if !status.LocalCloneFound {
		builder.add("bootstrap_local_clone", "Bootstrap local clone", "implementation", riskForLocalClone(view),
			fmt.Sprintf("Create a local checkout for %s so reconcile and agent tasks can operate on a workspace.", view.Repository),
			[]string{
				fmt.Sprintf("A local clone exists for %s", view.Repository),
				"reconcile reports localCloneFound=true",
			},
			filterIssues(status.Issues, "local clone not found:"),
		)
	}

	if strings.TrimSpace(status.RepoContractPath) == "" {
		builder.add("configure_repo_contract_path", "Configure repo contract path", "docs", "low",
			fmt.Sprintf("Update ManagedRepo %s so repoContractPath is declared in desired state.", view.Name),
			[]string{
				"ManagedRepo spec declares repoContractPath",
				`reconcile no longer reports "repoContractPath is not configured"`,
			},
			filterIssues(status.Issues, "repoContractPath is not configured"),
		)
	} else if status.LocalCloneFound && !status.HasRepoContract {
		builder.add("create_repo_contract", "Create repo contract", "docs", "low",
			fmt.Sprintf("Add %s to %s and describe the repo contract for agentctl.", status.RepoContractPath, view.Repository),
			[]string{
				fmt.Sprintf("%s exists in the repository", status.RepoContractPath),
				"reconcile reports hasRepoContract=true",
			},
			filterIssues(status.Issues, "repo contract not found:", "repo contract path is a directory:", "repo contract check failed:"),
		)
	}

	if status.RemoteURL != "" && !status.RemoteReachable {
		builder.add("restore_remote_reachability", "Restore remote reachability", "implementation", riskForRemote(view),
			fmt.Sprintf("Investigate and restore read-only access to %s for %s.", status.RemoteURL, view.Repository),
			[]string{
				"reconcile reports remoteReachable=true",
				"remote default branch can be observed",
			},
			filterIssues(status.Issues, "remote reachability check failed:"),
		)
	}

	if status.ObservedRemoteRepo != "" && status.ObservedRemoteRepo != status.Repository {
		builder.add("align_remote_repository_reference", "Align remote repository reference", "implementation", riskForRemote(view),
			fmt.Sprintf("Align ManagedRepo.repository and the observed remote for %s.", view.Name),
			[]string{
				fmt.Sprintf("ManagedRepo.repository matches the observed remote for %s", view.Name),
				"reconcile no longer reports remote repository mismatch",
			},
			filterIssues(status.Issues, "remote points to"),
		)
	}

	return builder.items
}

func (b *proposalBuilder) add(category, title, taskType, risk, objective string, desiredOutcome, triggerIssues []string) {
	approval := inferProposalApproval(b.db, b.view, taskType, risk)
	proposal := ReconcileTaskProposal{
		ID:                proposalID(b.view.Name, category),
		RepoRef:           b.view.Name,
		Repository:        b.view.Repository,
		Tier:              b.view.Tier,
		Category:          category,
		Title:             title,
		Objective:         objective,
		TaskType:          taskType,
		Risk:              risk,
		ReviewPolicyRef:   b.view.DefaultReviewPolicyRef,
		ApprovalPolicyRef: b.view.DefaultApprovalPolicyRef,
		Approval:          approval,
		DesiredOutcome:    desiredOutcome,
		TriggerIssues:     triggerIssues,
	}
	b.items = append(b.items, proposal)
}

func inferProposalApproval(db *sql.DB, view ManagedRepoView, taskType, risk string) ProposalApprovalStatus {
	approvalEval, err := ResolveApprovalPolicy(db, view.DefaultApprovalPolicyRef, PolicyMatchContext{
		TaskType: taskType,
		Risk:     risk,
		RepoRole: view.Role,
		RepoTier: view.Tier,
	})
	if err == nil && approvalEval.PolicyFound && approvalEval.Matched {
		status := proposalApprovalNotRequired
		if approvalEval.RequiresHumanApproval {
			status = proposalApprovalRequired
		}
		reason := strings.TrimSpace(approvalEval.Reason)
		if reason == "" {
			reason = fmt.Sprintf("approval policy %q matched", approvalEval.PolicyRef)
		} else {
			reason = fmt.Sprintf("approval policy %q matched: %s", approvalEval.PolicyRef, reason)
		}
		return ProposalApprovalStatus{
			Status: status,
			Reason: reason,
		}
	}

	switch {
	case risk == "high":
		return ProposalApprovalStatus{
			Status: proposalApprovalRequired,
			Reason: "high-risk proposals require explicit human approval",
		}
	case view.SelfHosting.RequiresHumanApprovalBeforeMerge && taskType == "implementation":
		return ProposalApprovalStatus{
			Status: proposalApprovalRequired,
			Reason: "self-hosting implementation work is gated until human approval",
		}
	case view.Tier == "control-plane" && taskType == "implementation":
		return ProposalApprovalStatus{
			Status: proposalApprovalRequired,
			Reason: "control-plane implementation work is conservatively gated",
		}
	case taskType == "docs" && risk == "low":
		return ProposalApprovalStatus{
			Status: proposalApprovalNotRequired,
			Reason: "low-risk docs/config proposals do not need a task-level approval gate",
		}
	case strings.TrimSpace(view.DefaultApprovalPolicyRef) == "":
		return ProposalApprovalStatus{
			Status: proposalApprovalUnknown,
			Reason: "no defaultApprovalPolicyRef is configured",
		}
	default:
		if err == nil && approvalEval.PolicyFound {
			return ProposalApprovalStatus{
				Status: proposalApprovalUnknown,
				Reason: fmt.Sprintf("approval policy %q has no matching rule", view.DefaultApprovalPolicyRef),
			}
		}
		return ProposalApprovalStatus{
			Status: proposalApprovalUnknown,
			Reason: fmt.Sprintf("full evaluation for approval policy %q is not implemented yet", view.DefaultApprovalPolicyRef),
		}
	}
}

func riskForLocalClone(view ManagedRepoView) string {
	if view.Tier == "control-plane" {
		return "medium"
	}
	return "low"
}

func riskForRemote(view ManagedRepoView) string {
	if view.Tier == "control-plane" {
		return "high"
	}
	return "medium"
}

func filterIssues(issues []string, prefixes ...string) []string {
	if len(prefixes) == 0 {
		return append([]string(nil), issues...)
	}
	filtered := make([]string, 0, len(issues))
	for _, issue := range issues {
		for _, prefix := range prefixes {
			if strings.Contains(issue, prefix) {
				filtered = append(filtered, issue)
				break
			}
		}
	}
	return filtered
}

func proposalID(repoRef, category string) string {
	return sanitizeProposalPart(repoRef) + "-" + sanitizeProposalPart(category)
}

func sanitizeProposalPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func observeRemoteState(view ManagedRepoView, localPath string) remoteObservation {
	if strings.TrimSpace(localPath) != "" {
		if isGitRepository(localPath) {
			remoteURL, err := gitRemoteURL(localPath, "origin")
			if err != nil {
				return remoteObservation{
					issues: []string{fmt.Sprintf("origin remote check failed: %v", err)},
				}
			}
			return probeRemoteURL("origin", remoteURL, view.Repository)
		}
		return remoteObservation{}
	}

	specRemoteURL := deriveRemoteURLFromSpec(view)
	if specRemoteURL == "" {
		return remoteObservation{}
	}
	return probeRemoteURL("spec", specRemoteURL, view.Repository)
}

func probeRemoteURL(source, remoteURL, expectedRepo string) remoteObservation {
	observation := remoteObservation{
		source: source,
		url:    remoteURL,
	}

	if observedRepo := parseGitHubRepoFromRemoteURL(remoteURL); observedRepo != "" {
		observation.repository = observedRepo
		if expectedRepo != "" && observedRepo != expectedRepo {
			observation.issues = append(observation.issues,
				fmt.Sprintf("%s remote points to %s, expected %s", source, observedRepo, expectedRepo))
		}
	}

	defaultBranch, err := probeRemoteDefaultBranch(remoteURL)
	if err != nil {
		observation.issues = append(observation.issues,
			fmt.Sprintf("%s remote reachability check failed: %v", source, err))
		return observation
	}

	observation.reachable = true
	observation.defaultBranch = defaultBranch
	return observation
}

func deriveRemoteURLFromSpec(view ManagedRepoView) string {
	candidates := []string{
		strings.TrimSpace(view.SourceRepoRef),
		strings.TrimSpace(view.Repository),
	}
	for _, candidate := range candidates {
		if remoteURL := normalizeRemoteCandidate(candidate); remoteURL != "" {
			return remoteURL
		}
	}
	return ""
}

func normalizeRemoteCandidate(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ""
	}

	switch {
	case strings.HasPrefix(candidate, "git@"),
		strings.HasPrefix(candidate, "ssh://"),
		strings.HasPrefix(candidate, "https://"),
		strings.HasPrefix(candidate, "http://"),
		strings.HasPrefix(candidate, "file://"),
		strings.HasPrefix(candidate, "/"),
		strings.HasPrefix(candidate, "./"),
		strings.HasPrefix(candidate, "../"):
		return candidate
	case strings.HasPrefix(candidate, "github.com/"):
		return ensureGitSuffix("https://" + strings.TrimPrefix(candidate, "https://"))
	case strings.Count(candidate, "/") == 1:
		return ensureGitSuffix("https://github.com/" + candidate)
	default:
		return ""
	}
}

func ensureGitSuffix(remoteURL string) string {
	trimmed := strings.TrimSpace(remoteURL)
	if trimmed == "" || strings.HasSuffix(trimmed, ".git") {
		return trimmed
	}
	return trimmed + ".git"
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
