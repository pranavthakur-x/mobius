package tui

import (
	"strings"
	"sync"
	"testing"

	"mobius/internal/tui/markdown"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestBudgetNotInTranscript(t *testing.T) {
	buf := NewChatBuffer()
	settings := DefaultSettings()
	ledger := NewBudgetLedger()
	ev := BudgetEvent{SourceRef: SourceRef{ThreadID: "a"}, InTokens: 1, OutTokens: 2, CostUSD: 0.01}
	ledger.Apply(ev)
	buf.ApplyLiveEvent(AgentStepEvent{SourceRef: SourceRef{Name: "w"}, Text: "step"}, settings)
	if buf.HasBudgetInTranscript() {
		t.Fatal("budget must not be in transcript blocks")
	}
}

func TestBudgetCumulativeReplaceAndTotal(t *testing.T) {
	l := NewBudgetLedger()
	l.Apply(BudgetEvent{SourceRef: SourceRef{ThreadID: "a"}, InTokens: 10, OutTokens: 1, CostUSD: 0.1})
	l.Apply(BudgetEvent{SourceRef: SourceRef{ThreadID: "a"}, InTokens: 20, OutTokens: 2, CostUSD: 0.2})
	tot := l.Total()
	if tot.InTokens != 20 || tot.OutTokens != 2 {
		t.Fatalf("replace per thread: %+v", tot)
	}
	l.Apply(BudgetEvent{SourceRef: SourceRef{ThreadID: "b"}, InTokens: 5, OutTokens: 5, CostUSD: 0.05})
	tot = l.Total()
	if tot.InTokens != 25 || tot.CostUSD < 0.249 {
		t.Fatalf("multi-agent sum: %+v", tot)
	}
}

func TestThoughtPromotionNoDuplicate(t *testing.T) {
	buf := NewChatBuffer()
	settings := DefaultSettings()
	buf.BeginRun()
	text := "final answer"
	buf.ApplyLiveEvent(AgentThoughtEvent{SourceRef: SourceRef{Name: "eng"}, Text: text}, settings)
	buf.ResolveFinal("Software Engineer", "software_engineer", text, "run-1", nil)
	vis := buf.VisibleBlocks()
	finals := 0
	for _, b := range vis {
		if b.Kind == BlockFinal {
			finals++
		}
		if b.Kind == BlockThought {
			t.Fatal("thought should be promoted")
		}
	}
	if finals != 1 {
		t.Fatalf("want 1 final, got %d", finals)
	}
}

func TestToolAfterThoughtMarksIntermediate(t *testing.T) {
	buf := NewChatBuffer()
	settings := DefaultSettings()
	buf.ApplyLiveEvent(AgentThoughtEvent{SourceRef: SourceRef{Name: "Software Engineer"}, Text: "thinking out loud"}, settings)
	buf.ApplyLiveEvent(AgentToolEvent{SourceRef: SourceRef{Name: "Software Engineer"}, Name: "view_file", Args: `{"path":"main.go"}`}, settings)
	for _, b := range buf.Blocks {
		if b.Kind == BlockThought || strings.Contains(b.Text, "thinking") {
			t.Fatal("thinking text should not stay in the transcript")
		}
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	lines := flattenTranscript(buf.VisibleBlocks(), 80, true)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Software Engineer") || !strings.Contains(joined, "view_file") {
		t.Fatalf("activity should name the agent and the tool:\n%s", joined)
	}
	if !strings.Contains(joined, "\x1b[3") {
		t.Fatalf("tool activity should be faded italic, got %q", joined)
	}
	reply := flattenTranscript([]TranscriptBlock{{
		Kind: BlockFinal, Speaker: "orchestrator", Text: "Hello",
	}}, 80, true)
	replyLine := ""
	for _, ln := range reply {
		if strings.Contains(ln, "Hello") {
			replyLine = ln
		}
	}
	if strings.Contains(replyLine, "\x1b[3") {
		t.Fatalf("conversation reply should stay full contrast, got %q", replyLine)
	}
}

func TestPerChatIsolation(t *testing.T) {
	store := NewChatStore("c1")
	settings := DefaultSettings()
	store.Ensure("c2")
	store.Buffer("c2").AppendUser("other", "r1")
	store.Buffer("c1").AppendUser("here", "r1")
	if len(store.Buffer("c2").Blocks) != 1 || store.Buffer("c2").Blocks[0].Text != "other" {
		t.Fatal("isolation failed")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			store.Buffer("c2").ApplyLiveEvent(AgentStepEvent{SourceRef: SourceRef{}, Text: "s"}, settings)
		}(i)
	}
	wg.Wait()
	if len(store.Buffer("c1").Blocks) != 1 {
		t.Fatal("inactive chat mutated")
	}
}

