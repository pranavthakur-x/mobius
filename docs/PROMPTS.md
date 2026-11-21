# Mobius prompt design

Prompts are split into non-overlapping layers so every token earns its slot and no rule is repeated across layers:

| Layer | File | Role |
|-------|------|------|
| Core harness rules | `pkg/agentctx/prompt.go` | Grounding, workflow, tool discipline, error recovery, reversibility/safety, output shape |
| Workspace guide | `config/repl_guide.md` (via `config/guides.toml`) | Stack-agnostic workspace conventions (native verification discovery, sensor compliance, style matching) |
| Specialist roles | `roles/*.yml` | Role persona, craft methodology, and role-specific output contract |
| Multi-agent router | `pkg/orchestrator/prompt.go` | Zero-tool JSON routing schema, untrusted-input boundary, self-contained worker briefs |
| Execution limits | `pkg/session/session.go` | Step, USD cost, and timeout caps from `config/agent.toml` |

**Composition:**
- Solo sessions (`session.NewSession` with empty override): `Core -> Repo guides -> Limits`.
- Role workers (`session.NewRoleSession`): `Core -> Repo guides -> Role YAML -> Limits`.
- Orchestrator (`session.NewSession` with `orchestratorSystemPrompt`): `Router prompt -> Limits` only (zero tools).

`AGENTS.md` is kept on disk for IDE rules and automatic sensors (`## Verification Commands`), and intentionally excluded from `config/guides.toml` so agents do not pay a per-turn token tax re-reading directory maps and roadmaps.

## Principles used

1. **Minimal non-overlapping rules** — keep total instruction count low (curse of instructions / IFScale) and avoid duplicating base rules inside roles or guides.
2. **Macro XML + micro Markdown** — XML boundaries (`<grounding>`, `<workflow>`, `<tools>`, `<safety>`, `<role_instructions>`, `<user_message>`) strengthen adherence and prompt-injection isolation across model families.
3. **Grounding & causal proof** — read before referencing/editing, never claim verification without running it, and require `path:line` + failure trigger in code reviews (OpenAI Codex `review_prompt.md` pattern).
4. **Self-contained handoffs & anti-loop recovery** — orchestrator writes standalone briefs for isolated workers; workers change hypothesis on errors and stop/report after repeated failures (MAST multi-agent taxonomy).
5. **Ratchet on observed failures** — add rules only for observed failure traces in `.mobius/events/<thread>.jsonl`, not speculative edge cases.
