package tui

import (
	"context"
	"strings"

	"mobius/pkg/orchestrator"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.addingModel {
		return m.handleAddModelKey(msg)
	}
	if m.workPathOn {
		return m.handleWorkPathKey(msg)
	}
	if m.connectOn {
		return m.handleConnectKey(msg)
	}
	if m.menu != MenuNone {
		return m.handleMenuKey(msg)
	}
	switch msg.String() {
	case "ctrl+c":
		if m.inFlight || m.routing {
			m.cancelActiveTurn()
			return m, nil
		}
		m.Shutdown()
		return m, tea.Quit
	case "ctrl+o":
		buf := m.store.Buffer(m.chatID)
		buf.ToggleToolExpand()
		return m, nil
	case "enter":
		if msg.Alt {
			return m.insertRune('\n'), nil
		}
		return m.submit()
	case "shift+enter", "alt+enter":
		return m.insertRune('\n'), nil
	case "up":
		if m.draft == "" {
			m.scrollTranscriptLines(-1)
		}
		return m, nil
	case "down":
		if m.draft == "" {
			m.scrollTranscriptLines(1)
		}
		return m, nil
	case "pgup":
		m.vp.PageUp()
		return m, nil
	case "pgdown":
		m.vp.PageDown()
		return m, nil
	case "home":
		m.vp.ScrollUp(m.vp.ContentLines)
		return m, nil
	case "end":
		m.vp.PinBottom()
		return m, nil
	case "ctrl+u":
		m.vp.ScrollUp(m.vp.Height / 2)
		return m, nil
	case "ctrl+d":
		m.vp.ScrollDown(m.vp.Height / 2)
		return m, nil
	case "left":
		if n := len([]rune(m.draft)); m.cursorFromEnd < n {
			m.cursorFromEnd++
		}
		return m, nil
	case "right":
		if m.cursorFromEnd > 0 {
			m.cursorFromEnd--
		}
		return m, nil
	case "ctrl+a":
		m.cursorFromEnd = len([]rune(m.draft))
		return m, nil
	case "ctrl+e":
		m.cursorFromEnd = 0
		return m, nil
	case " ", "space":
		return m.insertRune(' '), nil
	case "backspace":
		m.deleteBeforeCursor()
		return m, nil
	case "delete", "del":
		m.deleteAtCursor()
		return m, nil
	case "esc":
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			for _, r := range msg.Runes {
				if trig := MenuTrigger(m.draft, r); trig != MenuNone {
					return m.openMenu(trig), nil
				}
				if r >= 32 {
					m.insertRune(r)
				}
			}
		}
	}
	return m, nil
}

func (m *Model) scrollTranscriptLines(delta int) {
	if delta < 0 {
		m.vp.ScrollUp(-delta)
	} else if delta > 0 {
		m.vp.ScrollDown(delta)
	}
	buf := m.store.Buffer(m.chatID)
	buf.AutoFollow = m.vp.AutoFollow
	buf.ScrollOff = m.vp.Offset
}

func (m *Model) openMenu(menu FooterMenu) *Model {
	m.menu = menu
	m.menuIndex = 0
	m.loadMenuItems()
	return m
}

func (m *Model) menuLineCount() int {
	if m.addingModel {
		return len(addModelLabels)
	}
	if m.workPathOn {
		return 1
	}
	if m.connectOn {
		if m.connectListing {
			n := len(m.connectItems)
			if n < 1 {
				return 1
			}
			return n
		}
		return len(connectLabels)
	}
	if m.menu == MenuNone {
		return 0
	}
	if len(m.menuItems) == 0 {
		return 1
	}
	return len(m.menuItems)
}

