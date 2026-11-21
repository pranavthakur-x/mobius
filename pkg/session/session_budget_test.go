package session

import (
	"fmt"
	"strings"
	"testing"
)

func TestSessionIncludesAgentBudget(t *testing.T) {
	chdirModuleRoot(t)
	ag := newTestAgent(t)
	steps, _, _ := ag.Limits()
	if steps <= 0 {
		t.Fatal("expected a step budget from config/agent.toml")
	}

	sess := NewSession("s1", "Code Review", ag, "You are Code Review.")
	msgs := sess.Context.Messages()
	if len(msgs) == 0 {
		t.Fatal("expected a system message")
	}
	if !strings.Contains(msgs[0].Content, "You are Code Review.") {
		t.Fatalf("role text missing: %s", msgs[0].Content)
	}
	want := fmt.Sprintf("Hard stop from config/agent.toml: %d steps", steps)
	if !strings.Contains(msgs[0].Content, want) {
		t.Fatalf("budget notice missing %q: %s", want, msgs[0].Content)
	}
}

func TestNewRoleSessionComposesBaseGuidesAndRole(t *testing.T) {
	chdirModuleRoot(t)
	ag := newTestAgent(t)
	steps, _, _ := ag.Limits()

	sess := NewRoleSession("r1", "Code Review", ag, "<role_persona>Role: Code Reviewer</role_persona>")
	msgs := sess.Context.Messages()
	if len(msgs) == 0 {
		t.Fatal("expected a system message")
	}
	sys := msgs[0].Content
	for _, want := range []string{
		"<identity>",
		"<grounding>",
		"<tools>",
		"<role_instructions>",
		"<role_persona>Role: Code Reviewer</role_persona>",
		fmt.Sprintf("Hard stop from config/agent.toml: %d steps", steps),
	} {
		if !strings.Contains(sys, want) {
			t.Fatalf("role session system prompt missing %q:\n%s", want, sys)
		}
	}
}
