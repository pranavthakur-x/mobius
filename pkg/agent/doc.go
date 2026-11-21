// Package agent implements the core ReAct runtime loop for Mobius.
//
// It coordinates multi-turn LLM generation, parallel tool execution, automatic
// context compaction, cost-budget enforcement, trajectory event persistence,
// and post-edit verification sensors.
//
// Key entry points are [NewAgent], [Agent.Run], and [LoadAgentConfig].
//
// # Concurrency
//
// [Agent] is safe for concurrent use. Mutable runtime fields (active model,
// provider, hooks, and sensor registry) are guarded by an internal RWMutex,
// while follow-up notes queued via [Agent.PushNote] are synchronized by a
// dedicated mutex. Within a single step, independent tool calls execute
// concurrently in worker goroutines and their results are committed to the
// conversation context and event store in original call order.
//
// # Boundaries
//
// Package agent delegates LLM transport to pkg/llm, tool execution to
// pkg/tools, oversized output offloading to internal/artifact, and spend tracking
// to internal/budget.
package agent
