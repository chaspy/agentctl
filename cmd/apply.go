package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chaspy/agentctl/internal/store"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var applyFile string
var applyDryRun bool

var applyCmd = &cobra.Command{
	Use:   "apply -f <manifest>",
	Short: "Import a resource manifest into agentctl state",
	Long: `Import a desired-state manifest into agentctl's local database.

Supported kinds today:
  - ManagedRepo
  - AgentOpsEcosystem
  - SelfHostingPolicy
  - RoutingPolicy
  - ReviewPolicy
  - ApprovalPolicy
  - BenchmarkPolicy
  - ExperienceProposal
  - ReleaseGate
  - AgentTask

Examples:
  agentctl apply -f ~/go/src/github.com/chaspy/myassistant/ops/repos/book-assistant.yaml`,
	Args: cobra.NoArgs,
	RunE: runApply,
}

func init() {
	rootCmd.AddCommand(applyCmd)
	applyCmd.Flags().StringVarP(&applyFile, "file", "f", "", "Manifest file to apply")
	applyCmd.Flags().BoolVar(&applyDryRun, "dry-run", false, "Parse and validate the manifest without writing to the database")
	_ = applyCmd.MarkFlagRequired("file")
}

func runApply(cmd *cobra.Command, args []string) error {
	content, err := os.ReadFile(applyFile)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	validation := validateManifestContent(content)
	if !validation.Valid {
		return fmt.Errorf("manifest validation failed: %s", strings.Join(validation.Errors, "; "))
	}

	switch validation.Kind {
	case resourceKindManagedRepo:
		return applyManagedRepo(content)
	case resourceKindAgentOpsEcosystem,
		resourceKindSelfHostingPolicy,
		resourceKindRoutingPolicy,
		resourceKindReviewPolicy,
		resourceKindApprovalPolicy,
		resourceKindBenchmarkPolicy,
		resourceKindExperienceProposal,
		resourceKindReleaseGate:
		return applyControlPlaneResource(content, validation.Kind)
	case resourceKindAgentTask:
		return applyAgentTask(content)
	default:
		return fmt.Errorf("unsupported kind %q for apply", validation.Kind)
	}
}

type genericControlPlaneResourceManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec any `yaml:"spec"`
}

func applyControlPlaneResource(content []byte, kind string) error {
	var manifest genericControlPlaneResourceManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return fmt.Errorf("parse %s: %w", kind, err)
	}

	name := strings.TrimSpace(manifest.Metadata.Name)
	if name == "" {
		return fmt.Errorf("%s metadata.name is required", kind)
	}

	specJSON, err := json.Marshal(manifest.Spec)
	if err != nil {
		return fmt.Errorf("marshal %s spec: %w", kind, err)
	}

	absPath, err := filepath.Abs(applyFile)
	if err != nil {
		return fmt.Errorf("resolve manifest path: %w", err)
	}

	row := &store.ControlPlaneResource{
		Kind:         kind,
		Name:         name,
		APIVersion:   strings.TrimSpace(manifest.APIVersion),
		SourcePath:   absPath,
		SourceCommit: gitHeadForPath(absPath),
		SpecHash:     sha256Hex(content),
		RawSpecJSON:  string(specJSON),
	}

	if applyDryRun {
		fmt.Printf("Validated %s %s (dry-run)\n", row.Kind, row.Name)
		if row.SourceCommit != "" {
			fmt.Printf("Source commit: %s\n", row.SourceCommit)
		}
		return nil
	}

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if err := store.UpsertControlPlaneResource(db, row); err != nil {
		return fmt.Errorf("upsert %s: %w", kind, err)
	}

	fmt.Printf("Applied %s %s\n", row.Kind, row.Name)
	if row.SourceCommit != "" {
		fmt.Printf("Source commit: %s\n", row.SourceCommit)
	}
	return nil
}

func applyManagedRepo(content []byte) error {
	var manifest managedRepoManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return fmt.Errorf("parse ManagedRepo: %w", err)
	}

	if strings.TrimSpace(manifest.Metadata.Name) == "" {
		return fmt.Errorf("ManagedRepo metadata.name is required")
	}
	if strings.TrimSpace(manifest.Spec.Repo) == "" {
		return fmt.Errorf("ManagedRepo spec.repo is required")
	}

	repository, err := normalizeManagedRepoRef(manifest.Spec.Repo)
	if err != nil {
		return err
	}

	specJSON, err := json.Marshal(manifest.Spec)
	if err != nil {
		return fmt.Errorf("marshal ManagedRepo spec: %w", err)
	}

	absPath, err := filepath.Abs(applyFile)
	if err != nil {
		return fmt.Errorf("resolve manifest path: %w", err)
	}

	row := &store.ManagedRepo{
		Name:                      manifest.Metadata.Name,
		Repository:                repository,
		SourceRepoRef:             strings.TrimSpace(manifest.Spec.Repo),
		Role:                      strings.TrimSpace(manifest.Spec.Role),
		Visibility:                strings.TrimSpace(manifest.Spec.Visibility),
		RepoContractPath:          strings.TrimSpace(manifest.Spec.RepoContractPath),
		DefaultRoutingPolicyRef:   strings.TrimSpace(manifest.Spec.DefaultRoutingPolicyRef),
		DefaultReviewPolicyRef:    strings.TrimSpace(manifest.Spec.DefaultReviewPolicyRef),
		DefaultApprovalPolicyRef:  strings.TrimSpace(manifest.Spec.DefaultApprovalPolicyRef),
		DefaultBenchmarkPolicyRef: strings.TrimSpace(manifest.Spec.DefaultBenchmarkPolicyRef),
		Notes:                     strings.TrimSpace(manifest.Spec.Notes),
		SourcePath:                absPath,
		SourceCommit:              gitHeadForPath(absPath),
		SpecHash:                  sha256Hex(content),
		RawSpecJSON:               string(specJSON),
	}

	if applyDryRun {
		fmt.Printf("Validated ManagedRepo %s -> %s (dry-run)\n", row.Name, row.Repository)
		if row.SourceCommit != "" {
			fmt.Printf("Source commit: %s\n", row.SourceCommit)
		}
		return nil
	}

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if err := store.UpsertManagedRepo(db, row); err != nil {
		return fmt.Errorf("upsert managed repo: %w", err)
	}

	fmt.Printf("Applied ManagedRepo %s -> %s\n", row.Name, row.Repository)
	if row.SourceCommit != "" {
		fmt.Printf("Source commit: %s\n", row.SourceCommit)
	}
	return nil
}

