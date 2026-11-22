package tui

import (
	"context"
	"strings"
	"testing"

	"mobius/internal/tui/markdown"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCtrlCInterruptsActiveTurnWithoutQuitting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	turnCtx, turnCancel := context.WithCancel(ctx)
	m := &Model{
		settings:     DefaultSettings(),
		ctx:          ctx,
		cancel:       cancel,
		events:       make(chan LiveEvent, 16),
		store:        NewChatStore("c1"),
		ledger:       NewBudgetLedger(),
		decoder:      NewLineDecoder(),
		vp:           NewViewport(),
		width:        100,
		height:       30,
		chatID:       "c1",
		chatName:     "Main",
		modelName:    "claude-3-5-sonnet",
		composeLines: 1,
		inFlight:     true,
		turnCancel:   turnCancel,
	}

	buf := m.store.Buffer("c1")
	buf.SetTeams([]TeamLine{{DeptName: "Engineering", RoleID: "swe", Name: "Software Engineer", State: WorkerRunning}})
	buf.ApplyLiveEvent(StreamDeltaEvent{
		SourceRef: SourceRef{ChatID: "c1", Name: "Software Engineer", RoleID: "swe"},
		TextDelta: "Partial streamed response",
	}, m.settings)

	// First Ctrl+C while turn is active should cancel turn, NOT quit.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Fatal("expected Ctrl+C during active turn to cancel without returning tea.Quit")
	}
	if turnCtx.Err() != context.Canceled {
		t.Fatal("expected turn context to be canceled")
	}
	if m.inFlight || m.routing {
		t.Fatal("expected inFlight and routing to be false after cancel")
	}
	if ctx.Err() != nil {
		t.Fatal("expected app context to stay alive after turn interrupt")
	}

	blocks := buf.VisibleBlocks()
	var sawPartialFinal, sawInterruptTrace bool
	for _, bl := range blocks {
		if bl.Kind == BlockStreaming {
			t.Fatalf("found leftover BlockStreaming after cancel: %+v", bl)
		}
		if bl.Kind == BlockFinal && bl.Text == "Partial streamed response" {
			sawPartialFinal = true
		}
		if bl.Kind == BlockTrace && strings.Contains(bl.Text, "Interrupted by user") {
			sawInterruptTrace = true
		}
	}
	if !sawPartialFinal {
		t.Fatal("expected partial streamed text to be preserved as BlockFinal")
	}
	if !sawInterruptTrace {
		t.Fatal("expected interrupt trace notice in transcript")
	}

	// Second Ctrl+C while idle should shut down and quit.
	_, quitCmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if quitCmd == nil {
		t.Fatal("expected Ctrl+C while idle to return tea.Quit")
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("expected app context to be canceled on idle Ctrl+C")
	}
}

func TestRenderHeaderIncludesGitStatus(t *testing.T) {
	got := markdown.StripANSI(renderHeader(120, 4, "Chat 1", "gpt-4o", "Git: (dev 3 modified)", "/tmp/project", "all", 0))
	if !strings.Contains(got, "Mobius") || !strings.Contains(got, "gpt-4o") {
		t.Fatalf("header missing brand or model:\n%s", got)
	}
	if !strings.Contains(got, "Git: (dev 3 modified)") {
		t.Fatalf("header missing git status:\n%s", got)
	}
	if !strings.Contains(got, "/tmp/project") {
		t.Fatalf("header missing workDir:\n%s", got)
	}
	if !strings.Contains(got, "▄██▄  ▄██▄") {
		t.Fatalf("header missing pixel mascot:\n%s", got)
	}
}

