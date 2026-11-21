package cli

import (
	"sort"
	"strings"

	"mobius/internal/tui"
	"mobius/pkg/llm"
	"mobius/pkg/orchestrator"
)

// ListModels returns all configured models sorted by ID with the active model marked.
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

// ListProviders returns the configured provider groups available for selection.
func (b *TUIBackend) ListProviders() []tui.ProviderGroup {
	groups := b.app.Config.ProviderGroups()
	out := make([]tui.ProviderGroup, 0, len(groups))
	for _, group := range groups {
		out = append(out, tui.ProviderGroup{ID: group.ID, Label: group.Label})
	}
	return out
}

// ProviderModels returns the models configured for providerID with the active
// endpoint marked as current.
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

// UseProviderModel activates model on providerID and applies it to chatID.
func (b *TUIBackend) UseProviderModel(chatID, providerID, model string) error {
	if err := b.app.UseProviderModel(providerID, model); err != nil {
		return err
	}
	return b.applyChatModel(chatID, model)
}

// AddProviderModel registers model under providerID with optional pricing and
// applies it to chatID.
func (b *TUIBackend) AddProviderModel(chatID, providerID, model, promptPrice, completionPrice string) error {
	if err := b.app.AddProviderModel(providerID, model, promptPrice, completionPrice); err != nil {
		return err
	}
	return b.applyChatModel(chatID, model)
}

// SetModel updates the model and pricing on chatID's session agent and workspace.
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

// CurrentModel returns the active model ID for chatID, or the globally
// configured active model when the session cannot be resolved.
func (b *TUIBackend) CurrentModel(chatID string) string {
	sess, err := b.session(chatID)
	if err != nil {
		return b.app.Config.ActiveModel
	}
	return sess.Agent.GetModel()
}

// Connection returns the current endpoint connection settings for display or editing.
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

// ListAPIs returns deduplicated saved API endpoints with the active endpoint marked.
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

// UseAPI activates the saved API endpoint identified by id.
func (b *TUIBackend) UseAPI(id string) error {
	return b.app.UseSavedAPI(id)
}

// SaveConnection validates and persists connection settings and updates active workspaces.
func (b *TUIBackend) SaveConnection(chatID string, in tui.ConnectionInput) error {
	return b.app.ApplyConnection(in.Name, in.BaseURL, in.APIKey, in.Model, in.PromptPrice, in.CompletionPrice)
}