func applyAgentTask(content []byte) error {
	var manifest agentTaskManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return fmt.Errorf("parse AgentTask: %w", err)
	}

	name := strings.TrimSpace(manifest.Metadata.Name)
	if name == "" {
		return fmt.Errorf("AgentTask metadata.name is required")
	}
	repoRef := strings.TrimSpace(manifest.Spec.RepoRef)
	if repoRef == "" {
		return fmt.Errorf("AgentTask spec.repoRef is required")
	}

	specJSON, err := json.Marshal(manifest.Spec)
	if err != nil {
		return fmt.Errorf("marshal AgentTask spec: %w", err)
	}

	absPath, err := filepath.Abs(applyFile)
	if err != nil {
		return fmt.Errorf("resolve manifest path: %w", err)
	}

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	managedRepo, err := store.GetManagedRepo(db, repoRef)
	if err != nil {
		return fmt.Errorf("resolve repoRef %s: %w", repoRef, err)
	}
	if managedRepo == nil {
		return fmt.Errorf("AgentTask repoRef %q is not applied in managed_repos", repoRef)
	}

	row := &store.AgentTask{
		Name:                        name,
		RepoRef:                     repoRef,
		Repository:                  managedRepo.Repository,
		Objective:                   strings.TrimSpace(manifest.Spec.Objective),
		TaskType:                    strings.TrimSpace(manifest.Spec.TaskType),
		Risk:                        strings.TrimSpace(manifest.Spec.Risk),
		ContextRefs:                 append([]string(nil), manifest.Spec.ContextRefs...),
		DesiredOutcome:              append([]string(nil), manifest.Spec.DesiredOutcome...),
		RoutingPolicyRef:            strings.TrimSpace(manifest.Spec.RoutingPolicyRef),
		ReviewPolicyRef:             strings.TrimSpace(manifest.Spec.ReviewPolicyRef),
		ApprovalPolicyRef:           strings.TrimSpace(manifest.Spec.ApprovalPolicyRef),
		ApprovalRequiredBeforeMerge: manifest.Spec.Approval.RequiredBeforeMerge,
		SourceKind:                  "manifest",
		SourceRef:                   name,
		Status:                      "planned",
		SourcePath:                  absPath,
		SourceCommit:                gitHeadForPath(absPath),
		SpecHash:                    sha256Hex(content),
		RawSpecJSON:                 string(specJSON),
	}

	if applyDryRun {
		fmt.Printf("Validated AgentTask %s -> %s (dry-run)\n", row.Name, row.RepoRef)
		if row.SourceCommit != "" {
			fmt.Printf("Source commit: %s\n", row.SourceCommit)
		}
		return nil
	}

	if err := store.UpsertAgentTask(db, row); err != nil {
		return fmt.Errorf("upsert agent task: %w", err)
	}

	fmt.Printf("Applied AgentTask %s -> %s\n", row.Name, row.RepoRef)
	if row.SourceCommit != "" {
		fmt.Printf("Source commit: %s\n", row.SourceCommit)
	}
	return nil
}

func normalizeManagedRepoRef(value string) (string, error) {
	repo := strings.TrimSpace(value)

	switch {
	case strings.HasPrefix(repo, "git@github.com:"):
		repo = strings.TrimPrefix(repo, "git@github.com:")
	case strings.HasPrefix(repo, "https://github.com/"):
		repo = strings.TrimPrefix(repo, "https://github.com/")
	case strings.HasPrefix(repo, "http://github.com/"):
		repo = strings.TrimPrefix(repo, "http://github.com/")
	case strings.HasPrefix(repo, "github.com/"):
		repo = strings.TrimPrefix(repo, "github.com/")
	}

	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.Trim(repo, "/")

	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("invalid ManagedRepo spec.repo %q: expected owner/repo or github.com/owner/repo", value)
	}
	return parts[0] + "/" + parts[1], nil
}

func gitHeadForPath(path string) string {
	dir := filepath.Dir(path)
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