func TestMascotClickTriggersAndCompletesAnimation(t *testing.T) {
	m := &Model{
		settings: Settings{ShowContext: false},
		store:    NewChatStore("c1"),
		ledger:   NewBudgetLedger(),
		vp:       NewViewport(),
		width:    100,
		height:   30,
		chatID:   "c1",
	}

	// Clicking outside the mascot hit-box does nothing.
	_, cmd := m.Update(tea.MouseMsg{X: 50, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd != nil || m.mascotFrame != 0 {
		t.Fatal("expected click outside mascot not to animate")
	}

	// Clicking inside the mascot hit-box starts animation and returns a tick command.
	_, cmd = m.Update(tea.MouseMsg{X: 5, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil || m.mascotFrame != 1 || m.mascotRemaining != mascotOrbitFrames {
		t.Fatalf("expected animation started, frame=%d remaining=%d cmd=%v", m.mascotFrame, m.mascotRemaining, cmd != nil)
	}

	// Clicking again while running extends without spawning a duplicate ticker.
	_, dupCmd := m.Update(tea.MouseMsg{X: 5, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if dupCmd != nil {
		t.Fatal("expected re-click during animation not to spawn duplicate ticker")
	}

	// Ticking through all remaining frames advances frames and returns cleanly to frame 0.
	for i := 0; i < mascotOrbitFrames; i++ {
		_, cmd = m.Update(mascotTickMsg{})
	}
	if m.mascotFrame != 0 || m.mascotRemaining != 0 || cmd != nil {
		t.Fatalf("expected animation to settle at frame 0, got frame=%d remaining=%d cmd=%v", m.mascotFrame, m.mascotRemaining, cmd != nil)
	}
}

func TestComposeCursorEditingAndDualRules(t *testing.T) {
	m := &Model{
		settings:     DefaultSettings(),
		store:        NewChatStore("c1"),
		ledger:       NewBudgetLedger(),
		vp:           NewViewport(),
		width:        80,
		height:       24,
		chatID:       "c1",
		composeLines: 1,
	}

	// Type "helo", move left once, insert 'l' -> "hello"
	for _, r := range "helo" {
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if m.draft != "hello" {
		t.Fatalf("expected %q after mid-string insert, got %q", "hello", m.draft)
	}

	// Render footer and confirm top & bottom horizontal rules frame the compose line.
	sz := ComputeLayout(80, 24, false, m.composeLines, 0)
	footer := markdown.StripANSI(renderFooter(sz, m))
	if strings.Count(footer, strings.Repeat("─", 80)) < 2 {
		t.Fatalf("expected top and bottom rules in compose footer:\n%s", footer)
	}
	if !strings.Contains(footer, "❯ hello") {
		t.Fatalf("expected prompt text in compose footer:\n%s", footer)
	}
}

func TestHandleSlashHelpRendersInTranscript(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &Model{
		settings:     DefaultSettings(),
		ctx:          ctx,
		cancel:       cancel,
		events:       make(chan LiveEvent, 16),
		store:        NewChatStore("c1"),
		ledger:       NewBudgetLedger(),
		decoder:      NewLineDecoder(),
		vp:           NewViewport(),
		chatID:       "c1",
		composeLines: 1,
		draft:        "/help",
	}

	_, cmd := m.submit()
	if cmd != nil {
		t.Fatal("expected /help not to dispatch async LLM command")
	}
	blocks := m.store.Buffer("c1").VisibleBlocks()
	found := false
	for _, bl := range blocks {
		if bl.Kind == BlockFinal && strings.Contains(bl.Text, "/help") && strings.Contains(bl.Text, "Slash Commands") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected /help markdown reply in transcript, got blocks: %+v", blocks)
	}
}

func TestActionMenuHasHelpAndTypingSlashHelpWorks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := &Model{
		settings:     DefaultSettings(),
		ctx:          ctx,
		cancel:       cancel,
		events:       make(chan LiveEvent, 16),
		store:        NewChatStore("c1"),
		ledger:       NewBudgetLedger(),
		decoder:      NewLineDecoder(),
		vp:           NewViewport(),
		chatID:       "c1",
		composeLines: 1,
	}

	// 1. Pressing '/' opens MenuActions containing "Help" and activating ActionHelp prints help.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if m.menu != MenuActions {
		t.Fatalf("expected MenuActions, got %v", m.menu)
	}
	if len(m.menuItems) <= ActionHelp || m.menuItems[ActionHelp] != "Help" {
		t.Fatalf("expected Help in action menu, got %v", m.menuItems)
	}
	m.menuIndex = ActionHelp
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.menu != MenuNone {
		t.Fatalf("expected menu closed after ActionHelp")
	}
	if len(m.store.Buffer("c1").VisibleBlocks()) == 0 {
		t.Fatal("expected help output block after selecting Help from menu")
	}

	// 2. Typing "/help" character-by-character falls back cleanly from MenuActions into draft.
	for _, r := range "/help" {
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.draft != "/help" {
		t.Fatalf("expected draft %q, got %q", "/help", m.draft)
	}
}
