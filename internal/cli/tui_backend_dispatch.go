package cli

import (
	"context"
	"fmt"
	"strings"

	"mobius/internal/tui"
	"mobius/pkg/agent"
	"mobius/pkg/llm"
	"mobius/pkg/orchestrator"
	"mobius/pkg/roles"
	"mobius/pkg/tools"
)

// Catalog returns snapshots of the teams and roles available in chatID's workspace.
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

// ReloadRoles reloads role definitions for chatID's workspace.
func (b *TUIBackend) ReloadRoles(ctx context.Context, chatID string) error {
	ws, err := b.workspace(chatID)
	if err != nil {
		return err
	}
	return ws.Manager().ReloadRoles(ctx)
}

// WorkDir returns the active workspace directory path.
func (b *TUIBackend) WorkDir() string {
	if b == nil || b.app == nil {
		return "."
	}
	return b.app.WorkDir
}

// SetWorkDir updates the working directory used by tools and sensors.
func (b *TUIBackend) SetWorkDir(dir string) (string, error) {
	return b.app.SetWorkDir(dir)
}

// TeamsText returns a formatted listing of teams and roles for chatID.
func (b *TUIBackend) TeamsText(chatID string) string {
	ws, err := b.workspace(chatID)
	if err != nil {
		return ""
	}
	return ws.Manager().FormatTeamsList()
}

// Dispatch routes user input through chatID's orchestrator and returns a handle
// that surfaces immediate trace lines and asynchronous worker completion.
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

// Subscribe attaches line and stream hooks to the orchestrator and worker
// agents for chatID, forwarding decoded events to send until the returned
// cancel function is invoked or Subscribe is called again for chatID.
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
	if ag == nil {
		return
	}
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
	if kind == tui.SourceWorker {
		AttachStreamHook(ag, func(ev llm.StreamEvent) {
			select {
			case <-ctx.Done():
				return
			default:
				send(tui.StreamDeltaEvent{
					SourceRef: src,
					TextDelta: ev.TextDelta,
					ToolName:  ev.ToolName,
					ToolID:    ev.ToolID,
				})
			}
		})
	}
}

func (b *TUIBackend) lookupWorker(chatID, teamID, roleID string) (*orchestrator.Worker, bool) {
	ws, err := b.workspace(chatID)
	if err != nil {
		return nil, false
	}
	return ws.Manager().WorkerFor(teamID, roleID)
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
	if roleID == "" || store == nil {
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
