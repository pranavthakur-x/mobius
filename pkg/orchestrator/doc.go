// Package orchestrator routes chat messages across isolated per-chat
// workspaces, coordinating direct commands, @role mentions, team leads, and
// LLM-based routing decisions.
//
// Key types and entry points are [Workspace], [NewWorkspace],
// [WorkspaceRegistry], [Manager], [Decision], [DispatchMode], and
// [DispatchResult].
//
// # Concurrency
//
// [WorkspaceRegistry], [Manager], and [Workspace] are safe for concurrent
// use. Per-role busy claims prevent overlapping executions on the same role
// worker, and asynchronous dispatches report completion on a buffered
// [WorkerResult] channel.
//
// # Boundaries
//
// Role workers are provisioned with least-privilege tool subsets via
// [tools.NewRegistrySubset]. Recent routing history included in
// [Workspace.Decide] is bounded and sanitized through internal/redact.
package orchestrator
