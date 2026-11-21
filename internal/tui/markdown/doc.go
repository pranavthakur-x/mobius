// Package markdown implements a zero-dependency Lipgloss Markdown renderer,
// ANSI escape sequence sanitizer, and rune-safe line wrapper for Mobius TUI
// transcripts.
//
// # Rendering Pipeline
//
// [Render] strips untrusted ANSI CSI/OSC sequences via [StripANSI], parses block
// structures (fenced code blocks, ATX headings, horizontal rules, pipe tables,
// blockquotes, and nested ordered/unordered lists) alongside inline spans
// (code, bold, italic, links, priority badges, and review verdicts), and
// word-wraps styled tokens to the target pane width using [mobius/internal/tui/theme].
//
// [WrapRunes] and [VisibleWidth] provide rune-aware line wrapping and display
// width measurement for plain text.
//
// # Concurrency
//
// All exported functions in this package are pure and stateless and are safe for
// concurrent use across goroutines.
package markdown
