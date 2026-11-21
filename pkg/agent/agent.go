package agent

import (
	"context"
	"fmt"
	"mobius/internal/artifact"
	"mobius/internal/budget"
	"mobius/internal/id"
	"mobius/pkg/agentctx"
	"mobius/pkg/events"
	"mobius/pkg/llm"
	"mobius/pkg/sensors"
	"mobius/pkg/tools"
	"strings"
	"sync"
	"time"
)

// LineHook receives formatted status lines for the REPL (default: fmt.Print).
type LineHook func(text string)

// StreamHook receives incremental LLM output deltas (tokens and tool-call starts).
type StreamHook func(ev llm.StreamEvent)

func defaultLineHook(text string) { fmt.Print(text) }

// Agent executes multi-turn ReAct conversations with tool execution, budget
// tracking, context compaction, event recording, and sensor verification.
type Agent struct {
	mu sync.RWMutex

	threadID       string
	provider       llm.Provider
	registry       *tools.Registry
	sensorRegistry *sensors.Registry
	model          string
	maxSteps       int
	maxCost        float64
	timeout        time.Duration
	toolDefs       []llm.ToolDefinition
	tracker        budget.CostTracker
	lineHook       LineHook
	streamHook     StreamHook
	events         events.EventStore // EventStore
	artifactStore  *artifact.Store
	compactor      *agentctx.Compactor

	notesMu sync.Mutex
	notes   []string
}

