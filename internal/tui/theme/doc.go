// Package theme defines the Mobius TUI color palette and Lipgloss styles.
//
// # Palette and Styles
//
// Package-level Style* variables provide preconfigured [lipgloss.Style] values
// for panes, borders, typography, status indicators, and Markdown rendering.
// [SegmentStyle] maps context breakdown bar segment indices to their
// corresponding palette styles.
//
// # Concurrency
//
// All exported style variables and functions in this package are immutable or
// stateless after package initialization and are safe for concurrent use across
// goroutines.
package theme
