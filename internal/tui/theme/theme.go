package theme

import "github.com/charmbracelet/lipgloss"

var (
	colorRoot    = lipgloss.Color("#14141C")
	colorSidebar = lipgloss.Color("#1A1A24")
	colorBorder  = lipgloss.Color("#2E2E3A")
	colorText    = lipgloss.Color("#E8E8ED")
	colorMuted   = lipgloss.Color("#6B6B7A")
	colorDim     = lipgloss.Color("#3D3D4A")
	colorPurple  = lipgloss.Color("#9B6FE8")
	colorBlue    = lipgloss.Color("#5BA4E6")
	colorGreen   = lipgloss.Color("#6BCB77")
	colorOrange  = lipgloss.Color("#E8A35C")
	colorRed     = lipgloss.Color("#E85C5C")
	colorCode    = lipgloss.Color("#E6D3A3")
	colorUserBar = lipgloss.Color("#2C2C34")
)

// TUI Lipgloss styles for layout panes, typography, and semantic indicators.
var (
	// StyleRoot styles the main chat panel background and default foreground.
	StyleRoot = lipgloss.NewStyle().Background(colorRoot).Foreground(colorText)
	// StyleSidebar styles the context and teams sidebar panel.
	StyleSidebar = lipgloss.NewStyle().Background(colorSidebar).Foreground(colorText)
	// StyleBorder styles pane dividers, rules, and code/blockquote borders.
	StyleBorder = lipgloss.NewStyle().Foreground(colorBorder)
	// StyleMuted styles secondary labels, hints, and inactive items.
	StyleMuted = lipgloss.NewStyle().Foreground(colorMuted)
	// StyleDim styles dimmed track segments and scrollbar rails.
	StyleDim = lipgloss.NewStyle().Foreground(colorDim)
	// StyleTrace styles italicized agent step and tool activity lines.
	StyleTrace = lipgloss.NewStyle().Foreground(colorDim).Italic(true)
	// StylePurple styles accent highlights, department headers, and active thumbs.
	StylePurple = lipgloss.NewStyle().Foreground(colorPurple)
	// StyleBlue styles primary badges, list bullets, and system indicators.
	StyleBlue = lipgloss.NewStyle().Foreground(colorBlue)
	// StyleGreen styles active selections, success states, and cost figures.
	StyleGreen = lipgloss.NewStyle().Foreground(colorGreen)
	// StyleOrange styles warning badges and conversation usage segments.
	StyleOrange = lipgloss.NewStyle().Foreground(colorOrange)
	// StyleRed styles error banners and critical badges.
	StyleRed = lipgloss.NewStyle().Foreground(colorRed)
	// StyleCode styles inline code spans and fenced code block contents.
	StyleCode = lipgloss.NewStyle().Foreground(colorCode)
	// StyleUser styles the user prompt row and compose input bar.
	StyleUser = lipgloss.NewStyle().Background(colorUserBar).Foreground(colorText)
	// StyleBold styles bold inline Markdown text and table headers.
	StyleBold = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	// StyleItalic styles italicized inline Markdown text.
	StyleItalic = lipgloss.NewStyle().Foreground(colorText).Italic(true)
	// StyleHeading1 styles level-1 Markdown headings.
	StyleHeading1 = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	// StyleHeading2 styles level-2 Markdown headings.
	StyleHeading2 = lipgloss.NewStyle().Foreground(colorPurple).Bold(true)
	// StyleHeading3 styles level-3 and deeper Markdown headings.
	StyleHeading3 = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	// StyleQuote styles Markdown blockquote text.
	StyleQuote = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
)

// SegmentStyle returns the Lipgloss style for context usage bar segment index i.
func SegmentStyle(i int) lipgloss.Style {
	switch i {
	case 0:
		return StyleBlue
	case 1:
		return StyleGreen
	case 2:
		return StylePurple
	case 3:
		return StyleOrange
	default:
		return StyleDim
	}
}
