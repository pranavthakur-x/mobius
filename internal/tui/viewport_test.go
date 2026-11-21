package tui

import (
	"strings"
	"testing"

	"mobius/internal/tui/markdown"
)

func TestAutoFollowPauseResume(t *testing.T) {
	vp := NewViewport()
	vp.Height = 10
	vp.SetContentLines(30)
	vp.ScrollUp(1)
	if vp.AutoFollow {
		t.Fatal("scroll up should pause auto-follow")
	}
	vp.ScrollDown(vp.Offset + 1)
	if !vp.AutoFollow || vp.Offset != 0 {
		t.Fatal("return to bottom should resume auto-follow")
	}
}

func TestExactFrameHeight(t *testing.T) {
	m := &Model{
		width: 100, height: 30,
		settings:     DefaultSettings(),
		store:        NewChatStore("c"),
		ledger:       NewBudgetLedger(),
		chatID:       "c",
		chatName:     "Test",
		modelName:    "model",
		composeLines: 1,
	}
	out := FitScreen(m)
	lines := 0
	for _, c := range out {
		if c == '\n' {
			lines++
		}
	}
	lines++ // last line
	if lines != 30 {
		t.Fatalf("frame height %d want 30", lines)
	}
	raw := strings.Split(markdown.StripANSI(out), "\n")
	if !strings.Contains(raw[0], "Context") {
		t.Fatalf("sidebar should start at the top, first line %q", raw[0])
	}
	last := raw[len(raw)-1]
	hint := strings.Index(last, "mention")
	if hint < sidebarWidth {
		t.Fatalf("compose hint should sit in the chat column, line %q", last)
	}
}
