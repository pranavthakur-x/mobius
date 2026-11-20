package tui

import (
	"context"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// liveEventMsg wraps one LiveEvent for the Update loop.
type liveEventMsg struct {
	ev LiveEvent
}

type dispatchDoneMsg struct {
	handle DispatchHandle
	done   DispatchDone
}

// Model is the Bubble Tea application state.
type Model struct {
	backend  Backend
	settings Settings
	ctx      context.Context
	cancel   context.CancelFunc
	events   chan LiveEvent

	store   *ChatStore
	ledger  *BudgetLedger
	decoder *LineDecoder
	vp      Viewport

	width, height int
	chatID        string
	chatName      string
	modelName     string
	draft         string
	composeLines  int
	menu          FooterMenu
	menuIndex     int
	menuItems     []string
	menuIDs       []string
	roleNames     []string
	routing       bool

	sidebarBreakdown ContextBreakdown
	subCancel        context.CancelFunc

	connectOn      bool
	connectListing bool
	connectBack    bool
	connectItems   []string
	connectIDs     []string
	connectPick    int
	connectIndex   int
	connectFields  [6]string
	connectHadKey  bool
	connectKeepKey bool
	connectErr     string

	modelScope      string
	modelScopeLabel string
	addingModel     bool
	addModelIndex   int
	addModelFields  [3]string
	addModelErr     string

	workPathOn    bool
	workPathDraft string
	workPathErr   string
}

func NewModel(backend Backend, settings Settings, width, height int) (*Model, error) {
	chat, err := backend.ActiveChat()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{
		backend:      backend,
		settings:     settings,
		ctx:          ctx,
		cancel:       cancel,
		events:       make(chan LiveEvent, 256),
		store:        NewChatStore(chat.ID),
		ledger:       NewBudgetLedger(),
		decoder:      NewLineDecoder(),
		vp:           NewViewport(),
		width:        width,
		height:       height,
		chatID:       chat.ID,
		chatName:     chat.Name,
		modelName:    backend.CurrentModel(chat.ID),
		composeLines: 1,
	}
	m.hydrateChats()
	m.refreshTeams()
	m.refreshSidebar()
	m.subCancel = backend.Subscribe(chat.ID, m.enqueueEvent)
	return m, nil
}

func (m *Model) hydrateChats() {
	if m.backend == nil {
		return
	}
	for _, chat := range m.backend.ListChats() {
		buf := m.store.Buffer(chat.ID)
		for _, turn := range m.backend.History(chat.ID) {
			applyHistoryTurn(buf, chat.ID, turn, m.settings)
		}
	}
}

func (m *Model) remember(chatID, role, speaker, text string) {
	if m.backend == nil || strings.TrimSpace(text) == "" {
		return
	}
	m.backend.Remember(chatID, role, speaker, text)
	if chatID != m.chatID {
		return
	}
	for _, chat := range m.backend.ListChats() {
		if chat.ID == chatID {
			m.chatName = chat.Name
			return
		}
	}
}

func (m *Model) enqueueEvent(ev LiveEvent) {
	select {
	case m.events <- ev:
	case <-m.ctx.Done():
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		waitLiveEvent(m.events, m.ctx),
		tea.EnableMouseCellMotion,
	)
}

func waitLiveEvent(ch <-chan LiveEvent, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		select {
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			return liveEventMsg{ev: ev}
		case <-ctx.Done():
			return nil
		}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonWheelUp {
			m.vp.ScrollUp(3)
			buf := m.store.Buffer(m.chatID)
			buf.AutoFollow = m.vp.AutoFollow
		}
		if msg.Button == tea.MouseButtonWheelDown {
			m.vp.ScrollDown(3)
			buf := m.store.Buffer(m.chatID)
			buf.AutoFollow = m.vp.AutoFollow
		}
		return m, nil
	case liveEventMsg:
		cmd := m.applyLive(msg.ev)
		return m, tea.Batch(cmd, waitLiveEvent(m.events, m.ctx))
	case orchestratorReplyMsg:
		m.showOrchestratorDecision(msg.chatID, msg.runID, msg.trace, msg.reply)
		m.routing = false
		m.refreshSidebar()
		return m, waitLiveEvent(m.events, m.ctx)
	case dispatchStartedMsg:
		m.showOrchestratorDecision(msg.handle.ChatID, msg.handle.RunID, msg.handle.Trace, "")
		m.routing = false
		m.refreshSidebar()
		if msg.handle.Done == nil {
			return m, waitLiveEvent(m.events, m.ctx)
		}
		return m, tea.Batch(waitLiveEvent(m.events, m.ctx), waitDispatch(msg.handle))
	case dispatchDoneMsg:
		m.routing = false
		src := SourceRef{ChatID: msg.handle.ChatID, Name: msg.handle.RoleID}
		if w, ok := m.lookupRoleName(msg.handle.TeamID, msg.handle.RoleID); ok {
			src.Name = w
			src.RoleID = msg.handle.RoleID
			src.Kind = SourceWorker
		} else {
			src.Name = "orchestrator"
			src.Kind = SourceOrchestrator
		}
		if msg.done.Err != nil {
			m.remember(msg.handle.ChatID, "error", src.Name, msg.done.Err.Error())
			m.store.Buffer(msg.handle.ChatID).ApplyLiveEvent(RunErrorEvent{
				SourceRef: src,
				Text:      msg.done.Err.Error(),
				Key:       src.Name + ":" + msg.done.Err.Error(),
			}, m.settings)
			m.store.Buffer(msg.handle.ChatID).ApplyLiveEvent(WorkerStateEvent{
				SourceRef: src, State: WorkerError,
			}, m.settings)
		} else {
			m.remember(msg.handle.ChatID, "assistant", src.Name, msg.done.Reply)
			m.store.Buffer(msg.handle.ChatID).ApplyLiveEvent(FinalReplyEvent{
				SourceRef: src,
				Text:      msg.done.Reply,
				RunID:     msg.handle.RunID,
			}, m.settings)
			m.store.Buffer(msg.handle.ChatID).ApplyLiveEvent(WorkerStateEvent{
				SourceRef: src, State: WorkerDone,
			}, m.settings)
		}
		m.refreshSidebar()
		return m, waitLiveEvent(m.events, m.ctx)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

type dispatchStartedMsg struct {
	handle DispatchHandle
}

func (m *Model) showOrchestratorDecision(chatID, runID string, trace []string, reply string) {
	src := SourceRef{ChatID: chatID, Name: "orchestrator", Kind: SourceOrchestrator}
	buf := m.store.Buffer(chatID)
	for _, tr := range trace {
		buf.ApplyLiveEvent(RouteTraceEvent{SourceRef: src, Text: tr}, m.settings)
		m.remember(chatID, "trace", "orchestrator", tr)
	}
	if strings.TrimSpace(reply) == "" {
		return
	}
	buf.ApplyLiveEvent(FinalReplyEvent{SourceRef: src, Text: reply, RunID: runID}, m.settings)
	m.remember(chatID, "assistant", "orchestrator", reply)
}

func waitDispatch(handle DispatchHandle) tea.Cmd {
	return func() tea.Msg {
		if handle.Done == nil {
			return nil
		}
		d := <-handle.Done
		return dispatchDoneMsg{handle: handle, done: d}
	}
}

func (m *Model) applyLive(ev LiveEvent) tea.Cmd {
	switch e := ev.(type) {
	case BudgetEvent:
		m.ledger.Apply(e)
		m.refreshSidebar()
		return nil
	}
	buf := m.store.Buffer(ev.ChatID())
	buf.ApplyLiveEvent(ev, m.settings)
	if ev.ChatID() == m.chatID {
		m.syncViewport(buf)
	}
	return nil
}

func (m *Model) syncViewport(buf *ChatBuffer) {
	m.vp.AutoFollow = buf.AutoFollow
	m.vp.Unread = buf.Unread
	if !buf.AutoFollow {
		m.vp.Offset = buf.ScrollOff
	}
}

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
			m.vp.ScrollUp(1)
			buf := m.store.Buffer(m.chatID)
			buf.AutoFollow = m.vp.AutoFollow
			buf.ScrollOff = m.vp.Offset
		}
		return m, nil
	case "down":
		if m.draft == "" {
			m.vp.ScrollDown(1)
			buf := m.store.Buffer(m.chatID)
			buf.AutoFollow = m.vp.AutoFollow
			buf.ScrollOff = m.vp.Offset
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
	case " ", "space":
		m.draft += " "
		m.updateComposeLines()
		return m, nil
	case "backspace":
		if len(m.draft) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.draft)
			m.draft = m.draft[:len(m.draft)-size]
			m.updateComposeLines()
		}
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
					m.draft += string(r)
					m.updateComposeLines()
				}
			}
		}
	}
	return m, nil
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
		m.handleSlash(text)
		return m, nil
	}
	m.routing = true
	return m, m.dispatchAsync(text, runID)
}

