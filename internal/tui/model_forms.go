package tui

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// Modal form handlers for WorkDir and Add Model inputs (Connect API modal handlers live in connect.go).

func (m *Model) handleWorkPathKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.Shutdown()
		return m, tea.Quit
	case "esc":
		m.workPathOn = false
		m.workPathDraft = ""
		m.workPathErr = ""
		return m, nil
	case "enter":
		path := strings.TrimSpace(m.workPathDraft)
		if path == "" || m.backend == nil {
			m.workPathErr = "path is required"
			return m, nil
		}
		abs, err := m.backend.SetWorkDir(path)
		if err != nil {
			m.workPathErr = err.Error()
			return m, nil
		}
		m.workPathOn = false
		m.workPathDraft = ""
		m.workPathErr = ""
		src := SourceRef{ChatID: m.chatID, Name: "orchestrator", Kind: SourceOrchestrator}
		m.store.Buffer(m.chatID).ApplyLiveEvent(RouteTraceEvent{
			SourceRef: src, Text: "Working in " + abs,
		}, m.settings)
		return m, nil
	default:
		editFieldByKey(msg, func(fn func(string) string) {
			m.workPathDraft = fn(m.workPathDraft)
			m.workPathErr = ""
		})
	}
	return m, nil
}

func (m *Model) handleAddModelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.Shutdown()
		return m, tea.Quit
	case "esc":
		m.addingModel = false
		m.addModelFields = [3]string{}
		m.addModelErr = ""
		m.menu = MenuModels
		m.loadMenuItems()
		return m, nil
	case "tab", "down":
		m.addModelIndex = (m.addModelIndex + 1) % len(addModelLabels)
		m.addModelErr = ""
		return m, nil
	case "shift+tab", "up":
		m.addModelIndex--
		if m.addModelIndex < 0 {
			m.addModelIndex = len(addModelLabels) - 1
		}
		m.addModelErr = ""
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.addModelFields[0])
		if name == "" || m.backend == nil {
			m.addModelErr = "model name is required"
			return m, nil
		}
		if err := m.backend.AddProviderModel(m.chatID, m.modelScope, name, m.addModelFields[1], m.addModelFields[2]); err != nil {
			m.addModelErr = err.Error()
			return m, nil
		}
		m.addingModel = false
		m.addModelFields = [3]string{}
		m.addModelErr = ""
		m.modelName = m.backend.CurrentModel(m.chatID)
		m.menu = MenuModels
		m.menuIndex = 0
		m.loadMenuItems()
		for i, id := range m.menuIDs {
			if id == name {
				m.menuIndex = i
				break
			}
		}
		return m, nil
	default:
		editFieldByKey(msg, m.editAddModel)
	}
	return m, nil
}

func (m *Model) editAddModel(fn func(string) string) {
	if m.addModelIndex < 0 || m.addModelIndex >= len(m.addModelFields) {
		return
	}
	m.addModelFields[m.addModelIndex] = fn(m.addModelFields[m.addModelIndex])
	m.addModelErr = ""
}

func editFieldByKey(msg tea.KeyMsg, apply func(func(string) string)) {
	switch msg.String() {
	case "backspace":
		apply(func(s string) string {
			if s == "" {
				return s
			}
			_, size := utf8.DecodeLastRuneInString(s)
			return s[:len(s)-size]
		})
	case "ctrl+u":
		apply(func(string) string { return "" })
	case " ", "space":
		apply(func(s string) string { return s + " " })
	default:
		if msg.Type == tea.KeyRunes {
			r := string(msg.Runes)
			apply(func(s string) string { return s + r })
		}
	}
}
