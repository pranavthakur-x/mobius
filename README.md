# Mobius

<p align="center">
  <strong>Multi-agent coding harness for the terminal, built in Go.</strong><br />
  Route work to focused teams, keep their tool access narrow, and retain the chat history that matters.
</p>

<p align="center">
  <a href="#quick-start"><img src="https://img.shields.io/badge/Start-Quick%20start-blue?style=flat-square" alt="Quick start"></a>
  <a href="#key-capabilities"><img src="https://img.shields.io/badge/Language-Go%201.25+-00ADD8?style=flat-square&logo=go" alt="Go Version"></a>
  <a href="#license"><img src="https://img.shields.io/badge/License-MIT-green?style=flat-square" alt="License"></a>
  <a href="#configuration"><img src="https://img.shields.io/badge/Configuration-TOML-orange?style=flat-square" alt="Configuration"></a>
  <a href="#development--testing"><img src="https://img.shields.io/badge/Linter-golangci--lint-brightgreen?style=flat-square" alt="Linter"></a>
</p>

---

## Overview

**Mobius** is a terminal-first, multi-agent harness for working on codebases. A chat begins with an **orchestrator**. It answers straightforward requests itself or delegates a scoped task to a role such as Software Engineer, Code Review, or Product Manager.

Each worker has its own conversation and only the tools named in its role definition. They can work in the same directory without receiving one another's transcripts. Mobius also provides configurable execution limits, saved chats, automatic context compaction, token-cost tracking, and structured event logs.

Use the fullscreen TUI in a terminal, or set `MOBIUS_PLAIN=1` for the plain REPL.

## Why Mobius?

- **Focused delegation** — route implementation, review, and product work to separate roles.
- **Least-privilege tools** — role YAML files define exactly which filesystem and shell tools a worker receives.
- **Local control** — select the working directory, provider endpoint, model, and spending limits.
- **Persistent by default** — chats and event traces stay on disk so work can continue across runs.

---

## Architecture

```text
  You
   │
   ▼
┌───────────────────────────────────────────────────────────────────────┐
│ Mobius CLI — TUI (default) or plain REPL                              │
│ Chats · Connect API · Models · Teams · Work on path                   │
└───────────────────────────────────┬───────────────────────────────────┘
                                    │
                                    ▼
┌───────────────────────────────────────────────────────────────────────┐
│ Orchestrator — one per active chat                                    │
│ Decides: reply directly, or delegate one scoped task to one role      │
└───────────────┬───────────────────────────────────────┬───────────────┘
                │ direct reply                          │ delegate
                ▼                                       ▼
          ┌──────────────┐                ┌──────────────────────────────┐
          │ Chat response│                │          Teams               │
          └──────────────┘                │  Engineering    Product      │
                                          │  ┌───────────┐  ┌──────────┐ │
                                          │  │ Software  │  │ Product  │ │
                                          │  │ Engineer  │  │ Manager  │ │
                                          │  ├───────────┤  └──────────┘ │
                                          │  │ Code      │               │
                                          │  │ Review    │               │
                                          │  └───────────┘               │
                                          └──────────────┬───────────────┘
                                                         │
                                 each role has its own  │  context and tools
                                                         ▼
┌───────────────────────────────────────────────────────────────────────┐
│ Agent runtime                                                          │
│ ReAct loop · max steps · cost ceiling · timeout · context compaction  │
└───────────────┬───────────────────────────────────────┬───────────────┘
                │                                       │
                ▼                                       ▼
      ┌───────────────────────┐               ┌────────────────────────┐
      │ Selected LLM provider │               │ Shared work directory  │
      │ and model             │               │ Role-scoped file/tools │
      └───────────────────────┘               └────────────────────────┘

  Persistent state:  .mobius/chats/  ·  .mobius/events/  ·  roles/  ·  config/
```

The shared work directory is the only common worker surface: role conversations remain isolated. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and [docs/2026-11-21.md](docs/2026-11-21.md) for the routing and isolation model.

---

## Key capabilities

### Multi-team orchestration (`pkg/orchestrator`)

- One orchestrator per chat; structured routing (`reply` vs `delegate` to `team_id` / `role_id`).
- Departments under `roles/departments/` spawn a team at workspace init (e.g. engineering, product).
- Role YAML defines the system prompt and allow-listed tool set; unknown tools fail closed.
- Workers retain their own context across tasks, but their transcripts are never automatically injected into another worker's prompt.

### Interactive TUI (`pkg/cli/tui`)

- Fullscreen chat UI with footer menus for chats, models, APIs, teams, and the work path.
- Live spend bar and optional context sidebar (`config/tui.toml`).
- Set `MOBIUS_PLAIN=1` to force the slash-command REPL instead of the TUI.

### Saved API connections (`pkg/llm`)

- **Connect** in the TUI stores multiple named endpoints in `~/.config/mobius/connections.toml`.
- API keys are stored separately in the system keyring, never in that TOML file or this repository.
- `config/model_config.toml` supplies a built-in provider catalog, default model, and baseline pricing. Saved connections can become the active API at runtime.

### Chat persistence (`pkg/session`)

- Transcripts under `.mobius/chats/` with an index for switch / delete.
- Legacy `.mobius/events/*.jsonl` can be imported on boot; deleted chat IDs are not re-imported.

### Autonomous ReAct runtime (`pkg/agent`)

- Step-budgeted Reason–Act–Observe loops with per-turn timeouts.
- Built-in tools: `run_command`, `view_file`, `edit_file`, `write_file`, `list_dir`, `grep_search`.

### Context compaction (`pkg/agentctx`)

