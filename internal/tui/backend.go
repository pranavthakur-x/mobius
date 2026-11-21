package tui

import (
	"context"
)

// ChatSnapshot is one selectable chat session.
type ChatSnapshot struct {
	ID   string
	Name string
}

// TeamSnapshot is a department with ordered roles for mention/view menus.
type TeamSnapshot struct {
	ID    string
	Name  string
	Roles []RoleSnapshot
}

// RoleSnapshot is one mentionable worker role.
type RoleSnapshot struct {
	ID   string
	Name string
}

// ModelOption is a selectable LLM id.
// ProviderGroup is one API the user can open in the model menu.
type ProviderGroup struct {
	ID    string
	Label string
}

type ModelOption struct {
	ID      string
	URL     string
	Current bool
}

// ConnectionInput is the connect form. Blank price strings mean no price was given.
type ConnectionInput struct {
	Name            string
	BaseURL         string
	APIKey          string
	Model           string
	PromptPrice     string
	CompletionPrice string
}

// SavedAPI is one stored endpoint the user can activate.
type SavedAPI struct {
	ID      string
	Label   string
	Current bool
}

// HistoryTurn is one saved chat line.
type HistoryTurn struct {
	Role    string
	Speaker string
	Text    string
}

// ConnectionView is the connect form without the API key.
type ConnectionView struct {
	BaseURL         string
	Model           string
	HasKey          bool
	HasPrice        bool
	PromptPrice     string
	CompletionPrice string
}

// ContextSnapshot is sidebar state from the backend.
type ContextSnapshot struct {
	Breakdown ContextBreakdown
	Ledger    BudgetSnapshot
}

// DispatchHandle tracks an in-flight user dispatch.
type DispatchHandle struct {
	ChatID string
	RunID  string
	Kind   string
	Reply  string
	TeamID string
	RoleID string
	Done   <-chan DispatchDone
	Trace  []string
}

// DispatchDone is the worker completion payload.
type DispatchDone struct {
	Reply string
	Err   error
}

// Backend is implemented by internal/cli without importing it from tui tests.
type Backend interface {
	ActiveChat() (ChatSnapshot, error)
	ListChats() []ChatSnapshot
	ListModels() ([]ModelOption, error)
	ListProviders() []ProviderGroup
	ProviderModels(providerID string) []ModelOption
	UseProviderModel(chatID, providerID, model string) error
	AddProviderModel(chatID, providerID, model, promptPrice, completionPrice string) error
	DeleteChat(ctx context.Context, chatID string) (ChatSnapshot, error)
	Catalog(chatID string) []TeamSnapshot
	ReloadRoles(ctx context.Context, chatID string) error
	SetWorkDir(dir string) (string, error)
	TeamsText(chatID string) string
	NewChat(ctx context.Context) (ChatSnapshot, error)
	SwitchChat(chatID string) error
	SetModel(chatID, model string) error
	Connection() ConnectionView
	ListAPIs() []SavedAPI
	UseAPI(id string) error
	SaveConnection(chatID string, in ConnectionInput) error
	CurrentModel(chatID string) string
	ContextFor(chatID, viewRoleID string) ContextSnapshot
	History(chatID string) []HistoryTurn
	Remember(chatID, role, speaker, text string)
	Dispatch(ctx context.Context, chatID, input, runID string) (DispatchHandle, error)
	Subscribe(chatID string, send func(LiveEvent)) context.CancelFunc
	SessionID() string
}
