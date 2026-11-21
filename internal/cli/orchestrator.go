package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mobius/pkg/agent"
	"mobius/pkg/events"
	"mobius/pkg/llm"
	"mobius/pkg/orchestrator"
	"mobius/pkg/roles"
	"mobius/pkg/sensors"
	"mobius/pkg/session"
	"mobius/pkg/tools"
)

// App bundles shared CLI wiring for workspaces and sessions.
type App struct {
	Sessions   *session.Manager
	Workspaces *orchestrator.WorkspaceRegistry
	RoleStore  *roles.Store
	BaseTools  *tools.Registry
	Config     *llm.Config
	Events     events.EventStore
	ChatDir    string
	WorkDir    string
}

// NewApp creates session/workspace wiring for interactive and one-shot CLI.
func NewApp(sm *session.Manager, ws *orchestrator.WorkspaceRegistry, store *roles.Store, base *tools.Registry, cfg *llm.Config, ev events.EventStore) *App {
	return &App{
		Sessions:   sm,
		Workspaces: ws,
		RoleStore:  store,
		BaseTools:  base,
		Config:     cfg,
		Events:     ev,
		ChatDir:    ".mobius/chats",
		WorkDir:    ".",
	}
}

// SetWorkDir points file and shell tools at a directory the user chose.
func (a *App) SetWorkDir(dir string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app is required")
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, dir[2:])
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	a.WorkDir = abs
	if a.BaseTools != nil {
		a.BaseTools.SetWorkDir(abs)
	}
	if a.Sessions != nil {
		for _, item := range a.Sessions.ListSession() {
			sess, err := a.Sessions.Get(item.ID)
			if err != nil || sess.Agent == nil {
				continue
			}
			a.attachSensors(sess.Agent)
		}
	}
	return abs, nil
}

func (a *App) attachSensors(ag *agent.Agent) {
	if ag == nil || a == nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(a.WorkDir, "AGENTS.md"))
	if err != nil {
		ag.SetSensors(sensors.NewRegistry())
		return
	}
	ag.SetSensors(sensors.NewRegistryFromGuide(a.WorkDir, string(data)))
}

// AgentFactory builds agents using the shared event store.
func (a *App) AgentFactory() orchestrator.AgentFactory {
	return func(registry *tools.Registry, model orchestrator.ModelSpec) (*agent.Agent, error) {
		return agent.NewAgent(model.Provider, registry, model.Name, model.PromptPrice, model.CompletionPrice, a.Events)
	}
}

// ActiveModelSpec reads prices for the active configured model.
func (a *App) ActiveModelSpec(provider llm.Provider) orchestrator.ModelSpec {
	pCost, cCost := a.Config.GetPrices(a.Config.ActiveModel)
	return orchestrator.ModelSpec{
		Name:            a.Config.ActiveModel,
		Provider:        provider,
		PromptPrice:     pCost,
		CompletionPrice: cCost,
	}
}

