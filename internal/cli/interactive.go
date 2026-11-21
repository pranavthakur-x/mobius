package cli

import (
	"fmt"
	"os"

	"mobius/internal/tui"

	"github.com/charmbracelet/x/term"
)

// IsTTYFunc reports whether interactive fullscreen mode should be used.
type IsTTYFunc func() bool

// StartInteractive chooses fullscreen TUI or plain REPL.
func StartInteractive(app *App) {
	StartInteractiveWithTTY(app, defaultIsTTY)
}

func defaultIsTTY() bool {
	if os.Getenv("MOBIUS_PLAIN") == "1" {
		return false
	}
	return term.IsTerminal(os.Stdout.Fd())
}

// StartInteractiveWithTTY is the injectable entry for tests.
func StartInteractiveWithTTY(app *App, isTTY IsTTYFunc) {
	settings, err := tui.LoadSettings("config/tui.toml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: config/tui.toml: %v — using plain REPL\n", err)
		StartREPL(app)
		return
	}
	if isTTY() {
		backend := NewTUIBackend(app)
		if err := tui.Run(backend, settings); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		}
		return
	}
	StartREPL(app)
}
