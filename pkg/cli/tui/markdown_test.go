package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderMarkdownHeadingsListsAndInline(t *testing.T) {
	input := strings.Join([]string{
		"## Summary",
		"",
		"Fixed **race condition** in `pkg/orchestrator/manager.go`.",
		"",
		"- [P1] First item with a long explanation that should wrap neatly onto a continuation line",
		"  - Nested detail with *italic* text",
		"1. Run `go test -race ./...`",
		"2. Check verdict: APPROVE",
	}, "\n")

	lines := RenderMarkdown(input, 48)
	if len(lines) == 0 {
		t.Fatal("expected formatted lines")
	}
	plain := make([]string, len(lines))
	for i, ln := range lines {
		plain[i] = StripANSI(ln)
		if w := utf8.RuneCountInString(plain[i]); w > 48 {
			t.Fatalf("line %d exceeds width 48 (%d runes): %q", i, w, plain[i])
		}
	}
	joined := strings.Join(plain, "\n")
	if strings.Contains(joined, "## ") || strings.Contains(joined, "**") || strings.Contains(joined, "`") {
		t.Fatalf("raw markdown markers leaked into output:\n%s", joined)
	}
	for _, want := range []string{
		"Summary",
		"Fixed race condition in",
		"pkg/orchestrator/manager.go.",
		"• [P1] First item with a long explanation that",
		"  should wrap neatly onto a continuation line",
		"  ◦ Nested detail with italic text",
		"1. Run go test -race ./...",
		"2. Check verdict: APPROVE",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("formatted output missing %q:\n%s", want, joined)
		}
	}
}

func TestRenderMarkdownPreservesCodeBlockIndentation(t *testing.T) {
	input := "```go\nfunc main() {\n\tif true {\n\t\tfmt.Println(\"hi\")\n\t}\n}\n```"
	lines := RenderMarkdown(input, 60)
	plain := make([]string, len(lines))
	for i, ln := range lines {
		plain[i] = StripANSI(ln)
	}
	joined := strings.Join(plain, "\n")
	for _, want := range []string{
		"┌ go",
		"│ func main() {",
		"│     if true {",
		"│         fmt.Println(\"hi\")",
		"└",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("code block output missing %q:\n%s", want, joined)
		}
	}
}

func TestRenderMarkdownTableAndBlockquoteAndStripsANSI(t *testing.T) {
	input := strings.Join([]string{
		"> Important note \x1b[31mred\x1b[0m",
		"",
		"| File | Status |",
		"|------|--------|",
		"| `a.go` | pass |",
		"| `b.go` | fail |",
		"",
		"---",
	}, "\n")

	lines := RenderMarkdown(input, 50)
	plain := make([]string, len(lines))
	for i, ln := range lines {
		plain[i] = StripANSI(ln)
	}
	joined := strings.Join(plain, "\n")
	for _, want := range []string{
		"│ Important note red",
		"File",
		"Status",
		"─┼─",
		"a.go",
		"fail",
		"────────",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("formatted output missing %q:\n%s", want, joined)
		}
	}
}

func TestRenderTranscriptFinalUsesMarkdownFormatting(t *testing.T) {
	bl := TranscriptBlock{
		Kind:    BlockFinal,
		Speaker: "Software Engineer",
		RoleID:  "software_engineer",
		Text:    "## Done\n- Updated `pkg/agent/loop.go`",
	}
	lines := renderTranscriptLine(bl, 60, false)
	joined := StripANSI(strings.Join(lines, "\n"))
	if strings.Contains(joined, "## ") || strings.Contains(joined, "`") {
		t.Fatalf("BlockFinal did not format markdown:\n%s", joined)
	}
	if !strings.Contains(joined, "  Done") || !strings.Contains(joined, "  • Updated pkg/agent/loop.go") {
		t.Fatalf("unexpected formatted BlockFinal:\n%s", joined)
	}
}
