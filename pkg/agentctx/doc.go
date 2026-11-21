// Package agentctx manages conversation history, system prompt assembly,
// heuristic token budgeting, and LLM-driven checkpoint compaction.
//
// Key entry points are [NewConversationContext], [NewCompactor],
// [BuildSystemPrompt], [BuildRoleSystemPrompt], and
// [EstimateConversationTokens].
//
// # Concurrency
//
// [ConversationContext] and [Compactor] are safe for concurrent use via
// internal RWMutex synchronization. [ConversationContext.Messages] and
// [ConversationContext.SetMessages] deep-copy tool-call slices so callers
// cannot observe or mutate internal state without holding the lock.
//
// # Boundaries
//
// Package agentctx depends only on pkg/llm and the standard library.
// Compaction uses a cut-point search that preserves the initial system prompt
// and avoids splitting assistant tool calls from their tool results.
package agentctx
