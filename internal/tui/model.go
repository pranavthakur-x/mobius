package tui

import (
	"context"
	"strings"

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

type dispatchStartedMsg struct {
	handle DispatchHandle
}

type orchestratorReplyMsg struct {
	chatID string
	reply  string
	trace  []string
	runID  string
}

// Model is the Bubble Tea application state for Mobius's terminal UI.
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

// NewModel constructs a Model hydrated with active chat history, team catalog, and live event subscription.
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
		events:       make(chan LiveEvent, 1024),
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

// Init starts the background live-event listener and enables mouse cell motion.
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

// Update advances the TUI state machine in response to terminal, mouse, key, and live-event messages.
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
		// Drain already-buffered bursts (e.g. fast SSE token streams) in one render pass.
		for i := 0; i < 64; i++ {
			select {
			case next, ok := <-m.events:
				if !ok {
					return m, cmd
				}
				_ = m.applyLive(next)
			default:
				return m, tea.Batch(cmd, waitLiveEvent(m.events, m.ctx))
			}
		}
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

// View renders the full-screen terminal frame constrained to the current terminal dimensions.
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	return FitScreen(m)
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

// Shutdown stops listeners; do not close the event channel while hooks may still fire.
func (m *Model) Shutdown() {
	m.cancel()
	if m.subCancel != nil {
		m.subCancel()
	}
}
