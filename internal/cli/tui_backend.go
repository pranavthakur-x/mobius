package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"mobius/internal/tui"
	"mobius/pkg/agent"
	"mobius/pkg/llm"
	"mobius/pkg/orchestrator"
	"mobius/pkg/session"
)

var _ tui.Backend = (*TUIBackend)(nil)

// TUIBackend adapts App wiring to the tui.Backend interface for interactive
// fullscreen TUI sessions.
type TUIBackend struct {
	app *App

	mu          sync.Mutex
	hookCancels map[string]context.CancelFunc
	decoders    map[string]*tui.LineDecoder
}

// NewTUIBackend constructs a TUIBackend backed by app.
func NewTUIBackend(app *App) *TUIBackend {
	return &TUIBackend{
		app:         app,
		hookCancels: make(map[string]context.CancelFunc),
		decoders:    make(map[string]*tui.LineDecoder),
	}
}

// ActiveChat returns a snapshot of the currently active chat session.
func (b *TUIBackend) ActiveChat() (tui.ChatSnapshot, error) {
	s, err := b.app.Sessions.GetActive()
	if err != nil {
		return tui.ChatSnapshot{}, err
	}
	return tui.ChatSnapshot{ID: s.ID, Name: s.Name()}, nil
}

// ListChats returns snapshots of all tracked chat sessions.
func (b *TUIBackend) ListChats() []tui.ChatSnapshot {
	list := b.app.Sessions.ListSession()
	out := make([]tui.ChatSnapshot, 0, len(list))
	for _, it := range list {
		out = append(out, tui.ChatSnapshot{ID: it.ID, Name: it.Name})
	}
	return out
}

// NewChat activates an existing unstarted chat or provisions a new chat
// session and workspace.
func (b *TUIBackend) NewChat(ctx context.Context) (tui.ChatSnapshot, error) {
	if unstarted := b.app.Sessions.GetUnstarted(); unstarted != nil {
		_, _ = b.app.Sessions.SwitchSession(unstarted.ID)
		return tui.ChatSnapshot{ID: unstarted.ID, Name: unstarted.Name()}, nil
	}
	provider, err := b.app.Config.GetProviderForModel("")
	if err != nil {
		return tui.ChatSnapshot{}, err
	}
	spec := b.app.ActiveModelSpec(provider)
	ws, err := orchestrator.NewWorkspace(ctx, "new-chat", b.app.RoleStore, b.app.BaseTools, spec, b.app.AgentFactory())
	if err != nil {
		return tui.ChatSnapshot{}, err
	}
	orch := ws.OrchestratorSession()
	b.app.attachSensors(orch.Agent)
	newSess := b.app.Sessions.CreateSession("New Chat", orch.Agent)
	newSess.MarkStarted("New Chat")
	b.app.Workspaces.Register(newSess.ID, ws)
	_ = session.EnsureChat(b.app.ChatDir, newSess.ID, newSess.Name())
	return tui.ChatSnapshot{ID: newSess.ID, Name: newSess.Name()}, nil
}

// SwitchChat activates chatID and persists the active chat selection.
func (b *TUIBackend) SwitchChat(chatID string) error {
	_, err := b.app.Sessions.SwitchSession(chatID)
	if err == nil {
		_ = session.SetActiveChat(b.app.ChatDir, chatID)
	}
	return err
}

// DeleteChat closes chatID's workspace, removes its persisted chat and event
// history, and returns the next active chat or creates a new one.
func (b *TUIBackend) DeleteChat(ctx context.Context, chatID string) (tui.ChatSnapshot, error) {
	if ws, ok := b.app.Workspaces.Get(chatID); ok {
		ws.Close()
		b.app.Workspaces.Remove(chatID)
	}
	next, err := b.app.Sessions.Remove(chatID)
	if err != nil {
		return tui.ChatSnapshot{}, err
	}
	eventsDir := filepath.Join(filepath.Dir(b.app.ChatDir), "events")
	if err := session.DeleteChat(b.app.ChatDir, eventsDir, chatID); err != nil {
		return tui.ChatSnapshot{}, err
	}
	if next == nil {
		return b.NewChat(ctx)
	}
	_ = session.SetActiveChat(b.app.ChatDir, next.ID)
	return tui.ChatSnapshot{ID: next.ID, Name: next.Name()}, nil
}

