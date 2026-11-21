package tui

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

	StyleRoot     = lipgloss.NewStyle().Background(colorRoot).Foreground(colorText)
	StyleSidebar  = lipgloss.NewStyle().Background(colorSidebar).Foreground(colorText)
	StyleBorder   = lipgloss.NewStyle().Foreground(colorBorder)
	StyleMuted    = lipgloss.NewStyle().Foreground(colorMuted)
	StyleDim      = lipgloss.NewStyle().Foreground(colorDim)
	StyleTrace    = lipgloss.NewStyle().Foreground(colorDim).Italic(true)
	StylePurple   = lipgloss.NewStyle().Foreground(colorPurple)
	StyleBlue     = lipgloss.NewStyle().Foreground(colorBlue)
	StyleGreen    = lipgloss.NewStyle().Foreground(colorGreen)
	StyleOrange   = lipgloss.NewStyle().Foreground(colorOrange)
	StyleRed      = lipgloss.NewStyle().Foreground(colorRed)
	StyleCode     = lipgloss.NewStyle().Foreground(colorCode)
	StyleUser     = lipgloss.NewStyle().Background(colorUserBar).Foreground(colorText)
	StyleBold     = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	StyleItalic   = lipgloss.NewStyle().Foreground(colorText).Italic(true)
	StyleHeading1 = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	StyleHeading2 = lipgloss.NewStyle().Foreground(colorPurple).Bold(true)
	StyleHeading3 = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	StyleQuote    = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
)

func segmentStyle(i int) lipgloss.Style {
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
