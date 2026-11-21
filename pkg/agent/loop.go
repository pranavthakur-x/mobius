package agent

import (
	"context"
	"fmt"
	"mobius/pkg/agentctx"
	"mobius/pkg/events"
	"mobius/pkg/llm"
	"mobius/pkg/sensors"
	"strings"
)

// Run executes the multi-step ReAct loop for userInstruction against c,
// enforcing step, cost, and timeout limits and returning the final assistant reply.
func (a *Agent) Run(ctx context.Context, c *agentctx.ConversationContext, userInstruction string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	trimmedGoal := strings.TrimSpace(userInstruction)
	if trimmedGoal == "" {
		if !a.HasPendingNotes() {
			return "", nil
		}
	} else {
		c.AddUserMessage(userInstruction)
		if a.events != nil {
			_ = a.events.Append(ctx, events.Event{
				ThreadID: a.threadID,
				Type:     events.EventUserMessage,
				Content:  userInstruction,
			})
		}
		a.emitLine("[Goal] %s\n\n", userInstruction)
	}

	consecutiveSensorFailures := 0
	const maxSensorRetries = 3

	for step := 1; step <= a.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("agent interrupted: %w", err)
		}

		for _, note := range a.drainNotes() {
			c.AddUserMessage(note)
		}

		currentTokenCount := agentctx.EstimateConversationTokens(c.Messages())
		if a.compactor != nil && a.compactor.ShouldCompact(currentTokenCount) {
			a.emitLine("[Step %d/%d] Summarizing...\n", step, a.maxSteps)
			_ = a.compactor.Compact(ctx, c)
		}

		a.emitLine("[Step %d/%d] Thinking...\n", step, a.maxSteps)

		a.mu.RLock()
		model := a.model
		provider := a.provider
		sensorReg := a.sensorRegistry
		tracker := a.tracker
		streamHook := a.streamHook
		a.mu.RUnlock()

		req := &llm.ChatRequest{
			Model:   model,
			Message: c.Messages(),
			Tools:   a.toolDefs,
		}

		var onStream llm.StreamCallback
		if streamHook != nil {
			onStream = llm.StreamCallback(streamHook)
		}
		resp, err := llm.GenerateWithStream(ctx, provider, req, onStream)
		if err != nil {
			return "", fmt.Errorf("step %d LLM call failed: %w", step, err)
		}

		if tracker != nil {
			tracker.Add(resp.Usage)
			// 2. Check if we exceeded max allowed cost
			if err := tracker.Check(); err != nil {
				return "", fmt.Errorf("step %d aborted by budget: %w", step, err)
			}
			// 3. Print live status
			a.emitLine("%s\n\n", tracker.Status())
		}

		c.AddAssistantMessage(resp.Content, resp.ToolCalls)
		if resp.Content != "" {
			a.emitLine("[Thought]\n%s\n\n", resp.Content)
		}

		if len(resp.ToolCalls) == 0 {
			// If verification checks are active and we had sensor failures, guard against premature completion
			if sensorReg != nil && sensorReg.Count() > 0 && consecutiveSensorFailures > 0 {
				a.emitLine("[Sensor] Validating before completion...\n")
				verdicts := sensorReg.RunAll(ctx)
				var stillFailing []sensors.Verdict
				for _, v := range verdicts {
					if !v.Passed {
						stillFailing = append(stillFailing, v)
					}
				}
				if len(stillFailing) > 0 {
					consecutiveSensorFailures++
					if consecutiveSensorFailures <= maxSensorRetries {
						sensorMsg := "[Verification Incomplete] You cannot conclude the task because verification checks are currently failing:\n\n" + formatSensorFailures(stillFailing)
						a.emitLine("%s\n\n", sensorMsg)
						c.AddUserMessage(sensorMsg)
						continue
					}
				}
			}

			if notes := a.drainNotes(); len(notes) > 0 {
				for _, note := range notes {
					c.AddUserMessage(note)
				}
				continue
			}

			if a.events != nil {
				_ = a.events.Append(ctx, events.Event{
					ThreadID: a.threadID,
					Step:     step,
					Type:     events.EventAssistantMessage,
					Content:  resp.Content,
					Usage:    &resp.Usage,
				})
			}
			return resp.Content, nil
		}

		fileModified := a.runToolCalls(ctx, c, step, resp.ToolCalls)

		// Self-healing sensor verification
		if fileModified && sensorReg != nil && sensorReg.Count() > 0 {
			a.emitLine("[Sensor] Running verification checks...\n")
			verdicts := sensorReg.RunAll(ctx)
			var failed []sensors.Verdict
			for _, v := range verdicts {
				if !v.Passed {
					failed = append(failed, v)
				}
			}

			if len(failed) > 0 {
				consecutiveSensorFailures++
				sensorMsg := formatSensorFailures(failed)
				a.emitLine("%s\n\n", sensorMsg)

				if consecutiveSensorFailures >= maxSensorRetries {
					sensorMsg += fmt.Sprintf("\n\n[Warning] Sensor check has failed %d consecutive times. Please carefully rethink your changes.", consecutiveSensorFailures)
				}

				c.AddUserMessage(sensorMsg)

				if a.events != nil {
					_ = a.events.Append(ctx, events.Event{
						ThreadID: a.threadID,
						Step:     step,
						Type:     events.EventSensorResult,
						Content:  sensorMsg,
					})
				}
			} else {
				consecutiveSensorFailures = 0
				a.emitLine("[Sensor] All %d verification checks passed.\n\n", len(verdicts))
				if a.events != nil {
					_ = a.events.Append(ctx, events.Event{
						ThreadID: a.threadID,
						Step:     step,
						Type:     events.EventSensorResult,
						Content:  fmt.Sprintf("All %d verification checks passed.", len(verdicts)),
					})
				}
			}
		}
	}
	return "", fmt.Errorf("agent reached maximum step budget (%d steps)", a.maxSteps)
}

func formatSensorFailures(failed []sensors.Verdict) string {
	var sb strings.Builder
	sb.WriteString("[Automatic Sensor Check Failed]\n")
	for _, v := range failed {
		name := v.SensorName
		if name == "" {
			name = "verification"
		}
		fmt.Fprintf(&sb, "Sensor: %s\nOutput:\n%s\n", name, strings.TrimSpace(v.Output))
	}
	sb.WriteString("\nThe code did not compile or pass verification. Please inspect the compiler/test error above and fix the issue before proceeding.")
	return sb.String()
}
