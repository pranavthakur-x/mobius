package main

import (
	"context"
	"fmt"
	"mobius/internal/cli"
	"mobius/pkg/agent"
	"mobius/pkg/events"
	"mobius/pkg/llm"
	"mobius/pkg/orchestrator"
	"mobius/pkg/roles"
	"mobius/pkg/tools"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

var Version = "v0.1.0"

func printUsage() {
	fmt.Printf(`mobius %s — High-Performance AI Agent Harness in Go

Usage:
  mobius [flags] [directory] [instruction]

Examples:
  mobius                              # Launch interactive TUI in current directory
  mobius "fix test failures"          # Run one-shot instruction in current directory
  mobius ~/my-repo "add unit tests"   # Run instruction in target directory

Flags:
  -v, --version    Print mobius version
  -h, --help       Show this help message

Environment Variables:
  ANTHROPIC_API_KEY   Anthropic Claude API key
  OPENAI_API_KEY      OpenAI API key
  GEMINI_API_KEY      Google Gemini API key
  DEEPSEEK_API_KEY    DeepSeek API key
  OPENROUTER_API_KEY  OpenRouter API key
`, Version)
}

func main() {
	_ = godotenv.Load()

	for _, arg := range os.Args[1:] {
		if arg == "-v" || arg == "--version" {
			fmt.Printf("mobius %s\n", Version)
			return
		}
		if arg == "-h" || arg == "--help" {
			printUsage()
			return
		}
	}

	workDir, args := workDirFromArgs(os.Args[1:])
	registry := tools.NewDefaultRegistry(workDir)

	eventsDir := filepath.Join(workDir, ".mobius", "events")
	eventStore, err := events.NewFileEventStore(eventsDir, 256)
	if err != nil {
		fallbackDir := filepath.Join(os.TempDir(), ".mobius", "events")
		eventStore, err = events.NewFileEventStore(fallbackDir, 256)
		if err != nil {
			fmt.Printf("Error: Failed to init event store: %v\n", err)
			os.Exit(1)
		}
	}
	defer eventStore.Close()

	cfg, err := llm.LoadConfigWithFallback("")
	if err != nil {
		fmt.Printf("Error: Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Auto-select provider based on available environment variables if default has no key
	cfg.AutoSelectModel()

	roleStore, err := roles.Open(context.Background(), "roles")
	if err != nil {
		fmt.Printf("Error: Failed to load roles: %v\n", err)
		os.Exit(1)
	}

	if err := llm.LoadExtraModels(cfg, ""); err != nil {
		fmt.Printf("Error: extra models: %v\n", err)
	}
	if conns, active, err := llm.LoadSaved(""); err == nil {
		cfg.ImportConnections(conns, active)
	} else if !os.IsNotExist(err) {
		fmt.Printf("Error: saved API connections: %v\n", err)
	}

	provider, err := cfg.GetProviderForModel("")
	if err != nil {
		fmt.Printf("Error: %v\n\n", err)
		fmt.Println("Please set an API key in your environment:")
		fmt.Println("  export ANTHROPIC_API_KEY=\"sk-ant-...\"")
		fmt.Println("  export OPENAI_API_KEY=\"sk-...\"")
		fmt.Println("  export GEMINI_API_KEY=\"...\"")
		fmt.Println("  export DEEPSEEK_API_KEY=\"...\"")
		fmt.Println("  export OPENROUTER_API_KEY=\"sk-or-...\"")
		os.Exit(1)
	}

	pCost, cCost := cfg.GetPrices(cfg.ActiveModel)
	model := orchestrator.ModelSpec{
		Name:            cfg.ActiveModel,
		Provider:        provider,
		PromptPrice:     pCost,
		CompletionPrice: cCost,
	}

	factory := func(reg *tools.Registry, m orchestrator.ModelSpec) (*agent.Agent, error) {
		return agent.NewAgent(m.Provider, reg, m.Name, m.PromptPrice, m.CompletionPrice, eventStore)
	}

	ctx := context.Background()
	app, err := cli.Boot(ctx, roleStore, registry, model, factory, eventStore, cfg, workDir)
	if err != nil {
		fmt.Printf("Error: Failed to open chats: %v\n", err)
		os.Exit(1)
	}

	if len(args) > 0 {
		userInstruction := strings.Join(args, " ")
		active, err := app.Sessions.GetActive()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		if err := cli.RunGoal(app, active, userInstruction); err != nil {
			os.Exit(1)
		}
		app.Workspaces.Each(func(w *orchestrator.Workspace) { w.Close() })
		return
	}

	cli.StartInteractive(app)
}

// workDirFromArgs uses the first argument when it is a directory.
// Anything after that is a one-shot instruction. A non-directory first
// argument stays part of the instruction.
func workDirFromArgs(args []string) (string, []string) {
	if len(args) == 0 {
		return ".", args
	}
	info, err := os.Stat(args[0])
	if err != nil || !info.IsDir() {
		return ".", args
	}
	abs, err := filepath.Abs(args[0])
	if err != nil {
		return args[0], args[1:]
	}
	return abs, args[1:]
}
