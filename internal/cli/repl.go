package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"mobius/pkg/orchestrator"

	"github.com/charmbracelet/x/term"
)

func readPrompt(prompt string) (string, bool) {
	return readLine(prompt, false)
}

func readSecret(prompt string) (string, bool) {
	return readLine(prompt, true)
}

func readLine(prompt string, mask bool) (string, bool) {
	fmt.Print(prompt)
	fd := os.Stdin.Fd()
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", false
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	var input []rune
	buf := make([]byte, 1)

	for {
		_, err = os.Stdin.Read(buf)
		if err != nil {
			return "", false
		}

		b := buf[0]

		// Enter key
		if b == '\r' || b == '\n' {
			fmt.Print("\r\n")
			return string(input), true
		}

		// Ctrl+C (ASCII 3)
		if b == 3 {
			fmt.Print("\r\n")
			return "", false
		}

		// Escape key (ASCII 27)
		if b == 27 {
			// If it's a standalone Esc, clear current input
			for len(input) > 0 {
				input = input[:len(input)-1]
				fmt.Print("\b \b")
			}
			continue
		}

		// Slash menu trigger on empty line
		if b == '/' && len(input) == 0 {
			_ = term.Restore(fd, oldState)
			fmt.Print("\r\n")

			selected := ShowActionMenu()
			if selected != "" {
				return selected, true
			}

			oldState, _ = term.MakeRaw(fd)
			fmt.Print(prompt)
			continue
		}

		// Backspace (127 / 8)
		if b == 127 || b == 8 {
			if len(input) > 0 {
				input = input[:len(input)-1]
				fmt.Print("\b \b")
			}
			continue
		}

		// Printable characters
		if b >= 32 && b <= 126 {
			r := rune(b)
			input = append(input, r)
			if mask {
				fmt.Print("•")
			} else {
				fmt.Print(string(r))
			}
		}
	}
}

func StartREPL(app *App) {
	PrintBanner(app.Config.ActiveModel)
	sm := app.Sessions

	for {
		input, ok := readPrompt("mobius > ")
		if !ok {
			break
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		switch input {
		case "/help":
			fmt.Println(orchestrator.HelpText())
		case "/exit":
			fmt.Println("Goodbye!")
			return
		case "/listchats":
			selectedID := ShowSessionMenu(sm)
			if selectedID != "" {
				s, err := sm.SwitchSession(selectedID)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
				} else {
					fmt.Printf("Switched to session: '%s'\n", s.Name())
				}
			}
		case "/newchat":
			handleNewChat(app)
		case "/connect":
			if err := promptConnection(app); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		case "/models":
			activeSession, err := sm.GetActive()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			currentModel := activeSession.Agent.GetModel()
			model, err := ShowModelMenu(app.Config, currentModel)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			if model == "" || model == currentModel {
				continue // User cancelled with Esc or re-selected current model
			}
			provider, err := app.Config.GetProviderForModel(model)
			if err != nil {
				fmt.Printf("Error getting provider for %s: %v\n", model, err)
				continue
			}
			pCost, cCost := app.Config.GetPrices(model)
			activeSession.Agent.SetModel(model, provider, pCost, cCost)
			if ws, err := app.WorkspaceForSession(activeSession); err == nil {
				ws.SetModel(orchestrator.ModelSpec{
					Name:            model,
					Provider:        provider,
					PromptPrice:     pCost,
					CompletionPrice: cCost,
				})
			}
			fmt.Printf("Switched model to '%s'\n", model)
		case "/workdir":
			dir, ok := readPrompt("Path to work on: ")
			if !ok {
				continue
			}
			abs, err := app.SetWorkDir(dir)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			fmt.Printf("Working in %s\n", abs)
		default:
			// Normal AI Prompt
			activeSession, err := sm.GetActive()
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			_ = RunGoalAsync(app, activeSession, input)
		}
	}
}

// Helper to keep StartREPL tidy
func handleNewChat(app *App) {
	sm := app.Sessions
	if unstarted := sm.GetUnstarted(); unstarted != nil {
		_, _ = sm.SwitchSession(unstarted.ID)
		fmt.Printf("Reusing empty session '%s' (ID: %s).\n", unstarted.Name(), unstarted.ID)
		return
	}

	provider, err := app.Config.GetProviderForModel("")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	spec := app.ActiveModelSpec(provider)
	ws, err := orchestrator.NewWorkspace(context.Background(), "new-chat", app.RoleStore, app.BaseTools, spec, app.AgentFactory())
	if err != nil {
		fmt.Printf("Error creating workspace: %v\n", err)
		return
	}
	orch := ws.OrchestratorSession()
	app.attachSensors(orch.Agent)
	newSess := sm.CreateSession("New Chat", orch.Agent)
	newSess.MarkStarted("New Chat")
	app.Workspaces.Register(newSess.ID, ws)
	fmt.Printf("Started new session '%s' (ID: %s)\n", newSess.Name(), newSess.ID)
}

func promptConnection(app *App) error {
	view := app.Config.ConnectionForm()
	for _, api := range app.Config.SavedAPIs() {
		mark := ""
		if api.Current {
			mark = " (current)"
		}
		label := api.Name
		if label == "" {
			label = api.BaseURL
		}
		fmt.Printf("saved: %s · %s%s\n", label, api.Model, mark)
	}
	fmt.Println("Add an API. The key is stored in the system keystore. Leave both prices blank to hide cost.")
	name, ok := readPrompt("Name: ")
	if !ok {
		return nil
	}
	base, ok := readPrompt("API URL [" + view.BaseURL + "]: ")
	if !ok {
		return nil
	}
	if strings.TrimSpace(base) == "" {
		base = view.BaseURL
	}
	keyPrompt := "API key: "
	if view.HasKey {
		keyPrompt = "API key (blank keeps the saved key): "
	}
	key, ok := readSecret(keyPrompt)
	if !ok {
		return nil
	}
	model, ok := readPrompt("Model [" + view.Model + "]: ")
	if !ok {
		return nil
	}
	if strings.TrimSpace(model) == "" {
		model = view.Model
	}
	promptPrice, ok := readPrompt("Prompt price / 1M (blank hides cost): ")
	if !ok {
		return nil
	}
	completion, ok := readPrompt("Completion price / 1M (blank hides cost): ")
	if !ok {
		return nil
	}
	if err := app.ApplyConnection(name, base, key, model, promptPrice, completion); err != nil {
		return err
	}
	if app.Config.PricesKnown(strings.TrimSpace(model)) {
		fmt.Println("Saved.")
		return nil
	}
	fmt.Println("Saved. Cost stays hidden until a price is entered.")
	return nil
}
