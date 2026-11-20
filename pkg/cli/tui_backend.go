package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"path/filepath"

	"mobius/pkg/agent"
	"mobius/pkg/cli/tui"
	"mobius/pkg/llm"
	"mobius/pkg/orchestrator"
	"mobius/pkg/roles"
	"mobius/pkg/session"
	"mobius/pkg/tools"
)

// TUIBackend implements tui.Backend using App wiring.
type TUIBackend struct {
	app *App

	mu          sync.Mutex
	hookCancels map[string]context.CancelFunc
	decoders    map[string]*tui.LineDecoder
}

func NewTUIBackend(app *App) *TUIBackend {
	return &TUIBackend{
		app:         app,
		hookCancels: make(map[string]context.CancelFunc),
		decoders:    make(map[string]*tui.LineDecoder),
	}
}

func (b *TUIBackend) ActiveChat() (tui.ChatSnapshot, error) {
	s, err := b.app.Sessions.GetActive()
	if err != nil {
		return tui.ChatSnapshot{}, err
	}
	return tui.ChatSnapshot{ID: s.ID, Name: s.Name()}, nil
}

func (b *TUIBackend) ListChats() []tui.ChatSnapshot {
	list := b.app.Sessions.ListSession()
	out := make([]tui.ChatSnapshot, 0, len(list))
	for _, it := range list {
		out = append(out, tui.ChatSnapshot{ID: it.ID, Name: it.Name})
	}
	return out
}

