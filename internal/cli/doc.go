// Package cli (mobius/internal/cli) provides the command-line runtime wiring,
// interactive frontends, and one-shot execution runner for Mobius.
//
// # App Wiring
//
// [Boot] and [App] assemble persistent chat sessions, orchestrator
// workspaces, role catalogs, tool registries, LLM provider configuration,
// and event stores into a shared application container.
//
// # Interactive Frontends
//
// [StartInteractive] and [StartInteractiveWithTTY] launch the Bubble Tea
// fullscreen TUI ([mobius/internal/tui]) when stdout is a terminal and
// config/tui.toml loads cleanly, or fall back to the plain-text REPL
// ([StartREPL]) when MOBIUS_PLAIN=1 is set, output is non-interactive, or TUI
// settings fail to load.
//
// # TUI Backend Adapter
//
// [TUIBackend] adapts [App] to the [tui.Backend] interface in
// [mobius/internal/tui], exposing chat session CRUD, model and API endpoint
// management, role catalog inspection, asynchronous orchestration dispatch, and
// live event subscriptions. Subscriptions and line decoders are guarded by an
// internal mutex, while session and workspace mutations delegate to their
// respective thread-safe managers.
//
// # One-Shot Runner
//
// [RunGoal] and [RunGoalAsync] execute a single user instruction against a
// session synchronously or asynchronously and record visible transcript turns.
package cli
