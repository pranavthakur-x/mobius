package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseGitPorcelainStatus(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "clean tracked main",
			in:   "## main...origin/main\n",
			want: "Git: (main clean)",
		},
		{
			name: "dirty dev branch",
			in:   "## dev...origin/dev [ahead 1]\n M main.go\n?? new.go\nA  added.go\n",
			want: "Git: (dev 3 modified)",
		},
		{
			name: "unborn branch clean",
			in:   "## No commits yet on main\n",
			want: "Git: (main clean)",
		},
		{
			name: "unborn branch dirty",
			in:   "## No commits yet on feature/x\n?? README.md\n",
			want: "Git: (feature/x 1 modified)",
		},
		{
			name: "detached HEAD",
			in:   "## HEAD (no branch)\n M file.go\n",
			want: "Git: (HEAD 1 modified)",
		},
		{
			name: "empty output",
			in:   "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGitPorcelainStatus(tc.in)
			if got != tc.want {
				t.Fatalf("parseGitPorcelainStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGitStatusSummaryNonGitDir(t *testing.T) {
	dir := t.TempDir()
	if got := GitStatusSummary(dir); got != "" {
		t.Fatalf("expected empty status for non-git dir, got %q", got)
	}
}

func TestGitStatusSummaryRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not installed")
	}
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}

	runGit("init", "-b", "main")
	if got := GitStatusSummary(dir); got != "Git: (main clean)" {
		t.Fatalf("got %q, want %q", got, "Git: (main clean)")
	}

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := GitStatusSummary(dir); got != "Git: (main 2 modified)" {
		t.Fatalf("got %q, want %q", got, "Git: (main 2 modified)")
	}
}
