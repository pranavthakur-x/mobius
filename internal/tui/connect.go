package tui

import (
	"strings"
	"unicode/utf8"

	"mobius/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) openConnect() {
	m.menu = MenuNone
	m.menuItems = nil
	m.connectOn = true
	m.connectErr = ""
	saved := 0
	if m.backend != nil {
		saved = len(m.backend.ListAPIs())
	}
	m.loadAPIList()
	if saved == 0 {
		m.openConnectForm(false)
		return
	}
	m.connectListing = true
	m.connectPick = 0
}

func (m *Model) handleConnectListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.Shutdown()
		return m, tea.Quit
	case "esc":
		m.connectOn = false
		m.connectErr = ""
		return m, nil
	case "up":
		if len(m.connectItems) == 0 {
			return m, nil
		}
		m.connectPick--
		if m.connectPick < 0 {
			m.connectPick = len(m.connectItems) - 1
		}
		return m, nil
	case "down":
		if len(m.connectItems) == 0 {
			return m, nil
		}
		m.connectPick++
		if m.connectPick >= len(m.connectItems) {
			m.connectPick = 0
		}
		return m, nil
	case "enter":
		if m.connectPick < 0 || m.connectPick >= len(m.connectIDs) {
			return m, nil
		}
		id := m.connectIDs[m.connectPick]
		if id == "" {
			m.openConnectForm(true)
			return m, nil
		}
		if m.backend != nil {
			if err := m.backend.UseAPI(id); err != nil {
				m.connectErr = err.Error()
				return m, nil
			}
			m.modelName = m.backend.CurrentModel(m.chatID)
		}
		m.connectOn = false
		m.refreshSidebar()
		return m, nil
	}
	return m, nil
}

func renderConnectList(m *Model, width, lines int) string {
	items := m.connectItems
	if len(items) == 0 {
		items = []string{"Add API"}
	}
	selected := m.connectPick
	if selected < 0 || selected >= len(items) {
		selected = 0
	}
	if lines < 1 {
		lines = len(items)
	}
	view, sel := windowStrings(items, selected, lines)
	var b strings.Builder
	for i, item := range view {
		var line string
		if i == sel {
			line = theme.StyleGreen.Render("▸ " + item)
		} else {
			line = theme.StyleMuted.Render("  " + item)
		}
		if width > 0 {
			line = lipgloss.NewStyle().Width(width).MaxHeight(1).Render(line)
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(line)
	}
	return b.String()
}

func (m *Model) loadAPIList() {
	m.connectItems = nil
	m.connectIDs = nil
	if m.backend == nil {
		m.connectItems = []string{"Add API"}
		m.connectIDs = []string{""}
		return
	}
	for _, api := range m.backend.ListAPIs() {
		label := api.Label
		if api.Current {
			label += " · current"
		}
		m.connectItems = append(m.connectItems, label)
		m.connectIDs = append(m.connectIDs, api.ID)
	}
	m.connectItems = append(m.connectItems, "Add API")
	m.connectIDs = append(m.connectIDs, "")
}

func (m *Model) openConnectForm(back bool) {
	m.connectListing = false
	m.connectBack = back
	m.connectIndex = 0
	m.connectErr = ""
	m.connectFields = [6]string{}
	m.connectKeepKey = false
	m.connectHadKey = false
}

func (m *Model) handleConnectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.connectListing {
		return m.handleConnectListKey(msg)
	}
	switch msg.String() {
	case "ctrl+c":
		m.Shutdown()
		return m, tea.Quit
	case "esc":
		if m.connectBack {
			m.connectListing = true
			m.connectErr = ""
			m.loadAPIList()
			return m, nil
		}
		m.connectOn = false
		m.connectFields = [6]string{}
		m.connectErr = ""
		return m, nil
	case "tab", "down":
		m.connectIndex = (m.connectIndex + 1) % len(connectLabels)
		m.connectErr = ""
		return m, nil
	case "shift+tab", "up":
		m.connectIndex--
		if m.connectIndex < 0 {
			m.connectIndex = len(connectLabels) - 1
		}
		m.connectErr = ""
		return m, nil
	case "enter":
		m.submitConnect()
		return m, nil
	case "backspace":
		m.editConnect(func(s string) string {
			if s == "" {
				return s
			}
			_, size := utf8.DecodeLastRuneInString(s)
			return s[:len(s)-size]
		})
		return m, nil
	case "ctrl+u":
		m.editConnect(func(string) string { return "" })
		return m, nil
	case " ", "space":
		m.editConnect(func(s string) string { return s + " " })
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.editConnect(func(s string) string { return s + string(msg.Runes) })
		}
	}
	return m, nil
}

func (m *Model) editConnect(fn func(string) string) {
	if m.connectIndex < 0 || m.connectIndex >= len(m.connectFields) {
		return
	}
	m.connectFields[m.connectIndex] = fn(m.connectFields[m.connectIndex])
	if m.connectIndex == 2 {
		m.connectKeepKey = m.connectFields[2] == "" && m.connectHadKey
	}
	m.connectErr = ""
}

func (m *Model) submitConnect() {
	if m.backend == nil {
		m.connectErr = "connect is unavailable"
		return
	}
	err := m.backend.SaveConnection(m.chatID, ConnectionInput{
		Name:            m.connectFields[0],
		BaseURL:         m.connectFields[1],
		APIKey:          m.connectFields[2],
		Model:           m.connectFields[3],
		PromptPrice:     m.connectFields[4],
		CompletionPrice: m.connectFields[5],
	})
	if err != nil {
		m.connectErr = err.Error()
		return
	}
	m.connectOn = false
	m.connectFields = [6]string{}
	m.connectErr = ""
	m.modelName = m.backend.CurrentModel(m.chatID)
	m.refreshSidebar()
}

func renderAddModelForm(m *Model, width, lines int) string {
	items := make([]string, len(addModelLabels))
	for i, label := range addModelLabels {
		items[i] = label + ": " + m.addModelFields[i]
	}
	return renderPicker(items, m.addModelIndex, width, lines)
}

func renderConnectForm(m *Model, width, lines int) string {
	items := make([]string, len(connectLabels))
	for i, label := range connectLabels {
		val := m.connectFields[i]
		if i == 2 {
			if val != "" {
				val = strings.Repeat("•", utf8.RuneCountInString(val))
			} else if m.connectKeepKey {
				val = "(saved)"
			}
		}
		items[i] = label + ": " + val
	}
	return renderPicker(items, m.connectIndex, width, lines)
}

func renderPicker(items []string, selected, width, lines int) string {
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
		var line string
		if i == sel {
			line = theme.StyleGreen.Render("▸ " + item)
		} else {
			line = theme.StyleMuted.Render("  " + item)
		}
		if width > 0 {
			line = lipgloss.NewStyle().Width(width).MaxHeight(1).Render(line)
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(line)
	}
	return b.String()
}
