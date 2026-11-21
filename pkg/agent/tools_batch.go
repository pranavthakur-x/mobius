package agent

import (
	"context"
	"fmt"
	"mobius/internal/artifact"
	"mobius/pkg/agentctx"
	"mobius/pkg/events"
	"mobius/pkg/llm"
	"sync"
)

type toolCallOutcome struct {
	toolCall llm.ToolCall
	output   string
	toolErr  string
}

func toolModifiesFiles(name string) bool {
	return name == "write_file" || name == "edit_file"
}

func (a *Agent) executeToolCall(ctx context.Context, tc llm.ToolCall) toolCallOutcome {
	out := toolCallOutcome{toolCall: tc}

	tool, err := a.registry.Get(tc.Function.Name)
	if err != nil {
		out.output = fmt.Sprintf("Error: tool '%s' not found", tc.Function.Name)
		out.toolErr = out.output
		return out
	}

	raw, execErr := tool.Execute(ctx, tc.Function.Arguments)
	if execErr != nil {
		out.output = fmt.Sprintf("Tool error: %s\nOutput: %s", execErr, raw)
		out.toolErr = execErr.Error()
		return out
	}
	out.output = raw
	return out
}

// runToolCalls executes all tool calls from one model turn. Independent calls run
// concurrently; results are applied to context and telemetry in original call order.
func (a *Agent) runToolCalls(ctx context.Context, c *agentctx.ConversationContext, step int, calls []llm.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}

	var skipped []llm.ToolCall
	if budget := a.toolCallBudget(step); budget > 0 && len(calls) > budget {
		skipped = append([]llm.ToolCall(nil), calls[budget:]...)
		calls = calls[:budget]
	}

	outcomes := make([]toolCallOutcome, len(calls))

	if len(calls) == 1 {
		outcomes[0] = a.executeToolCall(ctx, calls[0])
	} else {
		var wg sync.WaitGroup
		wg.Add(len(calls))
		for i := range calls {
			go func(i int) {
				defer wg.Done()
				outcomes[i] = a.executeToolCall(ctx, calls[i])
			}(i)
		}
		wg.Wait()
	}

	fileModified := false
	for i, outcome := range outcomes {
		tc := calls[i]
		if toolModifiesFiles(tc.Function.Name) {
			fileModified = true
		}

		a.emitLine("[Tool] %s(%s)\n", tc.Function.Name, tc.Function.Arguments)

		output := outcome.output
		toolErr := outcome.toolErr

		if a.artifactStore != nil {
			result := artifact.Intercept(a.artifactStore, a.threadID, tc.Function.Name, output)
			c.AddToolResult(tc.ID, result.Observation)
			if a.events != nil {
				_ = a.events.Append(ctx, events.Event{
					ThreadID:   a.threadID,
					Step:       step,
					Type:       events.EventToolResult,
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
					ToolArgs:   tc.Function.Arguments,
					ToolOutput: result.Observation,
					ContentRef: result.ArtifactRef,
					ToolError:  toolErr,
				})
			}
			continue
		}

		c.AddToolResult(tc.ID, output)
		if a.events != nil {
			_ = a.events.Append(ctx, events.Event{
				ThreadID:   a.threadID,
				Step:       step,
				Type:       events.EventToolResult,
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				ToolArgs:   tc.Function.Arguments,
				ToolOutput: output,
				ToolError:  toolErr,
			})
		}
	}

	for _, tc := range skipped {
		note := "Not run: config/agent.toml step budget is already used for this turn. Answer from what you have."
		c.AddToolResult(tc.ID, note)
		a.emitLine("[Tool] %s skipped (step budget)\n", tc.Function.Name)
	}

	return fileModified
}

// toolCallBudget is how many tool calls this step may run.
// Zero means the agent has no configured step limit.
func (a *Agent) toolCallBudget(step int) int {
	if a == nil || a.maxSteps <= 0 {
		return 0
	}
	remaining := a.maxSteps - step + 1
	if remaining < 1 {
		return 1
	}
	return remaining
}