type orchestratorReplyMsg struct {
	chatID string
	reply  string
	trace  []string
	runID  string
}

func (m *Model) dispatchAsync(text, runID string) tea.Cmd {
	backend := m.backend
	chatID := m.chatID
	return func() tea.Msg {
		handle, err := backend.Dispatch(context.Background(), chatID, text, runID)
		if err != nil {
			return dispatchDoneMsg{handle: DispatchHandle{ChatID: chatID, RunID: runID}, done: DispatchDone{Err: err}}
		}
		if handle.Done == nil {
			return orchestratorReplyMsg{chatID: chatID, reply: handle.Reply, trace: handle.Trace, runID: runID}
		}
		return dispatchStartedMsg{handle: handle}
	}
}

func (m *Model) handleSlash(cmd string) {
	switch cmd {
	case "/exit":
		m.Shutdown()
		return
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
}

func (m *Model) switchChat(chat ChatSnapshot) {
	if m.subCancel != nil {
		m.subCancel()
	}
	m.chatID = chat.ID
	m.chatName = chat.Name
	m.modelName = m.backend.CurrentModel(chat.ID)
	m.store.SwitchActive(chat.ID)
	m.vp.PinBottom()
	m.refreshTeams()
	m.refreshSidebar()
	m.subCancel = m.backend.Subscribe(chat.ID, m.enqueueEvent)
}

func (m *Model) insertRune(r rune) *Model {
	m.draft += string(r)
	m.updateComposeLines()
	return m
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

func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	return FitScreen(m)
}

func (m *Model) viewLabel() string {
	buf := m.store.Buffer(m.chatID)
	if buf.ViewFilter == "" {
		return "All"
	}
	return m.roleDisplay(buf.ViewFilter)
}

func (m *Model) viewOptions() []string {
	opts := []string{"All"}
	for _, r := range m.roleIDs() {
		opts = append(opts, m.roleDisplay(r))
	}
	return opts
}

func (m *Model) loadRoleNames() {
	m.roleNames = nil
	for _, t := range m.backend.Catalog(m.chatID) {
		for _, r := range t.Roles {
			m.roleNames = append(m.roleNames, r.Name)
		}
	}
}

func (m *Model) roleIDs() []string {
	var ids []string
	for _, t := range m.backend.Catalog(m.chatID) {
		for _, r := range t.Roles {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

func (m *Model) roleDisplay(roleID string) string {
	for _, t := range m.backend.Catalog(m.chatID) {
		for _, r := range t.Roles {
			if r.ID == roleID {
				return r.Name
			}
		}
	}
	return roleID
}

func (m *Model) lookupRoleName(teamID, roleID string) (string, bool) {
	for _, t := range m.backend.Catalog(m.chatID) {
		if t.ID != teamID && teamID != "" {
			continue
		}
		for _, r := range t.Roles {
			if r.ID == roleID {
				return r.Name, true
			}
		}
	}
	return "", false
}

func (m *Model) refreshTeams() {
	cat := m.backend.Catalog(m.chatID)
	var lines []TeamLine
	for _, t := range cat {
		for _, r := range t.Roles {
			lines = append(lines, TeamLine{
				DeptName: t.Name,
				RoleID:   r.ID,
				Name:     r.Name,
				State:    WorkerIdle,
			})
		}
	}
	m.store.Buffer(m.chatID).SetTeams(lines)
}

func (m *Model) refreshSidebar() {
	buf := m.store.Buffer(m.chatID)
	snap := m.backend.ContextFor(m.chatID, buf.ViewFilter)
	snap.Breakdown.NoteConversation(EstimateTranscript(buf.VisibleBlocks()))
	if snap.Breakdown.HasPrice {
		if tot := m.ledger.Total(); tot.CostUSD > snap.Breakdown.CostUSD {
			snap.Breakdown.CostUSD = tot.CostUSD
		}
	} else {
		snap.Breakdown.CostUSD = 0
	}
	m.sidebarBreakdown = snap.Breakdown
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
	case "backspace":
		if m.workPathDraft != "" {
			_, size := utf8.DecodeLastRuneInString(m.workPathDraft)
			m.workPathDraft = m.workPathDraft[:len(m.workPathDraft)-size]
		}
		m.workPathErr = ""
		return m, nil
	case "ctrl+u":
		m.workPathDraft = ""
		m.workPathErr = ""
		return m, nil
	case " ", "space":
		m.workPathDraft += " "
		m.workPathErr = ""
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.workPathDraft += string(msg.Runes)
			m.workPathErr = ""
		}
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
	case "backspace":
		m.editAddModel(func(s string) string {
			if s == "" {
				return s
			}
			_, size := utf8.DecodeLastRuneInString(s)
			return s[:len(s)-size]
		})
		return m, nil
	case "ctrl+u":
		m.editAddModel(func(string) string { return "" })
		return m, nil
	case " ", "space":
		m.editAddModel(func(s string) string { return s + " " })
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.editAddModel(func(s string) string { return s + string(msg.Runes) })
		}
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

// Shutdown stops listeners; do not close the event channel while hooks may still fire.
func (m *Model) Shutdown() {
	m.cancel()
	if m.subCancel != nil {
		m.subCancel()
	}
}
