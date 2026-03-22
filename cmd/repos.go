package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var reposCmd = &cobra.Command{
	Use:   "repos [query]",
	Short: "List repositories under GOPATH (~/go/src/github.com/)",
	Long:  "Scans ~/go/src/github.com/<org>/<repo> and lists available repositories. Optionally filter by substring.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runRepos,
}

func init() {
	rootCmd.AddCommand(reposCmd)
}

func runRepos(cmd *cobra.Command, args []string) error {
	query := ""
	if len(args) > 0 {
		query = strings.ToLower(args[0])
	}

	repos, err := listRepos()
	if err != nil {
		return err
	}

	for _, r := range repos {
		if query != "" && !strings.Contains(strings.ToLower(r.ShortName), query) {
			continue
		}
		fmt.Printf("%s\t%s\n", r.ShortName, r.FullPath)
	}
	return nil
}

// RepoEntry represents a discovered repository.
type RepoEntry struct {
	ShortName string // e.g. "chaspy/myrepo"
	FullPath  string // e.g. "/Users/user/repos/owner/myrepo"
}

func listRepos() ([]RepoEntry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	base := filepath.Join(home, "go", "src", "github.com")

	orgs, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", base, err)
	}

	var repos []RepoEntry
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
			repos = append(repos, RepoEntry{
				ShortName: org.Name() + "/" + entry.Name(),
				FullPath:  filepath.Join(orgPath, entry.Name()),
			})
		}
	}
	return repos, nil
}

// ResolveRepoPath resolves a short name (e.g. "owner/repo" or "repo")
// to its full filesystem path. Tries exact org/repo match first, then substring.
func ResolveRepoPath(query string) (RepoEntry, error) {
	repos, err := listRepos()
	if err != nil {
		return RepoEntry{}, err
	}

	q := strings.ToLower(query)

	// Exact match
	for _, r := range repos {
		if strings.ToLower(r.ShortName) == q {
			return r, nil
		}
	}

	// Substring match
	var candidates []RepoEntry
	for _, r := range repos {
		if strings.Contains(strings.ToLower(r.ShortName), q) {
			candidates = append(candidates, r)
		}
	}

	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) > 1 {
		names := make([]string, len(candidates))
		for i, c := range candidates {
			names[i] = c.ShortName
		}
		return RepoEntry{}, fmt.Errorf("ambiguous query %q, matches: %s", query, strings.Join(names, ", "))
	}

	return RepoEntry{}, fmt.Errorf("repository matching %q not found", query)
}
