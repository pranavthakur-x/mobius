package tui

import (
	"fmt"
	"strings"

	"mobius/internal/tui/theme"

	"github.com/charmbracelet/lipgloss"
)

const sidebarWidth = 30

// LayoutSizes splits terminal height/width into header, body, footer.
type LayoutSizes struct {
	Width, Height  int
	Header, Footer int
	ChatW, ChatH   int
	SidebarW       int
	TranscriptH    int
	MenuLines      int
	ShowContext    bool
}

func ComputeLayout(width, height int, showContext bool, composeLines, menuLines int) LayoutSizes {
	if composeLines < 1 {
		composeLines = 1
	}
	if composeLines > 8 {
		composeLines = 8
	}
	if menuLines < 0 {
		menuLines = 0
	}
	maxMenu := height - 1 - 2 - composeLines - 1 // header, rule, hint, compose, one transcript line
	if maxMenu < 1 {
		maxMenu = 1
	}
	if menuLines > maxMenu {
		menuLines = maxMenu
	}
	footer := 2 + composeLines + menuLines // rule + menu + input + hint
	header := 1
	transcript := height - header - footer
	if transcript < 1 {
		transcript = 1
	}
	sidebar := 0
	chatW := width
	if showContext {
		sidebar = sidebarWidth
		chatW = width - sidebar
		if chatW < 20 {
			chatW = 20
		}
	}
	return LayoutSizes{
		Width: width, Height: height,
		Header: header, Footer: footer,
		ChatW: chatW, ChatH: transcript,
		SidebarW: sidebar, TranscriptH: transcript,
		MenuLines:   menuLines,
		ShowContext: showContext,
	}
}

// FitScreen renders the full frame at exactly height lines.
// The context sidebar is a full-height left column. Header, transcript, and
// the compose box stay in the right column.
func FitScreen(m *Model) string {
	menuLines := m.menuLineCount()
	sz := ComputeLayout(m.width, m.height, m.settings.ShowContext, m.composeLines, menuLines)
	right := renderRight(sz, m)
	if !sz.ShowContext {
		return fitBlock(right, m.height)
	}
	buf := m.store.Buffer(m.chatID)
	side := renderSidebar(sz.SidebarW, m.height, m.sidebarBreakdown, m.ledger.Total(), buf.Teams)
	return fitBlock(lipgloss.JoinHorizontal(lipgloss.Top, side, right), m.height)
}

func renderHeader(chatW int, chatName, model, view string) string {
	title := fmt.Sprintf("Mobius %s · %s  = %s", chatName, model, view)
	return lipgloss.NewStyle().Width(chatW).Render(title)
}

func renderRight(sz LayoutSizes, m *Model) string {
	header := theme.StyleRoot.Width(sz.ChatW).MaxHeight(1).Render(renderHeader(sz.ChatW, m.chatName, m.modelName, m.viewLabel()))
	transcript := renderTranscript(sz, m)
	footer := renderFooter(sz, m)
	return fitBlock(header+"\n"+transcript+"\n"+footer, sz.Height)
}

func renderTranscript(sz LayoutSizes, m *Model) string {
	buf := m.store.Buffer(m.chatID)
	innerW := sz.ChatW - 1
	if innerW < 1 {
		innerW = 1
	}
	lines := flattenTranscript(buf.VisibleBlocks(), innerW, m.settings.ShowSteps)
	m.vp.Height = sz.TranscriptH
	m.vp.SetContentLines(len(lines))
	start, end := m.vp.VisibleRange()
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		start = end
	}
	visible := lines[start:end]
	for len(visible) < sz.TranscriptH {
		visible = append([]string{""}, visible...)
	}
	if len(visible) > sz.TranscriptH {
		visible = visible[len(visible)-sz.TranscriptH:]
	}
	transcript := strings.Join(visible, "\n")
	scroll := renderScrollbar(sz.TranscriptH, m.vp)
	panel := theme.StyleRoot.Width(innerW).Height(sz.TranscriptH).MaxHeight(sz.TranscriptH).Render(transcript)
	return lipgloss.JoinHorizontal(lipgloss.Top, panel, scroll)
}