func (m *Model) loadMenuItems() {
	m.menuIDs = nil
	switch m.menu {
	case MenuActions:
		m.menuItems = append([]string(nil), actionLabels...)
	case MenuRoles:
		m.loadRoleNames()
		m.menuItems = append([]string(nil), m.roleNames...)
	case MenuView:
		m.menuItems = m.viewOptions()
	case MenuModels:
		m.menuItems = nil
		if m.backend == nil {
			return
		}
		if m.modelScope == "" {
			for _, group := range m.backend.ListProviders() {
				m.menuItems = append(m.menuItems, group.Label)
				m.menuIDs = append(m.menuIDs, group.ID)
			}
			return
		}
		for _, opt := range m.backend.ProviderModels(m.modelScope) {
			label := opt.ID
			if opt.Current {
				label += " · current"
			}
			m.menuItems = append(m.menuItems, label)
			m.menuIDs = append(m.menuIDs, opt.ID)
		}
		m.menuItems = append(m.menuItems, "Add model")
		m.menuIDs = append(m.menuIDs, addModelID)
	case MenuChats:
		m.menuItems = nil
		if m.backend == nil {
			return
		}
		for _, chat := range m.backend.ListChats() {
			m.menuItems = append(m.menuItems, chat.Name)
			m.menuIDs = append(m.menuIDs, chat.ID)
		}
	default:
		m.menuItems = nil
	}
}

