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
	got := markdown.StripANSI(renderHeader(120, "Chat 1", "gpt-4o", "Git: (dev 3 modified)", "all"))
	if !strings.Contains(got, "Git: (dev 3 modified)") {
		t.Fatalf("header missing git status: %q", got)
	}
	if !strings.Contains(got, "Mobius Chat 1 · gpt-4o · Git: (dev 3 modified)") {
		t.Fatalf("unexpected header formatting: %q", got)
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