func fitBlock(s string, height int) string {
	if height < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func renderScrollbar(trackH int, vp Viewport) string {
	if trackH <= 0 {
		return ""
	}
	var b strings.Builder
	thumb := vp.ThumbPosition(trackH)
	for i := 0; i < trackH; i++ {
		if i == thumb {
			b.WriteString(theme.StylePurple.Render("┃"))
		} else {
			b.WriteString(theme.StyleDim.Render("│"))
		}
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func renderFooter(sz LayoutSizes, m *Model) string {
	width := sz.ChatW
	if width < 1 {
		width = 1
	}
	var b strings.Builder
	b.WriteString(theme.StyleBorder.Width(width).Render(strings.Repeat("─", width)))
	b.WriteString("\n")
	if m.workPathOn {
		draft := m.workPathDraft
		if draft == "" {
			draft = "/path/to/project"
		}
		b.WriteString(theme.StyleGreen.Width(width).Render("▸ " + draft))
		b.WriteString("\n")
		hint := "Enter works in this folder · Esc back"
		if m.workPathErr != "" {
			hint = m.workPathErr
		}
		b.WriteString(theme.StyleMuted.Width(width).Render(hint))
		return b.String()
	}
	if m.addingModel {
		b.WriteString(renderAddModelForm(m, width, sz.MenuLines))
		b.WriteString("\n")
		hint := "Enter adds this model · Esc back · blank price hides cost"
		if m.modelScopeLabel != "" {
			hint = "Add to " + m.modelScopeLabel + " · Enter adds · blank price hides cost"
		}
		if m.addModelErr != "" {
			hint = m.addModelErr
		}
		b.WriteString(theme.StyleMuted.Width(width).Render(hint))
		return b.String()
	}
	if m.connectOn {
		if m.connectListing {
			b.WriteString(renderConnectList(m, width, sz.MenuLines))
			b.WriteString("\n")
			hint := "Enter uses this API · Add API saves another · Esc closes"
			if m.connectErr != "" {
				hint = m.connectErr
			}
			b.WriteString(theme.StyleMuted.Width(width).Render(hint))
			return b.String()
		}
		b.WriteString(renderConnectForm(m, width, sz.MenuLines))
		b.WriteString("\n")
		hint := "Enter saves this API · Esc back · blank price hides cost"
		if m.connectErr != "" {
			hint = m.connectErr
		}
		b.WriteString(theme.StyleMuted.Width(width).Render(hint))
		return b.String()
	}
	if m.menu != MenuNone {
		b.WriteString(renderMenuFooter(m, width, sz.MenuLines))
		b.WriteString("\n")
	}
	input := m.draft
	if input == "" {
		input = " "
	}
	b.WriteString(theme.StyleUser.Width(width).Render("❯ " + input))
	b.WriteString("\n")
	hint := "= view    * mention    ctrl+o tools"
	if m.routing {
		hint += "     " + theme.StyleGreen.Render("routing…")
	}
	b.WriteString(theme.StyleMuted.Width(width).Render(hint))
	return b.String()
}

func renderMenuFooter(m *Model, width, lines int) string {
	items := m.menuItems
	selected := m.menuIndex
	if len(items) == 0 {
		items = []string{"Nothing to show"}
		selected = 0
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= len(items) {
		selected = len(items) - 1
	}
	if lines < 1 {
		lines = len(items)
	}
	view, sel := windowStrings(items, selected, lines)
	var b strings.Builder
	for i, item := range view {
		label := item
		switch m.menu {
		case MenuRoles:
			label = "*" + item + " "
		case MenuView:
			label = "=" + item
		}
		var line string
		if i == sel {
			hint := "  (↑↓ select · Enter · Esc)"
			if m.menu == MenuChats {
				hint = "  (↑↓ select · Enter · Del deletes · Esc)"
			}
			line = theme.StyleGreen.Render("▸ "+label) + theme.StyleMuted.Render(hint)
		} else {
			line = theme.StyleMuted.Render("  " + label)
		}
		if width > 0 {
			line = theme.StyleRoot.Width(width).MaxHeight(1).Render(line)
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(line)
	}
	return b.String()
}

func windowStrings(items []string, selected, n int) (view []string, sel int) {
	if n <= 0 || n >= len(items) {
		return items, selected
	}
	start := selected - n/2
	if start < 0 {
		start = 0
	}
	if start+n > len(items) {
		start = len(items) - n
	}
	return items[start : start+n], selected - start
}
