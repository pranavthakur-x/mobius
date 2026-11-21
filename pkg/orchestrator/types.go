package orchestrator

import (
	"mobius/pkg/agent"
	"mobius/pkg/llm"
	"mobius/pkg/session"
	"mobius/pkg/tools"
)

// ModelSpec carries the active model and per-token prices.
type ModelSpec struct {
	Name            string
	Provider        llm.Provider
	PromptPrice     float64
	CompletionPrice float64
}

// AgentFactory builds an agent for a tool registry and model spec.
type AgentFactory func(registry *tools.Registry, model ModelSpec) (*agent.Agent, error)

// Decision is a parsed orchestrator routing payload.
type Decision struct {
	Think  string
	Action string
	Reply  string
	TeamID string
	RoleID string
	Task   string
}

// DispatchMode controls whether Handle waits for workers.
type DispatchMode int

const (
	DispatchAsync DispatchMode = iota
	DispatchSync
)

// DispatchKind classifies a Handle result.
type DispatchKind string

const (
	KindReply    DispatchKind = "reply"
	KindDelegate DispatchKind = "delegate"
	KindCommand  DispatchKind = "command"
)

// WorkerResult is the outcome of a background worker run.
type WorkerResult struct {
	Reply string
	Err   error
}

// DispatchResult is returned from Workspace.Handle.
type DispatchResult struct {
	Kind   DispatchKind
	Reply  string
	TeamID string
	RoleID string
	Done   <-chan WorkerResult
}

// Worker is one role agent bound to a team.
type Worker struct {
	RoleID      string
	TeamID      string
	Name        string
	Session     *session.Session
	description string
	roleSystem  string
	tools       []string
}

// Team groups workers for one department instance in a chat.
type Team struct {
	ID          string
	Name        string
	DefaultLead string
	workers     []*Worker
	workerIndex map[string]*Worker
	directives  []string
}

// claimKey identifies a busy worker slot.
type claimKey struct {
	teamID string
	roleID string
}

// recentTurn is one bounded, redacted routing-history entry passed to Decide
// so follow-up user messages resolve cleanly without replaying full transcripts.
type recentTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CatalogRole is one role entry in the routing catalog JSON.
type CatalogRole struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Busy        bool     `json:"busy"`
}

// CatalogTeam is one team entry in the routing catalog JSON.
type CatalogTeam struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	DefaultLead string        `json:"default_lead"`
	Roles       []CatalogRole `json:"roles"`
}

// CatalogSnapshot is the JSON catalog passed to the orchestrator model.
type CatalogSnapshot struct {
	Teams []CatalogTeam `json:"teams"`
}
