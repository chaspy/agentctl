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

type applyHeader struct {
	Kind string `yaml:"kind"`
}

type managedRepoManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec managedRepoSpec `yaml:"spec"`
}

type managedRepoSpec struct {
	Repo                      string `yaml:"repo" json:"repo"`
	Role                      string `yaml:"role" json:"role"`
	Visibility                string `yaml:"visibility" json:"visibility"`
	RepoContractPath          string `yaml:"repoContractPath" json:"repoContractPath,omitempty"`
	DefaultRoutingPolicyRef   string `yaml:"defaultRoutingPolicyRef" json:"defaultRoutingPolicyRef,omitempty"`
	DefaultReviewPolicyRef    string `yaml:"defaultReviewPolicyRef" json:"defaultReviewPolicyRef,omitempty"`
	DefaultApprovalPolicyRef  string `yaml:"defaultApprovalPolicyRef" json:"defaultApprovalPolicyRef,omitempty"`
	DefaultBenchmarkPolicyRef string `yaml:"defaultBenchmarkPolicyRef" json:"defaultBenchmarkPolicyRef,omitempty"`
	Notes                     string `yaml:"notes" json:"notes,omitempty"`
}

var applyCmd = &cobra.Command{
	Use:   "apply -f <manifest>",
	Short: "Import a resource manifest into agentctl state",
	Long: `Import a desired-state manifest into agentctl's local database.

Today, only ManagedRepo resources are supported.

Examples:
  agentctl apply -f ~/go/src/github.com/chaspy/myassistant/ops/repos/book-assistant.yaml`,
	Args: cobra.NoArgs,
	RunE: runApply,
}

func init() {
	rootCmd.AddCommand(applyCmd)
	applyCmd.Flags().StringVarP(&applyFile, "file", "f", "", "Manifest file to apply")
	_ = applyCmd.MarkFlagRequired("file")
}

func runApply(cmd *cobra.Command, args []string) error {
	content, err := os.ReadFile(applyFile)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var header applyHeader
	if err := yaml.Unmarshal(content, &header); err != nil {
		return fmt.Errorf("parse manifest header: %w", err)
	}

	switch strings.TrimSpace(header.Kind) {
	case "ManagedRepo":
		return applyManagedRepo(content)
	default:
		return fmt.Errorf("unsupported kind %q: only ManagedRepo is supported today", header.Kind)
	}
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

	db, err := store.Open("")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

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

	if err := store.UpsertManagedRepo(db, row); err != nil {
		return fmt.Errorf("upsert managed repo: %w", err)
	}

	fmt.Printf("Applied ManagedRepo %s -> %s\n", row.Name, row.Repository)
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
