package tui

// SourceKind identifies orchestrator vs worker emitters.
type SourceKind int

const (
	SourceOrchestrator SourceKind = iota
	SourceWorker
)

// SourceRef tags live events with routing identity.
type SourceRef struct {
	ChatID   string
	ThreadID string
	TeamID   string
	RoleID   string
	Name     string
	Kind     SourceKind
}

// WorkerState is the sidebar team status for one role.
type WorkerState int

const (
	WorkerIdle WorkerState = iota
	WorkerRunning
	WorkerDone
	WorkerError
)

func (s WorkerState) String() string {
	switch s {
	case WorkerRunning:
		return "running"
	case WorkerDone:
		return "done"
	case WorkerError:
		return "error"
	default:
		return "idle"
	}
}

// LiveEvent is implemented by all channel payloads from line hooks and dispatch.
type LiveEvent interface {
	liveEvent()
	Source() SourceRef
	ChatID() string
}

type AgentStepEvent struct {
	SourceRef SourceRef
	Text      string
}

func (e AgentStepEvent) liveEvent()        {}
func (e AgentStepEvent) Source() SourceRef { return e.SourceRef }
func (e AgentStepEvent) ChatID() string    { return e.SourceRef.ChatID }

type AgentThoughtEvent struct {
	SourceRef SourceRef
	Text      string
	RunID     string
}

func (e AgentThoughtEvent) liveEvent()        {}
func (e AgentThoughtEvent) Source() SourceRef { return e.SourceRef }
func (e AgentThoughtEvent) ChatID() string    { return e.SourceRef.ChatID }

type AgentToolEvent struct {
	SourceRef SourceRef
	Name      string
	Args      string
	RunID     string
}

func (e AgentToolEvent) liveEvent()        {}
func (e AgentToolEvent) Source() SourceRef { return e.SourceRef }
func (e AgentToolEvent) ChatID() string    { return e.SourceRef.ChatID }

type AgentSensorEvent struct {
	SourceRef SourceRef
	Text      string
}

func (e AgentSensorEvent) liveEvent()        {}
func (e AgentSensorEvent) Source() SourceRef { return e.SourceRef }
func (e AgentSensorEvent) ChatID() string    { return e.SourceRef.ChatID }

type BudgetEvent struct {
	SourceRef SourceRef
	InTokens  int
	OutTokens int
	CostUSD   float64
	CostINR   float64
	Raw       string
}

func (e BudgetEvent) liveEvent()        {}
func (e BudgetEvent) Source() SourceRef { return e.SourceRef }
func (e BudgetEvent) ChatID() string    { return e.SourceRef.ChatID }

type RouteTraceEvent struct {
	SourceRef SourceRef
	Text      string
}

func (e RouteTraceEvent) liveEvent()        {}
func (e RouteTraceEvent) Source() SourceRef { return e.SourceRef }
func (e RouteTraceEvent) ChatID() string    { return e.SourceRef.ChatID }

type WorkerStateEvent struct {
	SourceRef SourceRef
	State     WorkerState
}

func (e WorkerStateEvent) liveEvent()        {}
func (e WorkerStateEvent) Source() SourceRef { return e.SourceRef }
func (e WorkerStateEvent) ChatID() string    { return e.SourceRef.ChatID }

type FinalReplyEvent struct {
	SourceRef SourceRef
	Text      string
	RunID     string
	Err       error
}

func (e FinalReplyEvent) liveEvent()        {}
func (e FinalReplyEvent) Source() SourceRef { return e.SourceRef }
func (e FinalReplyEvent) ChatID() string    { return e.SourceRef.ChatID }

type RunErrorEvent struct {
	SourceRef SourceRef
	Text      string
	Key       string
}

func (e RunErrorEvent) liveEvent()        {}
func (e RunErrorEvent) Source() SourceRef { return e.SourceRef }
func (e RunErrorEvent) ChatID() string    { return e.SourceRef.ChatID }

type UserMessageEvent struct {
	SourceRef SourceRef
	Text      string
	RunID     string
}

func (e UserMessageEvent) liveEvent()        {}
func (e UserMessageEvent) Source() SourceRef { return e.SourceRef }
func (e UserMessageEvent) ChatID() string    { return e.SourceRef.ChatID }

// StreamDeltaEvent carries an incremental text token or tool-call start from an SSE stream.
type StreamDeltaEvent struct {
	SourceRef SourceRef
	TextDelta string
	ToolName  string
	ToolID    string
	RunID     string
}

func (e StreamDeltaEvent) liveEvent()        {}
func (e StreamDeltaEvent) Source() SourceRef { return e.SourceRef }
func (e StreamDeltaEvent) ChatID() string    { return e.SourceRef.ChatID }
