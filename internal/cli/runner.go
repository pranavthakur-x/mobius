package cli

import (
	"context"
	"fmt"

	"mobius/pkg/session"
)

// RunGoal executes userInstruction synchronously in s, prints any reply,
// and records the user and assistant turns in the chat transcript.
func RunGoal(app *App, s *session.Session, userInstruction string) error {
	return runGoal(app, s, userInstruction, true)
}

// RunGoalAsync prints the immediate orchestration reply without waiting on workers.
func RunGoalAsync(app *App, s *session.Session, userInstruction string) error {
	return runGoal(app, s, userInstruction, false)
}

func runGoal(app *App, s *session.Session, userInstruction string, sync bool) error {
	if app == nil {
		return fmt.Errorf("app is required")
	}
	if s == nil {
		return fmt.Errorf("session is required")
	}
	if !s.Started() {
		s.MarkStarted(s.Name())
	}
	ctx := context.Background()
	app.RememberTurn(s.ID, "user", "user", userInstruction)
	res, err := app.DispatchUserInput(ctx, s, userInstruction, sync)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return err
	}
	if res.Reply != "" {
		fmt.Printf("%s\n", res.Reply)
		app.RememberTurn(s.ID, "assistant", "orchestrator", res.Reply)
	}
	return nil
}