// NewAgent constructs an Agent configured from agent.toml with the provided LLM
// provider, tool registry, model name, token pricing, and optional event store.
func NewAgent(provider llm.Provider, registry *tools.Registry, model string, pricePrompt float64, priceComp float64, eventStore events.EventStore) (*Agent, error) {

	// 1. Validate required core components
	if provider == nil {
		return nil, fmt.Errorf("llm provider is required")
	}
	if registry == nil {
		return nil, fmt.Errorf("tools registry is required")
	}
	if model == "" {
		return nil, fmt.Errorf("model name cannot be empty")
	}

	agentCfg, err := LoadAgentConfig("config/agent.toml")
	if err != nil {
		agentCfg, err = LoadAgentConfig("pkg/agent/agent.toml")
		if err != nil {
			return nil, fmt.Errorf("failed to parse agent config : %w", err)
		}
	}

	// 2. Strict validation for agent budget limits
	if agentCfg.MaxSteps <= 0 {
		return nil, fmt.Errorf("max_steps must be greater than 0, got %d", agentCfg.MaxSteps)
	}
	if agentCfg.MaxCost <= 0 {
		return nil, fmt.Errorf("max_cost must be greater than 0, got %.2f", agentCfg.MaxCost)
	}
	if agentCfg.TimeoutSeconds <= 0 {
		return nil, fmt.Errorf("timeout_seconds must be greater than 0, got %d", agentCfg.TimeoutSeconds)
	}

	artStore, err := artifact.NewStore(".mobius/artifacts")
	if err != nil {
		return nil, fmt.Errorf("failed to init artifact store: %w", err)
	}

	// 3. Build tool definitions once
	var toolDefs []llm.ToolDefinition
	for _, t := range registry.List() {
		toolDefs = append(toolDefs, llm.ToolDefinition{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	// Pass the real prices instead of 0.15 and 0.60
	budgetTracker := budget.NewTracker(agentCfg.MaxCost, pricePrompt, priceComp)

	agent := &Agent{
		threadID:      id.NewThreadID(),
		provider:      provider,
		registry:      registry,
		model:         model,
		maxSteps:      agentCfg.MaxSteps,
		maxCost:       agentCfg.MaxCost,
		timeout:       time.Duration(agentCfg.TimeoutSeconds) * time.Second,
		toolDefs:      toolDefs, // Store once
		tracker:       budgetTracker,
		lineHook:      defaultLineHook,
		events:        eventStore,
		artifactStore: artStore,
		compactor:     agentctx.NewCompactor(agentctx.DefaultCompactorConfig(), provider, model),
	}
	return agent, nil
}

func (a *Agent) emitLine(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	a.mu.RLock()
	hook := a.lineHook
	if hook == nil {
		hook = defaultLineHook
	}
	a.mu.RUnlock()
	hook(text)
}

// SetLineHook sets the status-line sink; nil restores the default (fmt.Print).
func (a *Agent) SetLineHook(hook LineHook) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if hook == nil {
		a.lineHook = defaultLineHook
	} else {
		a.lineHook = hook
	}
}

// SetStreamHook sets the optional incremental token/tool stream callback.
// Passing nil disables live token streaming callbacks.
func (a *Agent) SetStreamHook(hook StreamHook) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.streamHook = hook
}

// Events returns the event store attached to this agent, or nil if unset.
func (a *Agent) Events() events.EventStore {
	return a.events
}

// ThreadID returns the unique thread identifier assigned to this agent.
func (a *Agent) ThreadID() string {
	return a.threadID
}

// Limits returns the step, cost, and time budget loaded from config/agent.toml.
func (a *Agent) Limits() (steps int, maxCostUSD float64, timeout time.Duration) {
	if a == nil {
		return 0, 0, 0
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.maxSteps, a.maxCost, a.timeout
}

// RecordUsage adds one provider response to this agent's running spend.
func (a *Agent) RecordUsage(u llm.Usage) {
	if a == nil {
		return
	}
	a.mu.RLock()
	tracker := a.tracker
	a.mu.RUnlock()
	if tracker == nil {
		return
	}
	tracker.Add(u)
}

// BudgetStatus returns the current budget summary string.
func (a *Agent) BudgetStatus() string {
	a.mu.RLock()
	tracker := a.tracker
	a.mu.RUnlock()
	if tracker == nil {
		return ""
	}
	return tracker.Status()
}

// GenerateTitle asks the LLM for a concise 3-4 word title for the session
func (a *Agent) GenerateTitle(ctx context.Context, prompt string) string {
	a.mu.RLock()
	model := a.model
	provider := a.provider
	a.mu.RUnlock()

	req := &llm.ChatRequest{
		Model: model,
		Message: []llm.Message{
			{
				Role:    llm.RoleUser,
				Content: agentctx.TitlePrompt(prompt),
			},
		},
	}

	resp, err := provider.Generate(ctx, req)
	if err != nil || strings.TrimSpace(resp.Content) == "" {
		// Fallback: take first 4 words of the prompt
		words := strings.Fields(prompt)
		if len(words) > 4 {
			words = words[:4]
		}
		return strings.Join(words, " ")
	}

	return strings.TrimSpace(resp.Content)
}

// GetModel returns the name of the active LLM model.
func (a *Agent) GetModel() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.model
}

// SetModel updates the active model, provider, token pricing, and compactor target.
func (a *Agent) SetModel(model string, provider llm.Provider, pricePrompt, priceComp float64) {
	a.mu.Lock()
	a.model = model
	a.provider = provider
	tracker := a.tracker
	compactor := a.compactor
	a.mu.Unlock()

	if tracker != nil {
		tracker.UpdatePrices(pricePrompt, priceComp)
	}
	if compactor != nil {
		compactor.UpdateModel(model, provider)
	}
}

// SetSensors attaches a verification sensor registry used after file edits.
func (a *Agent) SetSensors(sr *sensors.Registry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sensorRegistry = sr
}

// Sensors returns the attached verification sensor registry, or nil if unset.
func (a *Agent) Sensors() *sensors.Registry {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sensorRegistry
}

// PushNote queues a follow-up user message for the next safe drain point in Run.
// Whitespace-only notes are rejected. The note does not touch the conversation until drained.
func (a *Agent) PushNote(note string) bool {
	if strings.TrimSpace(note) == "" {
		return false
	}
	a.notesMu.Lock()
	defer a.notesMu.Unlock()
	a.notes = append(a.notes, note)
	return true
}

func (a *Agent) drainNotes() []string {
	a.notesMu.Lock()
	defer a.notesMu.Unlock()
	out := a.notes
	a.notes = nil
	return out
}

// HasPendingNotes reports whether PushNote has queued messages not yet drained by Run.
func (a *Agent) HasPendingNotes() bool {
	a.notesMu.Lock()
	defer a.notesMu.Unlock()
	return len(a.notes) > 0
}
