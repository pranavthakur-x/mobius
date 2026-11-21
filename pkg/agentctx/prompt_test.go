package agentctx

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptSteersAwayFromTelemetry(t *testing.T) {
	prompt := BuildSystemPrompt("")
	for _, want := range []string{
		"Never search .mobius",
		"fewest reads or searches, then answer and stop",
		"view_file / list_dir / grep_search",
		"Do not switch to run_command to cat or sed the same file",
		"<grounding>",
		"<workflow>",
		"<tools>",
		"<safety>",
		"Error recovery:",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
}

func TestBuildRoleSystemPromptLayersBaseGuidesAndRole(t *testing.T) {
	prompt := BuildRoleSystemPrompt("<role_persona>Code Reviewer</role_persona>", "\n\n# WORKSPACE GUIDES\nverify with go test")
	baseIdx := strings.Index(prompt, "<identity>")
	guideIdx := strings.Index(prompt, "# WORKSPACE GUIDES")
	roleIdx := strings.Index(prompt, "<role_instructions>")

	if baseIdx < 0 || guideIdx < 0 || roleIdx < 0 {
		t.Fatalf("expected base, guides, and role sections, got:\n%s", prompt)
	}
	if baseIdx >= guideIdx || guideIdx >= roleIdx {
		t.Fatalf("expected order base < guides < role, got indices %d, %d, %d", baseIdx, guideIdx, roleIdx)
	}
	if !strings.Contains(prompt, "<role_persona>Code Reviewer</role_persona>") {
		t.Fatalf("missing role body in prompt:\n%s", prompt)
	}
}

func TestDefaultSystemPromptIsStackAgnostic(t *testing.T) {
	prompt := BuildSystemPrompt("")
	for _, banned := range []string{"go test", "go build", "golang", "pkg/", "cmd/", "module mobius"} {
		if strings.Contains(strings.ToLower(prompt), banned) {
			t.Fatalf("core system prompt should be stack-agnostic, found %q", banned)
		}
	}
}