func (m *Model) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "delete", "del":
		if m.menu == MenuChats {
			m.deleteSelectedChat()
			return m, nil
		}
		return m, nil
	case "esc":
		if m.menu == MenuModels && m.modelScope != "" {
			m.modelScope = ""
			m.modelScopeLabel = ""
			m.menuIndex = 0
			m.loadMenuItems()
			return m, nil
		}
		if m.menu == MenuModels || m.menu == MenuChats {
			m.menu = MenuActions
			m.menuIndex = 0
			m.loadMenuItems()
			return m, nil
		}
		m.menu = MenuNone
		m.menuItems = nil
		return m, nil
	case "up":
		n := len(m.menuItems)
		if n == 0 {
			return m, nil
		}
		m.menuIndex--
		if m.menuIndex < 0 {
			m.menuIndex = n - 1
		}
		return m, nil
	case "down":
		n := len(m.menuItems)
		if n == 0 {
			return m, nil
		}
		m.menuIndex++
		if m.menuIndex >= n {
			m.menuIndex = 0
		}
		return m, nil
	case "enter":
		return m.activateMenu()
	default:
		if m.menu == MenuActions && msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			m.menu = MenuNone
			m.draft += "/" + string(msg.Runes[0])
			m.updateComposeLines()
			return m, nil
		}
		if m.menu == MenuRoles && msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			m.menu = MenuNone
			m.draft += MentionFallback(msg.Runes[0])
			m.updateComposeLines()
			return m, nil
		}
		if m.menu == MenuView && msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
			m.menu = MenuNone
			m.draft += "=" + string(msg.Runes[0])
			m.updateComposeLines()
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) activateMenu() (tea.Model, tea.Cmd) {
	switch m.menu {
	case MenuActions:
		idx := m.menuIndex
		if idx < 0 {
			idx = 0
		}
		if idx >= len(actionLabels) {
			idx = len(actionLabels) - 1
		}
		switch idx {
		case ActionExit:
			m.menu = MenuNone
			m.Shutdown()
			return m, tea.Quit
		case ActionNewChat:
			m.menu = MenuNone
			chat, err := m.backend.NewChat(m.ctx)
			if err == nil {
				m.switchChat(chat)
			}
		case ActionModels:
			m.menu = MenuModels
			m.modelScope = ""
			m.modelScopeLabel = ""
			m.addingModel = false
			m.menuIndex = 0
			m.loadMenuItems()
			return m, nil
		case ActionConnect:
			m.openConnect()
			return m, nil
		case ActionChats:
			m.menu = MenuChats
			m.menuIndex = 0
			m.loadMenuItems()
			return m, nil
		case ActionTeams:
			m.menu = MenuNone
			text := m.backend.TeamsText(m.chatID)
			buf := m.store.Buffer(m.chatID)
			buf.ApplyLiveEvent(RouteTraceEvent{SourceRef: SourceRef{ChatID: m.chatID, Name: "orchestrator"}, Text: text}, m.settings)
		case ActionWorkDir:
			m.menu = MenuNone
			m.workPathOn = true
			m.workPathDraft = ""
			m.workPathErr = ""
			return m, nil
		case ActionHelp:
			m.menu = MenuNone
			buf := m.store.Buffer(m.chatID)
			runID := buf.BeginRun()
			buf.ApplyLiveEvent(FinalReplyEvent{
				SourceRef: SourceRef{ChatID: m.chatID, Name: "mobius", Kind: SourceOrchestrator},
				Text:      orchestrator.HelpText(),
				RunID:     runID,
			}, m.settings)
			return m, nil
		}
	case MenuModels:
		if m.backend == nil || m.menuIndex < 0 || m.menuIndex >= len(m.menuIDs) {
			m.menu = MenuNone
			break
		}
		id := m.menuIDs[m.menuIndex]
		if m.modelScope == "" {
			m.modelScope = id
			if m.menuIndex < len(m.menuItems) {
				m.modelScopeLabel = m.menuItems[m.menuIndex]
			}
			m.menuIndex = 0
			m.loadMenuItems()
			return m, nil
		}
		if id == addModelID {
			m.addingModel = true
			m.addModelIndex = 0
			m.addModelFields = [3]string{}
			m.addModelErr = ""
			return m, nil
		}
		if err := m.backend.UseProviderModel(m.chatID, m.modelScope, id); err == nil {
			m.modelName = m.backend.CurrentModel(m.chatID)
		}
		m.modelScope = ""
		m.menu = MenuNone
	case MenuChats:
		if m.backend != nil && m.menuIndex >= 0 && m.menuIndex < len(m.menuIDs) {
			id := m.menuIDs[m.menuIndex]
			name := ""
			if m.menuIndex < len(m.menuItems) {
				name = m.menuItems[m.menuIndex]
			}
			if err := m.backend.SwitchChat(id); err == nil {
				m.switchChat(ChatSnapshot{ID: id, Name: name})
			}
		}
		m.menu = MenuNone
	case MenuRoles:
		idx := m.menuIndex
		if idx >= 0 && idx < len(m.roleNames) {
			m.draft = MentionInsert(m.roleNames[idx])
			m.updateComposeLines()
		}
		m.menu = MenuNone
	case MenuView:
		opts := m.viewOptions()
		idx := m.menuIndex
		if idx >= 0 && idx < len(opts) {
			buf := m.store.Buffer(m.chatID)
			if opts[idx] == "All" {
				buf.ViewFilter = ""
			} else {
				for _, r := range m.roleIDs() {
					if m.roleDisplay(r) == opts[idx] {
						buf.ViewFilter = r
						break
					}
				}
			}
			m.refreshSidebar()
		}
		m.menu = MenuNone
	}
	return m, nil
}

func (m *Model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.draft)
	if text == "" {
		return m, nil
	}
	m.draft = ""
	m.cursorFromEnd = 0
	m.composeLines = 1
	buf := m.store.Buffer(m.chatID)
	runID := buf.BeginRun()
	buf.AppendUser(text, runID)
	if !strings.HasPrefix(text, "/") {
		m.remember(m.chatID, "user", "user", text)
	}
	m.vp.PinBottom()
	buf.AutoFollow = true
	if strings.HasPrefix(text, "/") {
		return m.handleSlash(text, runID)
	}
	m.clearTurn()
	turnCtx, turnCancel := context.WithCancel(m.ctx)
	m.turnCancel = turnCancel
	m.turnCanceled = false
	m.inFlight = true
	m.routing = true
	return m, m.dispatchAsync(turnCtx, text, runID)
}