func (b *TUIBackend) ListModels() ([]tui.ModelOption, error) {
	models, err := b.app.Config.ListAllModels()
	if err != nil {
		return nil, err
	}
	cur := ""
	if s, err := b.app.Sessions.GetActive(); err == nil {
		cur = s.Agent.GetModel()
	}
	var out []tui.ModelOption
	for name, info := range models {
		id := name
		if info != nil && info.Model != "" {
			id = info.Model
		}
		out = append(out, tui.ModelOption{ID: id, URL: infoURL(info), Current: id == cur})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func infoURL(info *llm.ModelInfo) string {
	if info == nil {
		return ""
	}
	return info.BaseURL
}

func (b *TUIBackend) ListProviders() []tui.ProviderGroup {
	groups := b.app.Config.ProviderGroups()
	out := make([]tui.ProviderGroup, 0, len(groups))
	for _, group := range groups {
		out = append(out, tui.ProviderGroup{ID: group.ID, Label: group.Label})
	}
	return out
}

func (b *TUIBackend) ProviderModels(providerID string) []tui.ModelOption {
	var models []string
	var base string
	for _, group := range b.app.Config.ProviderGroups() {
		if group.ID == providerID {
			models = append([]string(nil), group.Models...)
			base = group.BaseURL
			break
		}
	}
	activeBase, activeModel := b.app.Config.ActiveEndpoint()
	out := make([]tui.ModelOption, 0, len(models))
	for _, model := range models {
		current := model == activeModel && (activeBase == base || (activeBase == "" && !strings.HasPrefix(providerID, "url:")))
		out = append(out, tui.ModelOption{ID: model, URL: base, Current: current})
	}
	return out
}

func (b *TUIBackend) UseProviderModel(chatID, providerID, model string) error {
	if err := b.app.UseProviderModel(providerID, model); err != nil {
		return err
	}
	return b.applyChatModel(chatID, model)
}

func (b *TUIBackend) AddProviderModel(chatID, providerID, model, promptPrice, completionPrice string) error {
	if err := b.app.AddProviderModel(providerID, model, promptPrice, completionPrice); err != nil {
		return err
	}
	return b.applyChatModel(chatID, model)
}

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

func (b *TUIBackend) applyChatModel(chatID, model string) error {
	sess, err := b.session(chatID)
	if err != nil {
		return nil
	}
	provider, err := b.app.Config.GetProviderForModel(model)
	if err != nil {
		return err
	}
	pCost, cCost := b.app.Config.GetPrices(model)
	sess.Agent.SetModel(model, provider, pCost, cCost)
	return nil
}

func (b *TUIBackend) Catalog(chatID string) []tui.TeamSnapshot {
	ws, err := b.workspace(chatID)
	if err != nil {
		return nil
	}
	snap := ws.Manager().CatalogSnapshot()
	var out []tui.TeamSnapshot
	for _, t := range snap.Teams {
		ts := tui.TeamSnapshot{ID: t.ID, Name: t.Name}
		for _, r := range t.Roles {
			ts.Roles = append(ts.Roles, tui.RoleSnapshot{ID: r.ID, Name: r.Name})
		}
		out = append(out, ts)
	}
	return out
}

func (b *TUIBackend) ReloadRoles(ctx context.Context, chatID string) error {
	ws, err := b.workspace(chatID)
	if err != nil {
		return err
	}
	return ws.Manager().ReloadRoles(ctx)
}

func (b *TUIBackend) SetWorkDir(dir string) (string, error) {
	return b.app.SetWorkDir(dir)
}

func (b *TUIBackend) TeamsText(chatID string) string {
	ws, err := b.workspace(chatID)
	if err != nil {
		return ""
	}
	return ws.Manager().FormatTeamsList()
}

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

func (b *TUIBackend) SwitchChat(chatID string) error {
	_, err := b.app.Sessions.SwitchSession(chatID)
	if err == nil {
		_ = session.SetActiveChat(b.app.ChatDir, chatID)
	}
	return err
}

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

func (b *TUIBackend) Remember(chatID, role, speaker, text string) {
	b.app.RememberTurn(chatID, role, speaker, text)
}

func (b *TUIBackend) SetModel(chatID, model string) error {
	sess, err := b.session(chatID)
	if err != nil {
		return err
	}
	provider, err := b.app.Config.GetProviderForModel(model)
	if err != nil {
		return err
	}
	pCost, cCost := b.app.Config.GetPrices(model)
	sess.Agent.SetModel(model, provider, pCost, cCost)
	if ws, err := b.workspace(chatID); err == nil {
		ws.SetModel(orchestrator.ModelSpec{
			Name: model, Provider: provider, PromptPrice: pCost, CompletionPrice: cCost,
		})
	}
	return nil
}

func (b *TUIBackend) CurrentModel(chatID string) string {
	sess, err := b.session(chatID)
	if err != nil {
		return b.app.Config.ActiveModel
	}
	return sess.Agent.GetModel()
}

func (b *TUIBackend) ContextFor(chatID, viewRoleID string) tui.ContextSnapshot {
	ws, err := b.workspace(chatID)
	if err != nil {
		return tui.ContextSnapshot{Breakdown: tui.ComputeBreakdown(nil, nil, 0)}
	}
	var msgs []llm.Message
	var toolDefs []llm.ToolDefinition
	if viewRoleID == "" {
		orch := ws.OrchestratorSession()
		msgs = orch.Context.Messages()
		toolDefs = b.chatToolDefs(ws, "")
	} else {
		for _, t := range ws.Manager().CatalogSnapshot().Teams {
			for _, r := range t.Roles {
				if r.ID == viewRoleID {
					if w, ok := ws.Manager().WorkerFor(t.ID, r.ID); ok {
						msgs = w.Session.Context.Messages()
						toolDefs = toolDefsForRole(b.app.RoleStore, b.app.BaseTools, r.ID)
					}
				}
			}
		}
	}
	cost := 0.0
	orch := ws.OrchestratorSession()
	if st := orch.Agent.BudgetStatus(); strings.HasPrefix(st, "[Budget]") {
		evs := tui.NewLineDecoder().Decode(tui.SourceRef{ThreadID: orch.Agent.ThreadID()}, st)
		for _, ev := range evs {
			if be, ok := ev.(tui.BudgetEvent); ok {
				cost += be.CostUSD
			}
		}
	}
	for _, team := range ws.Manager().CatalogSnapshot().Teams {
		for _, r := range team.Roles {
			if w, ok := ws.Manager().WorkerFor(team.ID, r.ID); ok {
				if st := w.Session.Agent.BudgetStatus(); strings.HasPrefix(st, "[Budget]") {
					evs := tui.NewLineDecoder().Decode(tui.SourceRef{ThreadID: w.Session.Agent.ThreadID()}, st)
					for _, ev := range evs {
						if be, ok := ev.(tui.BudgetEvent); ok {
							cost += be.CostUSD
						}
					}
				}
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

func (b *TUIBackend) ListAPIs() []tui.SavedAPI {
	apis := b.app.Config.SavedAPIs()
	seen := map[string]bool{}
	var out []tui.SavedAPI
	for _, api := range apis {
		if api.BaseURL == "" || seen[api.BaseURL] {
			continue
		}
		seen[api.BaseURL] = true
		id := api.ID
		current := false
		for _, other := range apis {
			if other.BaseURL == api.BaseURL && other.Current {
				id = other.ID
				current = true
				break
			}
		}
		label := api.Name
		if label == "" {
			label = api.BaseURL
		} else if api.BaseURL != "" {
			label += " · " + api.BaseURL
		}
		out = append(out, tui.SavedAPI{ID: id, Label: label, Current: current})
	}
	return out
}

func (b *TUIBackend) UseAPI(id string) error {
	if err := b.app.UseSavedAPI(id); err != nil {
		return err
	}
	return nil
}

func (b *TUIBackend) Connection() tui.ConnectionView {
	form := b.app.Config.ConnectionForm()
	return tui.ConnectionView{
		BaseURL:         form.BaseURL,
		Model:           form.Model,
		HasKey:          form.HasKey,
		HasPrice:        form.HasPrice,
		PromptPrice:     form.PromptPrice,
		CompletionPrice: form.CompletionPrice,
	}
}

func (b *TUIBackend) SaveConnection(chatID string, in tui.ConnectionInput) error {
	if err := b.app.ApplyConnection(in.Name, in.BaseURL, in.APIKey, in.Model, in.PromptPrice, in.CompletionPrice); err != nil {
		return err
	}
	return nil
}

func (b *TUIBackend) Dispatch(ctx context.Context, chatID, input, runID string) (tui.DispatchHandle, error) {
	sess, err := b.session(chatID)
	if err != nil {
		return tui.DispatchHandle{}, err
	}
	res, err := b.app.DispatchUserInput(ctx, sess, input, false)
	if err != nil {
		return tui.DispatchHandle{}, err
	}
	handle := tui.DispatchHandle{
		ChatID: chatID,
		RunID:  runID,
		Kind:   string(res.Kind),
		Reply:  res.Reply,
		TeamID: res.TeamID,
		RoleID: res.RoleID,
	}
	if res.Kind == orchestrator.KindDelegate {
		teamName, roleName := res.TeamID, res.RoleID
		for _, team := range b.Catalog(chatID) {
			if team.ID != res.TeamID {
				continue
			}
			if team.Name != "" {
				teamName = team.Name
			}
			for _, role := range team.Roles {
				if role.ID == res.RoleID && role.Name != "" {
					roleName = role.Name
				}
			}
		}
		handle.Trace = []string{fmt.Sprintf("delegated to %s · %s", teamName, roleName)}
	}
	if res.Kind == orchestrator.KindReply {
		handle.Trace = []string{"answered directly"}
	}
	if res.Done != nil {
		done := make(chan tui.DispatchDone, 1)
		go func() {
			r := <-res.Done
			done <- tui.DispatchDone{Reply: r.Reply, Err: r.Err}
		}()
		handle.Done = done
	}
	return handle, nil
}

func (b *TUIBackend) lookupWorker(chatID, teamID, roleID string) (*orchestrator.Worker, bool) {
	ws, err := b.workspace(chatID)
	if err != nil {
		return nil, false
	}
	return ws.Manager().WorkerFor(teamID, roleID)
}

func (b *TUIBackend) Subscribe(chatID string, send func(tui.LiveEvent)) context.CancelFunc {
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	if prev, ok := b.hookCancels[chatID]; ok {
		prev()
	}
	b.hookCancels[chatID] = cancel
	dec := tui.NewLineDecoder()
	b.decoders[chatID] = dec
	b.mu.Unlock()

	ws, err := b.workspace(chatID)
	if err != nil {
		return cancel
	}
	b.attachAgentHooks(ctx, chatID, ws.OrchestratorSession().Agent, send, dec, tui.SourceOrchestrator, "", "")
	for _, team := range ws.Manager().CatalogSnapshot().Teams {
		for _, r := range team.Roles {
			if w, ok := ws.Manager().WorkerFor(team.ID, r.ID); ok {
				b.attachAgentHooks(ctx, chatID, w.Session.Agent, send, dec, tui.SourceWorker, team.ID, r.ID)
				send(tui.WorkerStateEvent{
					SourceRef: tui.SourceRef{
						ChatID: chatID, ThreadID: w.Session.Agent.ThreadID(),
						TeamID: team.ID, RoleID: r.ID, Name: r.Name, Kind: tui.SourceWorker,
					},
					State: tui.WorkerIdle,
				})
			}
		}
	}
	return cancel
}

func (b *TUIBackend) attachAgentHooks(ctx context.Context, chatID string, ag *agent.Agent, send func(tui.LiveEvent), dec *tui.LineDecoder, kind tui.SourceKind, teamID, roleID string) {
	name := "orchestrator"
	if kind == tui.SourceWorker {
		if w, ok := b.lookupWorker(chatID, teamID, roleID); ok {
			name = w.Name
		}
	}
	src := tui.SourceRef{
		ChatID: chatID, ThreadID: ag.ThreadID(),
		TeamID: teamID, RoleID: roleID, Name: name, Kind: kind,
	}
	hook := func(text string) {
		for _, ev := range dec.Decode(src, text) {
			select {
			case <-ctx.Done():
				return
			default:
				send(ev)
			}
		}
		if kind == tui.SourceWorker && strings.Contains(text, "Thinking") {
			send(tui.WorkerStateEvent{SourceRef: src, State: tui.WorkerRunning})
		}
	}
	AttachLineHook(ag, hook)
}

func (b *TUIBackend) SessionID() string {
	if s, err := b.app.Sessions.GetActive(); err == nil {
		return s.ID
	}
	return ""
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
	// fallback: scan listed sessions
	for _, info := range b.app.Sessions.ListSession() {
		if info.ID == chatID {
			return b.app.Sessions.SwitchSession(chatID)
		}
	}
	return nil, fmt.Errorf("session %s not found", chatID)
}

func (b *TUIBackend) chatToolDefs(ws *orchestrator.Workspace, viewRoleID string) []llm.ToolDefinition {
	if viewRoleID != "" {
		return toolDefsForRole(b.app.RoleStore, b.app.BaseTools, viewRoleID)
	}
	seen := map[string]struct{}{}
	var defs []llm.ToolDefinition
	for _, team := range ws.Manager().CatalogSnapshot().Teams {
		for _, role := range team.Roles {
			for _, def := range toolDefsForRole(b.app.RoleStore, b.app.BaseTools, role.ID) {
				if _, ok := seen[def.Function.Name]; ok {
					continue
				}
				seen[def.Function.Name] = struct{}{}
				defs = append(defs, def)
			}
		}
	}
	return defs
}

func toolDefsForRole(store *roles.Store, base *tools.Registry, roleID string) []llm.ToolDefinition {
	if roleID == "" {
		return nil
	}
	role, ok := store.Role(roleID)
	if !ok {
		return nil
	}
	subset := tools.NewRegistrySubset(base, role.Tools)
	var defs []llm.ToolDefinition
	for _, t := range subset.List() {
		defs = append(defs, llm.ToolDefinition{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return defs
}