func TestToolFoldingCtrlO(t *testing.T) {
	buf := NewChatBuffer()
	settings := DefaultSettings()
	buf.ApplyLiveEvent(AgentToolEvent{SourceRef: SourceRef{}, Name: "a", Args: ""}, settings)
	buf.ApplyLiveEvent(AgentToolEvent{SourceRef: SourceRef{}, Name: "b", Args: ""}, settings)
	if len(buf.Blocks) != 1 || buf.Blocks[0].ToolCount != 2 {
		t.Fatalf("expected folded group, blocks=%d", len(buf.Blocks))
	}
	buf.ToggleToolExpand()
	if !buf.Blocks[0].Expanded {
		t.Fatal("ctrl+o should expand group")
	}
}

func TestStreamDeltaPromotesCleanlyToFinalWithoutDuplicates(t *testing.T) {
	buf := NewChatBuffer()
	settings := DefaultSettings()
	src := SourceRef{ChatID: "c1", RoleID: "software_engineer", Name: "Software Engineer", Kind: SourceWorker}

	buf.BeginRun()
	buf.ApplyLiveEvent(StreamDeltaEvent{SourceRef: src, TextDelta: "Fixed "}, settings)
	buf.ApplyLiveEvent(StreamDeltaEvent{SourceRef: src, TextDelta: "**bug**."}, settings)

	vis := buf.VisibleBlocks()
	if len(vis) != 1 || vis[0].Kind != BlockStreaming || vis[0].Text != "Fixed **bug**." {
		t.Fatalf("unexpected streaming block: %+v", vis)
	}
	rendered := markdown.StripANSI(strings.Join(flattenTranscript(vis, 60, true), "\n"))
	if !strings.Contains(rendered, "Fixed bug.▍") {
		t.Fatalf("expected live formatted stream with cursor, got:\n%s", rendered)
	}

	// Simulate intermediate tool start -> live tool prep -> actual tool execution clears stream.
	buf.ApplyLiveEvent(StreamDeltaEvent{SourceRef: src, ToolName: "edit_file", ToolID: "tc_1"}, settings)
	vis = buf.VisibleBlocks()
	if len(vis) != 1 || vis[0].StreamingTool != "edit_file" {
		t.Fatalf("expected preparing tool block, got: %+v", vis)
	}
	prepRendered := markdown.StripANSI(strings.Join(flattenTranscript(vis, 60, true), "\n"))
	if !strings.Contains(prepRendered, "preparing edit_file…") {
		t.Fatalf("expected preparing tool status, got:\n%s", prepRendered)
	}

	buf.ApplyLiveEvent(AgentToolEvent{SourceRef: src, Name: "edit_file", Args: `{"path":"a.go"}`}, settings)
	for _, bl := range buf.VisibleBlocks() {
		if bl.Kind == BlockStreaming {
			t.Fatalf("streaming block should clear when tool executes: %+v", bl)
		}
	}

	// Final answer streams and promotes cleanly in-place.
	buf.ApplyLiveEvent(StreamDeltaEvent{SourceRef: src, TextDelta: "Done "}, settings)
	buf.ApplyLiveEvent(StreamDeltaEvent{SourceRef: src, TextDelta: "now."}, settings)
	buf.ApplyLiveEvent(AgentThoughtEvent{SourceRef: src, Text: "Done now."}, settings)
	buf.ResolveFinal(src.Name, src.RoleID, "Done now.", "run-1", nil)

	finals := 0
	for _, bl := range buf.VisibleBlocks() {
		if bl.Kind == BlockStreaming || bl.Kind == BlockThought {
			t.Fatalf("stale transient block remained: %+v", bl)
		}
		if bl.Kind == BlockFinal {
			finals++
		}
	}
	if finals != 1 {
		t.Fatalf("want exactly 1 BlockFinal, got %d", finals)
	}
}

func TestConcurrentMultiWorkerStreamsStayIsolated(t *testing.T) {
	buf := NewChatBuffer()
	settings := DefaultSettings()
	se := SourceRef{ChatID: "c1", RoleID: "software_engineer", Name: "Software Engineer", Kind: SourceWorker}
	cr := SourceRef{ChatID: "c1", RoleID: "code_review", Name: "Code Review", Kind: SourceWorker}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			buf.ApplyLiveEvent(StreamDeltaEvent{SourceRef: se, TextDelta: "a"}, settings)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			buf.ApplyLiveEvent(StreamDeltaEvent{SourceRef: cr, TextDelta: "b"}, settings)
		}
	}()
	wg.Wait()

	vis := buf.VisibleBlocks()
	if len(vis) != 2 {
		t.Fatalf("expected 2 isolated streaming blocks, got %d", len(vis))
	}
	for _, bl := range vis {
		switch bl.RoleID {
		case "software_engineer":
			if bl.Text != strings.Repeat("a", 20) {
				t.Fatalf("se text corrupted: %q", bl.Text)
			}
		case "code_review":
			if bl.Text != strings.Repeat("b", 20) {
				t.Fatalf("cr text corrupted: %q", bl.Text)
			}
		default:
			t.Fatalf("unexpected role %q", bl.RoleID)
		}
	}
}
