package tui

import (
	"strings"
	"testing"

	"mobius/internal/tui/markdown"
)

func TestShowStepsTraceContextCombos(t *testing.T) {
	buf := NewChatBuffer()
	buf.ApplyLiveEvent(AgentStepEvent{SourceRef: SourceRef{}, Text: "s"}, Settings{ShowSteps: false, ShowTrace: true, ShowContext: true})
	buf.ApplyLiveEvent(RouteTraceEvent{SourceRef: SourceRef{}, Text: "trace"}, Settings{ShowSteps: true, ShowTrace: false, ShowContext: true})
	if len(buf.Blocks) != 0 {
		t.Fatal("hidden kinds should not append")
	}
	s := Settings{ShowSteps: true, ShowTrace: true, ShowContext: false}
	sz := ComputeLayout(80, 24, s.ShowContext, 1, 0)
	if sz.SidebarW != 0 || sz.ChatW != 80 {
		t.Fatalf("show_context=false layout: %+v", sz)
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
	joined := markdown.StripANSI(strings.Join(lines, "\n"))
	if strings.Contains(joined, "## ") || strings.Contains(joined, "`") {
		t.Fatalf("BlockFinal did not format markdown:\n%s", joined)
	}
	if !strings.Contains(joined, "  Done") || !strings.Contains(joined, "  • Updated pkg/agent/loop.go") {
		t.Fatalf("unexpected formatted BlockFinal:\n%s", joined)
	}
}
