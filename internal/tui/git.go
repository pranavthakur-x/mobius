package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const gitStatusTimeout = 1 * time.Second

// GitStatusSummary inspects the git repository at dir and returns a compact
// header label such as "Git: (main clean)" or "Git: (dev 3 modified)".
// Returns an empty string if dir is not inside a git work tree or git fails.
func GitStatusSummary(dir string) string {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain=v1", "--branch")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseGitPorcelainStatus(string(out))
}

func parseGitPorcelainStatus(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\r\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "## ") {
		return ""
	}
	branch := parseGitBranchHeader(strings.TrimPrefix(lines[0], "## "))
	if branch == "" {
		return ""
	}

	modified := 0
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) != "" {
			modified++
		}
	}
	if modified == 0 {
		return fmt.Sprintf("Git: (%s clean)", branch)
	}
	return fmt.Sprintf("Git: (%s %d modified)", branch, modified)
}

func parseGitBranchHeader(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	// Unborn branch: "No commits yet on main" or "Initial commit on main"
	for _, prefix := range []string{"No commits yet on ", "Initial commit on "} {
		if strings.HasPrefix(header, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(header, prefix))
		}
	}
	// Detached HEAD: "HEAD (no branch)"
	if strings.HasPrefix(header, "HEAD ") || header == "HEAD" {
		return "HEAD"
	}
	// Standard tracking header: "main...origin/main [ahead 1]"
	if idx := strings.Index(header, "..."); idx >= 0 {
		return strings.TrimSpace(header[:idx])
	}
	// Branch with extra info separated by space
	if idx := strings.IndexByte(header, ' '); idx >= 0 {
		return strings.TrimSpace(header[:idx])
	}
	return header
}
