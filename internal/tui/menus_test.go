package tui

import (
	"strings"
	"testing"

	"mobius/internal/tui/markdown"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEmptyOnlyMenuTriggers(t *testing.T) {
	if MenuTrigger("", '/') != MenuActions {
		t.Fatal("slash on empty")
	}
	if MenuTrigger("hi", '/') != MenuNone {
		t.Fatal("slash mid draft blocked")
	}
	if MenuTrigger("", '*') != MenuRoles {
		t.Fatal("star on empty")
	}
	if MenuTrigger("word", '*') != MenuNone {
		t.Fatal("star mid sentence is text")
	}
	if MenuTrigger("", '=') != MenuView {
		t.Fatal("equals on empty")
	}
}

func TestActionMenuListsEveryItem(t *testing.T) {
	m := &Model{menu: MenuActions}
	m.loadMenuItems()
	out := markdown.StripANSI(renderMenuFooter(m, 80, len(actionLabels)))
	if strings.Count(out, "\n")+1 != len(actionLabels) {
		t.Fatalf("menu lines = %d, want %d\n%s", strings.Count(out, "\n")+1, len(actionLabels), out)
	}
	for _, label := range actionLabels {
		if !strings.Contains(out, label) {
			t.Fatalf("menu missing %q\n%s", label, out)
		}
	}
}

func TestSpaceInsertsIntoDraft(t *testing.T) {
	m := &Model{store: NewChatStore("c"), chatID: "c", composeLines: 1, draft: "hi"}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.draft != "hi " {
		t.Fatalf("space dropped, draft %q", m.draft)
	}
	m.draft = ""
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.draft != " " {
		t.Fatalf("leading space dropped, draft %q", m.draft)
	}
}

func TestModelLabelIncludesURL(t *testing.T) {
	got := modelLabel(ModelOption{ID: "deepseek/deepseek-v4.1-flash", URL: "https://openrouter.ai/api/v1"})
	if got != "deepseek/deepseek-v4.1-flash · https://openrouter.ai/api/v1" {
		t.Fatalf("label %q", got)
	}
	if modelLabel(ModelOption{ID: "local"}) != "local" {
		t.Fatal("url-less model should stay the id")
	}
}

func TestConnectListShowsEveryAPI(t *testing.T) {
	m := &Model{
		connectOn:      true,
		connectListing: true,
		connectItems:   []string{"claude · https://api.anthropic.com", "flash · https://openrouter.ai/api/v1", "Add API"},
		connectIDs:     []string{"a", "b", ""},
	}
	out := markdown.StripANSI(renderConnectList(m, 90, 3))
	for _, label := range m.connectItems {
		if !strings.Contains(out, label) {
			t.Fatalf("list missing %q\n%s", label, out)
		}
	}
}

func TestConnectFormMasksKey(t *testing.T) {
	m := &Model{
		connectOn:     true,
		connectIndex:  2,
		connectFields: [6]string{"Example", "https://example.com/v1", "sk-secret", "my-model", "", ""},
	}
	out := markdown.StripANSI(renderConnectForm(m, 80, len(connectLabels)))
	if strings.Contains(out, "sk-secret") {
		t.Fatalf("api key leaked into the form:\n%s", out)
	}
	if !strings.Contains(out, "API key") || !strings.Contains(out, "••••") {
		t.Fatalf("masked key missing:\n%s", out)
	}
}

func TestConnectEditsTheSelectedField(t *testing.T) {
	m := &Model{connectOn: true, connectIndex: 3, store: NewChatStore("c"), chatID: "c"}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gpt")})
	if m.connectFields[3] != "gpt" {
		t.Fatalf("model field %q", m.connectFields[3])
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.connectErr == "" {
		t.Fatal("save without a backend should report an error")
	}
}

func TestHistoryRestoresUserAndReply(t *testing.T) {
	buf := NewChatBuffer()
	settings := DefaultSettings()
	applyHistoryTurn(buf, "c", HistoryTurn{Role: "user", Text: "list the llm apis"}, settings)
	applyHistoryTurn(buf, "c", HistoryTurn{Role: "trace", Speaker: "orchestrator", Text: "answered directly"}, settings)
	applyHistoryTurn(buf, "c", HistoryTurn{Role: "assistant", Speaker: "orchestrator", Text: "OpenRouter and Ollama"}, settings)
	var user, reply bool
	for _, block := range buf.VisibleBlocks() {
		if block.Kind == BlockUser && block.Text == "list the llm apis" {
			user = true
		}
		if block.Kind == BlockFinal && block.Text == "OpenRouter and Ollama" {
			reply = true
		}
	}
	if !user || !reply {
		t.Fatalf("history missing from transcript: %+v", buf.VisibleBlocks())
	}
}

func TestMentionFallback(t *testing.T) {
	if got := MentionFallback('x'); got != "*x" {
		t.Fatalf("got %q", got)
	}
}