func (m *Model) dispatchAsync(turnCtx context.Context, text, runID string) tea.Cmd {
	backend := m.backend
	chatID := m.chatID
	return func() tea.Msg {
		handle, err := backend.Dispatch(turnCtx, chatID, text, runID)
		if err != nil {
			return dispatchDoneMsg{handle: DispatchHandle{ChatID: chatID, RunID: runID}, done: DispatchDone{Err: err}}
		}
		if handle.Done == nil {
			return orchestratorReplyMsg{chatID: chatID, reply: handle.Reply, trace: handle.Trace, runID: runID}
		}
		return dispatchStartedMsg{handle: handle}
	}
}

func (m *Model) handleSlash(cmd, runID string) (tea.Model, tea.Cmd) {
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "/help":
		m.store.Buffer(m.chatID).ApplyLiveEvent(FinalReplyEvent{
			SourceRef: SourceRef{ChatID: m.chatID, Name: "mobius", Kind: SourceOrchestrator},
			Text:      orchestrator.HelpText(),
			RunID:     runID,
		}, m.settings)
		return m, nil
	case "/exit":
		m.Shutdown()
		return m, tea.Quit
	case "/newchat":
		if chat, err := m.backend.NewChat(m.ctx); err == nil {
			m.switchChat(chat)
		}
	case "/listchats":
		if chats := m.backend.ListChats(); len(chats) > 0 {
			_ = m.backend.SwitchChat(chats[len(chats)-1].ID)
			m.switchChat(chats[len(chats)-1])
		}
	case "/teams":
		text := m.backend.TeamsText(m.chatID)
		m.store.Buffer(m.chatID).ApplyLiveEvent(RouteTraceEvent{
			SourceRef: SourceRef{ChatID: m.chatID, Name: "orchestrator"}, Text: text,
		}, m.settings)
	}
	return m, nil
}

func (m *Model) cursorIndex(runes []rune) int {
	pos := len(runes) - m.cursorFromEnd
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	return pos
}

func (m *Model) insertRune(r rune) *Model {
	if m.cursorFromEnd <= 0 {
		m.cursorFromEnd = 0
		m.draft += string(r)
		m.updateComposeLines()
		return m
	}
	runes := []rune(m.draft)
	pos := m.cursorIndex(runes)
	next := make([]rune, 0, len(runes)+1)
	next = append(next, runes[:pos]...)
	next = append(next, r)
	next = append(next, runes[pos:]...)
	m.draft = string(next)
	m.updateComposeLines()
	return m
}

func (m *Model) deleteBeforeCursor() {
	runes := []rune(m.draft)
	pos := m.cursorIndex(runes)
	if pos <= 0 {
		return
	}
	next := make([]rune, 0, len(runes)-1)
	next = append(next, runes[:pos-1]...)
	next = append(next, runes[pos:]...)
	m.draft = string(next)
	m.updateComposeLines()
}

func (m *Model) deleteAtCursor() {
	runes := []rune(m.draft)
	pos := m.cursorIndex(runes)
	if pos >= len(runes) {
		return
	}
	next := make([]rune, 0, len(runes)-1)
	next = append(next, runes[:pos]...)
	next = append(next, runes[pos+1:]...)
	m.draft = string(next)
	if m.cursorFromEnd > 0 {
		m.cursorFromEnd--
	}
	m.updateComposeLines()
}

func (m *Model) updateComposeLines() {
	n := strings.Count(m.draft, "\n") + 1
	if n < 1 {
		n = 1
	}
	if n > 8 {
		n = 8
	}
	m.composeLines = n
}

func (m *Model) deleteSelectedChat() {
	if m.backend == nil || m.menuIndex < 0 || m.menuIndex >= len(m.menuIDs) {
		return
	}
	id := m.menuIDs[m.menuIndex]
	snap, err := m.backend.DeleteChat(m.ctx, id)
	if err != nil {
		return
	}
	if id == m.chatID {
		m.switchChat(snap)
	}
	m.loadMenuItems()
	if m.menuIndex >= len(m.menuItems) {
		m.menuIndex = len(m.menuItems) - 1
	}
	if m.menuIndex < 0 {
		m.menuIndex = 0
	}
}
