// Package budget tracks cumulative LLM token usage and enforces per-agent
// USD spending limits.
//
// Key types and entry points are [CostTracker], [Tracker], [NewTracker], and
// [USDToINRRate].
//
// # Concurrency
//
// [Tracker] is safe for concurrent use; all token counters, per-million token
// rates, and cost checks are protected by an internal RWMutex.
//
// # Boundaries
//
// Package budget depends only on pkg/llm for [llm.Usage] and performs pure
// in-memory accounting with no disk or network I/O.
package budget
