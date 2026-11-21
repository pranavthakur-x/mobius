// Package tui implements Mobius's fullscreen Bubble Tea terminal user interface.
//
// # Architecture
//
// [Run] measures the terminal dimensions, constructs a [Model] backed by a
// [Backend] implementation, and starts a Bubble Tea program with alternate screen
// and mouse cell-motion support. [Model] owns the application state machine
// ([Model.Init], [Model.Update], [Model.View], [Model.Shutdown]) and delegates
// input handling and layout across focused files:
//   - model_keys.go: key dispatch, compose draft editing, submission, and footer menus.
//   - model_forms.go and connect.go: modal forms for WorkDir, Add Model, and Connect API.
//   - view.go and render.go: fixed-height split-pane layout ([FitScreen]), sidebar,
//     scrollable transcript viewport ([Viewport]), and compose/menu footer.
//
// # Event Channel & ChatBuffer Concurrency Model
//
// Backend subscriptions ([Backend.Subscribe]) and line hooks decode raw agent
// output through [LineDecoder] into typed [LiveEvent] values and deliver them via
// Model.enqueueEvent onto a bounded buffered channel (capacity 1024). Delivery
// selects on the model context so producers never block after [Model.Shutdown].
// [Model.Shutdown] cancels the context and active subscription without closing the
// event channel, preventing send-on-closed-channel panics while background hooks
// unwind.
//
// Inside [Model.Update], waitLiveEvent pulls the next [LiveEvent] and drains up
// to 64 already-buffered events in a single pass so fast streaming bursts coalesce
// before re-rendering. [ChatStore] and [ChatBuffer] protect per-session
// transcript blocks, team state, active-run tracking, and scroll/unread metadata
// with [sync.Mutex] for race-free access. [BudgetEvent] updates flow exclusively
// into [BudgetLedger] and the sidebar context breakdown, never into the
// transcript.
//
// # Live SSE Streaming Blocks
//
// [StreamDeltaEvent] chunks accumulate in a [BlockStreaming] [TranscriptBlock]
// keyed by role ID or speaker name, rendering either an inline cursor or a tool
// preparation indicator. When a step, tool call, error, or [FinalReplyEvent]
// arrives, [ChatBuffer.ResolveFinal] promotes the streaming block in-place to
// [BlockFinal] or replaces it cleanly.
//
// # Lipgloss Markdown Rendering & Theme Subpackages
//
// Styling and formatting are separated into two focused subpackages:
//   - [mobius/internal/tui/theme]: defines Mobius's Lipgloss color palette,
//     exported Style* definitions, and [theme.SegmentStyle].
//   - [mobius/internal/tui/markdown]: strips untrusted ANSI escape sequences
//     via [markdown.StripANSI], wraps plain text via [markdown.WrapRunes], and
//     renders Markdown blocks and inline spans via [markdown.Render] using
//     [mobius/internal/tui/theme].
package tui
