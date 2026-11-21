package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGrepSearchSkipsTelemetryDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "router.go"), "func (h *Handler) HandleFunc() {}\n")
	writeFile(t, filepath.Join(root, ".mobius", "artifacts", "old.txt"), strings.Repeat("HandleFunc in dump\n", 40))
	writeFile(t, filepath.Join(root, ".git", "config"), "HandleFunc\n")
	writeFile(t, filepath.Join(root, ".cursor", "notes.txt"), "HandleFunc\n")
	writeFile(t, filepath.Join(root, "vendor", "mod", "x.go"), "HandleFunc\n")
	writeFile(t, filepath.Join(root, "node_modules", "pkg", "index.js"), "HandleFunc\n")

	tool := NewGrepTool(root)
	out, err := tool.Execute(context.Background(), `{"pattern":"HandleFunc"}`)
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, "pkg/router.go") {
		t.Fatalf("missing source hit: %s", out)
	}
	for _, banned := range []string{".mobius", ".git", ".cursor", "vendor", "node_modules"} {
		if strings.Contains(out, banned) {
			t.Fatalf("search leaked %s: %s", banned, out)
		}
	}
}

func TestGrepSearchRejectsExplicitTelemetryPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".mobius", "artifacts", "old.txt"), "HandleFunc\n")

	tool := NewGrepTool(root)
	out, err := tool.Execute(context.Background(), `{"pattern":"HandleFunc","path":".mobius/artifacts"}`)
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, "Search skipped") {
		t.Fatalf("wanted skip message, got %s", out)
	}
	if strings.Contains(out, "old.txt") {
		t.Fatalf("explicit .mobius path was searched: %s", out)
	}
}

func TestGrepSearchCapsMatches(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	for i := 0; i < 80; i++ {
		b.WriteString("needle line\n")
	}
	writeFile(t, filepath.Join(root, "pkg", "many.go"), b.String())

	tool := NewGrepTool(root)
	out, err := tool.Execute(context.Background(), `{"pattern":"needle","path":"pkg"}`)
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, "capped at 50 matches") {
		t.Fatalf("wanted cap notice, got %s", out)
	}
	if strings.Count(out, "needle line") > 50 {
		t.Fatalf("too many hits: %s", out)
	}
}

func TestGrepSearchConcurrent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "a.go"), "alpha token\n")
	writeFile(t, filepath.Join(root, ".mobius", "dump.txt"), "alpha token\n")

	tool := NewGrepTool(root)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	var failed atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				out, err := tool.Execute(ctx, `{"pattern":"alpha token"}`)
				if ctx.Err() != nil {
					return
				}
				if err != nil || !strings.Contains(out, "pkg/a.go") || strings.Contains(out, ".mobius") {
					failed.Store(true)
					cancel()
					return
				}
			}
		}()
	}
	wg.Wait()
	if failed.Load() {
		t.Fatal("concurrent grep leaked telemetry or missed source")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
