package cmd

import "testing"

func TestParseWorktreePath(t *testing.T) {
	// Typical `git worktree list --porcelain` output with two entries.
	porcelainOutput := `worktree /home/user/projects/myrepo
HEAD abc1234567890abcdef
branch refs/heads/main

worktree /home/user/projects/myrepo/worktree-feature-foo
HEAD def456789012345678
branch refs/heads/feature/foo

worktree /home/user/projects/myrepo/worktree-detached
HEAD deadbeef12345678
detached

`

	tests := []struct {
		name   string
		branch string
		want   string
	}{
		{
			name:   "main branch found in main checkout",
			branch: "main",
			want:   "/home/user/projects/myrepo",
		},
		{
			name:   "feature branch found in worktree",
			branch: "feature/foo",
			want:   "/home/user/projects/myrepo/worktree-feature-foo",
		},
		{
			name:   "non-existent branch returns empty string",
			branch: "not-there",
			want:   "",
		},
		{
			name:   "detached HEAD not matched by branch name",
			branch: "detached",
			want:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseWorktreePath(porcelainOutput, tc.branch)
			if got != tc.want {
				t.Errorf("parseWorktreePath(..., %q) = %q, want %q", tc.branch, got, tc.want)
			}
		})
	}
}

func TestWithVersionBumpReminder(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "append reminder when VERSION is absent",
			message: "この修正を実装してください。",
			want:    "この修正を実装してください。\n\n" + versionBumpReminder,
		},
		{
			name:    "skip when VERSION is already mentioned",
			message: "commit 前に VERSION を確認して更新してください。",
			want:    "commit 前に VERSION を確認して更新してください。",
		},
		{
			name:    "preserve trailing newline",
			message: "実装してください。\n",
			want:    "実装してください。\n" + versionBumpReminder,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := withVersionBumpReminder(tc.message)
			if got != tc.want {
				t.Errorf("withVersionBumpReminder(%q) = %q, want %q", tc.message, got, tc.want)
			}
		})
	}
}