- Token estimator and 80% high-watermark compaction with tool-pair integrity and checkpoint summaries.

### Artifacts & budget (`pkg/artifact`, `pkg/budget`)

- Large tool outputs offloaded to `.mobius/artifacts/`.
- Live USD spend tracking and a hard `max_cost` ceiling.

### Telemetry (`pkg/events`)

- Append-only JSONL per thread under `.mobius/events/`.

---

## Project structure

```text
mobius/
├── cmd/mobius/                  # CLI entrypoint
├── config/
│   ├── agent.toml               # max_steps, max_cost, timeout (all agents)
│   ├── model_config.toml        # Provider catalog & pricing
│   └── tui.toml                 # Sidebar / trace UI flags
├── roles/                       # Role YAML + departments/
├── docs/                        # Architecture, roadmap, orchestration notes
├── pkg/
│   ├── agent/                   # ReAct loop
│   ├── agentctx/                # Context compaction
│   ├── artifact/                # Large-output offload
│   ├── budget/                  # Cost accounting
│   ├── cli/                     # Boot, REPL, TUI backend
│   ├── events/                  # JSONL EventStore
│   ├── llm/                     # Providers, connections, keystore
│   ├── orchestrator/            # Workspace, teams, routing
│   ├── redact/                  # Secret scrubbing
│   ├── roles/                   # YAML loader & store
│   ├── session/                 # Chats & sessions
│   └── tools/                   # Tool registry
├── .github/workflows/ci.yml     # fmt, mod, vet, lint, race tests, build
├── Makefile
└── README.md
```

---

## Quick start

### Prerequisites

- **Go 1.25+** (see `go.mod`; CI uses Go 1.25 with `GOTOOLCHAIN=local`)
- An LLM API key for a hosted provider, or a local OpenAI-compatible endpoint such as Ollama

### Build and run

```bash
git clone https://github.com/pranavthakur0-0/mobius.git
cd mobius
make build
./bin/mobius
```

On first launch, choose **Connect API** from the footer menu and enter an endpoint URL, key, and model. The endpoint metadata is saved under `~/.config/mobius/`; the key itself is saved in the system keyring.

Alternatively, copy `.env.example` to `.env` and set the environment variable referenced by a provider in `config/model_config.toml`.

---

## Usage

### Start a chat

```bash
./bin/mobius
```

The default terminal interface offers **Start New Chat**, **Models**, **Connect API**, **Switch / List Chats**, **Show Teams**, and **Work on path**. Use the Models menu to select a saved API's model or add one with optional token pricing.

When a prompt needs work from a role, the orchestrator acknowledges the delegation. The worker's own session performs the task; workers do not silently receive other workers' conversations.

### Work directory

```bash
./bin/mobius /path/to/your/repo
```

If the first argument is a directory, Mobius uses it as the working directory for its tools. Any remaining arguments are treated as a one-shot instruction.

### One-shot mode

```bash
./bin/mobius /path/to/repo "Summarize open TODOs in pkg/"
```

Runs a single goal on the active chat and exits.

### Plain REPL

```bash
MOBIUS_PLAIN=1 ./bin/mobius
```

### Programmatic use

Embed agents and tools directly in Go — see [examples/basic_agent](examples/basic_agent/) and [examples/custom_tool](examples/custom_tool/).

---

## Plain REPL commands

When `MOBIUS_PLAIN=1` or the TUI cannot start, these slash commands are available:

| Command | Description |
| :--- | :--- |
| `/models` | Switch model / provider |
| `/newchat` | New isolated chat |
| `/listchats` | Select a saved chat |
| `/connect` | Add or update a saved API connection |
| `/workdir` | Set the directory used by tools |
| `/exit` | Exit Mobius |

In the TUI, the corresponding actions live in the footer menus.

---

## Configuration

### Agent limits (`config/agent.toml`)

```toml
[agent]
max_steps = 50
max_cost = 2.00
timeout_seconds = 300
```

These limits apply to the orchestrator and every delegated worker. Keep `max_cost` realistic for the providers and model prices you configure.

### Providers (`config/model_config.toml`)

Defines built-in provider entries, the default model, and USD-per-million-token prices. `env_key` names the optional environment variable used for that provider. Example:

```toml
default_model = "deepseek-chat"

[providers.deepseek]
name     = "DeepSeek"
type     = "openai"
base_url = "https://api.deepseek.com/v1"
env_key  = "DEEPSEEK_API_KEY"
models   = ["deepseek-chat", "deepseek-reasoner"]
```

### TUI (`config/tui.toml`)

```toml
show_steps = true
show_trace = true
show_context = false
```

### Roles

- `roles/*.yml` — a role id, system prompt, department, and allowed `tools`
- `roles/departments/*.yml` — a department's roles and default lead

Add a role, include it in a department, then restart Mobius to load the updated catalog. Keep tool grants minimal: for example, the bundled Code Review role can inspect files and search, but cannot edit files or run commands.

---

## Development & testing

Use the `Makefile` targets while developing:

```bash
make test          # unit tests
make test-race     # tests with -race
make lint          # golangci-lint
make fmt           # gofmt
make tidy          # go mod tidy + verify
make all           # format, vet, lint, test, build
```

Before opening a PR, run:

```bash
gofmt -w .
go mod tidy
go vet ./...
golangci-lint run ./...
go test -race ./...
go build ./...
```

CI on **Go 1.25** independently checks formatting, module integrity and tidiness, `go vet`, golangci-lint v2.5, race-enabled tests, every package build, and the `cmd/mobius` binary build.

---

## License

Distributed under the **MIT License**. See [LICENSE](LICENSE).