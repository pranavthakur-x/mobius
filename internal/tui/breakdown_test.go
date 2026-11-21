package tui

import (
	"strings"
	"testing"

	"mobius/internal/tui/markdown"
	"mobius/pkg/llm"
)

func TestSidebarHidesUnsetPrice(t *testing.T) {
	bd := ContextBreakdown{Capacity: TokenCapacity(), Used: 10, System: 10, CostUSD: 1.25}
	out := markdown.StripANSI(renderSidebar(30, 20, bd, BudgetSnapshot{CostUSD: 2}, nil))
	if strings.Contains(out, "$") {
		t.Fatalf("price shown without a configured price:\n%s", out)
	}
	bd.HasPrice = true
	out = markdown.StripANSI(renderSidebar(30, 20, bd, BudgetSnapshot{}, nil))
	if !strings.Contains(out, "$1.25000") {
		t.Fatalf("price hidden after it was set:\n%s", out)
	}
}

func TestUsageBarWidths(t *testing.T) {
	width := 20
	cap := TokenCapacity()
	check := func(used, wantFilled int) {
		bd := ContextBreakdown{Capacity: cap, Used: used, System: used}
		segs := UsageBarSegments(bd, width)
		filled := 0
		for _, s := range segs {
			filled += s.Count
		}
		if filled != wantFilled {
			t.Fatalf("used=%d want filled %d got %d", used, wantFilled, filled)
		}
	}
	check(0, 0)
	check(1, 1)
	check(64000, (width*64000)/cap)
	check(128000, width)
	check(200000, width)
	bd := ContextBreakdown{Capacity: cap, Used: 1000, System: 100, ToolDefs: 200, Rules: 300, Conversation: 400}
	for _, w := range []int{1, 2, 4, 8} {
		for _, seg := range UsageBarSegments(bd, w) {
			if seg.Count < 0 {
				t.Fatalf("negative segment %+v at width %d", seg, w)
			}
		}
		_ = renderUsageBar(bd, w)
	}
	_ = renderUsageBar(bd, 0)
}

func TestRulesAndConversationCounters(t *testing.T) {
	bd := ComputeBreakdown([]llm.Message{
		{Role: llm.RoleSystem, Content: "You are Mobius.\nRules:\n- stay in scope"},
		{Role: llm.RoleUser, Content: "hello there"},
	}, []llm.ToolDefinition{{
		Type:     "function",
		Function: llm.FunctionDef{Name: "view_file", Description: "read a file"},
	}}, 0.25)
	if bd.Rules == 0 {
		t.Fatal("rules section should count")
	}
	if bd.Conversation == 0 {
		t.Fatal("user text should count as conversation")
	}
	if bd.ToolDefs == 0 {
		t.Fatal("tool schema should count")
	}
	if bd.CostUSD != 0.25 {
		t.Fatalf("cost: %v", bd.CostUSD)
	}
	if bd.System == 0 {
		t.Fatal("system prompt should remain")
	}
	if bd.Used != bd.System+bd.Rules+bd.ToolDefs+bd.Conversation {
		t.Fatalf("used %d != system %d + rules %d + tools %d + conversation %d", bd.Used, bd.System, bd.Rules, bd.ToolDefs, bd.Conversation)
	}
	before := bd.Conversation
	bd.NoteConversation(EstimateTranscript([]TranscriptBlock{{
		Kind: BlockUser,
		Text: "hello there, please look at the workspace and explain the layout in detail",
	}}))
	if bd.Conversation <= before {
		t.Fatal("visible transcript should raise the conversation counter")
	}
}
