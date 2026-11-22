package tui

import (
	"time"

	"mobius/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	mascotWidth        = 15
	mascotHeight       = 3
	mascotTotalFrames  = 8
	mascotOrbitFrames  = 16 // 2 full figure-8 orbits per click
	mascotTickInterval = 75 * time.Millisecond
)

type mascotTickMsg struct{}

func mascotTickCmd() tea.Cmd {
	return tea.Tick(mascotTickInterval, func(time.Time) tea.Msg {
		return mascotTickMsg{}
	})
}

func (m *Model) isMascotClick(x, y int) bool {
	if m.height < 12 {
		return false
	}
	x0 := 0
	if m.settings.ShowContext {
		x0 = sidebarWidth
	}
	return y >= 0 && y < mascotHeight && x >= x0 && x < x0+mascotWidth
}

func (m *Model) triggerMascotAnim() tea.Cmd {
	alreadyRunning := m.mascotRemaining > 0
	m.mascotRemaining = mascotOrbitFrames
	if m.mascotFrame == 0 {
		m.mascotFrame = 1
	}
	if alreadyRunning {
		return nil
	}
	return mascotTickCmd()
}

// renderMascot renders the 3-line Mobius infinity pixel emblem.
// Frame 0 is the resting state; frames 1..8 trace a bright energy pulse + violet trail
// continuously around the figure-8 Mobius strip.
func renderMascot(frame int) [3]string {
	if frame <= 0 {
		return [3]string{
			theme.StyleMascot.Render("   ▄██▄  ▄██▄  "),
			theme.StyleMascot.Render("  ██  ████  ██ "),
			theme.StyleMascot.Render("   ▀██▀  ▀██▀  "),
		}
	}

	// Figure-8 path order across the 7 visual segments:
	// 0: top-left, 1: center-cross, 2: bottom-right, 3: right-edge,
	// 4: top-right, 5: center-cross, 6: bottom-left, 7: left-edge
	step := (frame - 1) % mascotTotalFrames
	trail := (step + mascotTotalFrames - 1) % mascotTotalFrames

	segStyle := func(indices ...int) func(...string) string {
		for _, idx := range indices {
			if idx == step {
				return theme.StyleBold.Render
			}
		}
		for _, idx := range indices {
			if idx == trail {
				return theme.StylePurple.Render
			}
		}
		return theme.StyleMascot.Render
	}

	tl := segStyle(0)("▄██▄")
	tr := segStyle(4)("▄██▄")
	le := segStyle(7)("██")
	cx := segStyle(1, 5)("████")
	re := segStyle(3)("██")
	bl := segStyle(6)("▀██▀")
	br := segStyle(2)("▀██▀")

	// Subtle vertical shimmer at the center twist on cross frames
	if step == 1 || step == 5 {
		cx = theme.StyleBold.Render("▓██▓")
	}

	return [3]string{
		"   " + tl + "  " + tr + "  ",
		"  " + le + "  " + cx + "  " + re + " ",
		"   " + bl + "  " + br + "  ",
	}
}