// ApplyConnection saves an API URL, key, and model and points every chat at it.
// A blank API key keeps the key already saved. Blank prices hide the cost.
func (a *App) ApplyConnection(name, baseURL, apiKey, model, promptRaw, completionRaw string) error {
	if a == nil || a.Config == nil {
		return fmt.Errorf("config is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(apiKey) == "" {
		apiKey = a.Config.KeyFor(llm.ConnectionID(baseURL, model))
	}
	conn, err := llm.ParseConnection(baseURL, apiKey, model, promptRaw, completionRaw)
	if err != nil {
		return err
	}
	conn.Name = name
	if err := a.Config.UseConnection(conn); err != nil {
		return err
	}
	if err := llm.SaveConnection("", conn); err != nil {
		return err
	}
	provider, err := a.Config.GetProviderForModel(conn.Model)
	if err != nil {
		return err
	}
	pCost, cCost := a.Config.GetPrices(conn.Model)
	spec := orchestrator.ModelSpec{
		Name:            conn.Model,
		Provider:        provider,
		PromptPrice:     pCost,
		CompletionPrice: cCost,
	}
	a.applyModel(spec)
	return nil
}

// UseSavedAPI activates one stored endpoint without removing the others.
func (a *App) UseSavedAPI(id string) error {
	if a == nil || a.Config == nil {
		return fmt.Errorf("config is required")
	}
	var chosen *llm.Connection
	for _, api := range a.Config.SavedAPIs() {
		if api.ID != id {
			continue
		}
		key := a.Config.KeyFor(id)
		conn, err := llm.ParseConnection(api.BaseURL, key, api.Model, api.PromptPrice, api.CompletionPrice)
		if err != nil {
			return err
		}
		if !api.HasPrice {
			conn.HasPrice = false
			conn.PromptPrice = 0
			conn.CompletionPrice = 0
		}
		conn.Name = api.Name
		chosen = &conn
		break
	}
	if chosen == nil {
		return fmt.Errorf("unknown API")
	}
	if err := a.Config.UseConnection(*chosen); err != nil {
		return err
	}
	if err := llm.SaveConnection("", *chosen); err != nil {
		return err
	}
	provider, err := a.Config.GetProviderForModel(chosen.Model)
	if err != nil {
		return err
	}
	pCost, cCost := a.Config.GetPrices(chosen.Model)
	a.applyModel(orchestrator.ModelSpec{
		Name: chosen.Model, Provider: provider, PromptPrice: pCost, CompletionPrice: cCost,
	})
	return nil
}

// UseProviderModel makes one model on one API the active endpoint.
func (a *App) UseProviderModel(groupID, model string) error {
	if strings.HasPrefix(groupID, "url:") {
		base := strings.TrimPrefix(groupID, "url:")
		for _, api := range a.Config.SavedAPIs() {
			if api.BaseURL == base && api.Model == model {
				return a.UseSavedAPI(api.ID)
			}
		}
		return fmt.Errorf("model %q is not on %s", model, base)
	}
	provider, pCost, cCost, err := a.Config.OpenGroupModel(groupID, model)
	if err != nil {
		return err
	}
	a.Config.NoteActive(model)
	a.applyModel(orchestrator.ModelSpec{
		Name: model, Provider: provider, PromptPrice: pCost, CompletionPrice: cCost,
	})
	return nil
}

// AddProviderModel stores a model on an API and starts using it.
func (a *App) AddProviderModel(groupID, model, promptRaw, completionRaw string) error {
	if strings.HasPrefix(groupID, "url:") {
		conn, err := a.Config.AddSavedModel(strings.TrimPrefix(groupID, "url:"), model, promptRaw, completionRaw)
		if err != nil {
			return err
		}
		if err := a.Config.UseConnection(conn); err != nil {
			return err
		}
		if err := llm.SaveConnection("", conn); err != nil {
			return err
		}
		provider, err := a.Config.GetProviderForModel(model)
		if err != nil {
			return err
		}
		pCost, cCost := a.Config.GetPrices(model)
		a.applyModel(orchestrator.ModelSpec{
			Name: model, Provider: provider, PromptPrice: pCost, CompletionPrice: cCost,
		})
		return nil
	}
	if err := a.Config.AddConfigModel(groupID, model, ""); err != nil {
		return err
	}
	return a.UseProviderModel(groupID, model)
}

func (a *App) applyModel(spec orchestrator.ModelSpec) {
	if a.Workspaces == nil {
		return
	}
	a.Workspaces.Each(func(w *orchestrator.Workspace) {
		w.SetModel(spec)
	})
}

// WorkspaceForSession returns the workspace for a chat session id.
func (a *App) WorkspaceForSession(sess *session.Session) (*orchestrator.Workspace, error) {
	ws, ok := a.Workspaces.Get(sess.ID)
	if !ok {
		return nil, fmt.Errorf("no workspace for session %s", sess.ID)
	}
	return ws, nil
}

// AttachLineHook wires worker/orchestrator output to the session line hook pattern.
func AttachLineHook(ag *agent.Agent, hook agent.LineHook) {
	if hook != nil {
		ag.SetLineHook(hook)
	}
}

// AttachStreamHook wires incremental SSE token/tool stream events from an agent.
func AttachStreamHook(ag *agent.Agent, hook agent.StreamHook) {
	if ag != nil {
		ag.SetStreamHook(hook)
	}
}

// DispatchUserInput routes one prompt through the workspace orchestrator.
func (a *App) DispatchUserInput(ctx context.Context, sess *session.Session, input string, sync bool) (orchestrator.DispatchResult, error) {
	ws, err := a.WorkspaceForSession(sess)
	if err != nil {
		return orchestrator.DispatchResult{}, err
	}
	mode := orchestrator.DispatchAsync
	if sync {
		mode = orchestrator.DispatchSync
	}
	return ws.Handle(ctx, input, mode)
}