// History returns the recorded transcript turns for chatID.
func (b *TUIBackend) History(chatID string) []tui.HistoryTurn {
	turns, err := session.History(b.app.ChatDir, chatID)
	if err != nil {
		return nil
	}
	out := make([]tui.HistoryTurn, 0, len(turns))
	for _, turn := range turns {
		out = append(out, tui.HistoryTurn{Role: turn.Role, Speaker: turn.Speaker, Text: turn.Text})
	}
	return out
}

// Remember appends a visible transcript turn for chatID.
func (b *TUIBackend) Remember(chatID, role, speaker, text string) {
	b.app.RememberTurn(chatID, role, speaker, text)
}

// SessionID returns the active session ID, or an empty string if none is active.
func (b *TUIBackend) SessionID() string {
	if s, err := b.app.Sessions.GetActive(); err == nil {
		return s.ID
	}
	return ""
}

// ContextFor computes a token and cost breakdown snapshot for chatID, scoped
// to the orchestrator when viewRoleID is empty or to the matching worker role.
func (b *TUIBackend) ContextFor(chatID, viewRoleID string) tui.ContextSnapshot {
	ws, err := b.workspace(chatID)
	if err != nil {
		return tui.ContextSnapshot{Breakdown: tui.ComputeBreakdown(nil, nil, 0)}
	}
	var msgs []llm.Message
	var toolDefs []llm.ToolDefinition
	if viewRoleID == "" {
		if orch := ws.OrchestratorSession(); orch != nil && orch.Context != nil {
			msgs = orch.Context.Messages()
		}
		toolDefs = b.chatToolDefs(ws, "")
	} else {
		for _, t := range ws.Manager().CatalogSnapshot().Teams {
			for _, r := range t.Roles {
				if r.ID != viewRoleID {
					continue
				}
				if w, ok := ws.Manager().WorkerFor(t.ID, r.ID); ok && w.Session != nil && w.Session.Context != nil {
					msgs = w.Session.Context.Messages()
					toolDefs = toolDefsForRole(b.app.RoleStore, b.app.BaseTools, r.ID)
				}
			}
		}
	}
	cost := 0.0
	orch := ws.OrchestratorSession()
	if orch != nil {
		cost += agentBudgetCost(orch.Agent)
	}
	for _, team := range ws.Manager().CatalogSnapshot().Teams {
		for _, r := range team.Roles {
			if w, ok := ws.Manager().WorkerFor(team.ID, r.ID); ok && w.Session != nil {
				cost += agentBudgetCost(w.Session.Agent)
			}
		}
	}
	bd := tui.ComputeBreakdown(msgs, toolDefs, cost)
	model := ""
	if orch != nil && orch.Agent != nil {
		model = orch.Agent.GetModel()
	}
	bd.HasPrice = b.app.Config.PricesKnown(model)
	if !bd.HasPrice {
		bd.CostUSD = 0
	}
	return tui.ContextSnapshot{Breakdown: bd}
}

func agentBudgetCost(ag *agent.Agent) float64 {
	if ag == nil {
		return 0
	}
	st := ag.BudgetStatus()
	if !strings.HasPrefix(st, "[Budget]") {
		return 0
	}
	var cost float64
	for _, ev := range tui.NewLineDecoder().Decode(tui.SourceRef{ThreadID: ag.ThreadID()}, st) {
		if be, ok := ev.(tui.BudgetEvent); ok {
			cost += be.CostUSD
		}
	}
	return cost
}

func (b *TUIBackend) workspace(chatID string) (*orchestrator.Workspace, error) {
	ws, ok := b.app.Workspaces.Get(chatID)
	if !ok {
		return nil, fmt.Errorf("no workspace for session %s", chatID)
	}
	return ws, nil
}

func (b *TUIBackend) session(chatID string) (*session.Session, error) {
	s, err := b.app.Sessions.GetActive()
	if err == nil && s.ID == chatID {
		return s, nil
	}
	// Fallback: scan listed sessions and activate the matching one.
	for _, info := range b.app.Sessions.ListSession() {
		if info.ID == chatID {
			return b.app.Sessions.SwitchSession(chatID)
		}
	}
	return nil, fmt.Errorf("session %s not found", chatID)
}
