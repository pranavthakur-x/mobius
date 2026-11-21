package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"mobius/pkg/agentctx"
	"mobius/pkg/llm"
	"mobius/pkg/tools"
)

type fakeStreamingProvider struct {
	mu      sync.Mutex
	steps   []func(onEvent llm.StreamCallback) (*llm.ChatResponse, error)
	callIdx int
}

func (p *fakeStreamingProvider) Name() string { return "fake-stream" }

func (p *fakeStreamingProvider) Generate(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: "non-stream"}, nil
}

func (p *fakeStreamingProvider) GenerateStream(_ context.Context, _ *llm.ChatRequest, onEvent llm.StreamCallback) (*llm.ChatResponse, error) {
	p.mu.Lock()
	idx := p.callIdx
	p.callIdx++
	fn := p.steps[idx]
	p.mu.Unlock()
	return fn(onEvent)
}

func TestAgentRunStreamsTokensAndToolStarts(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&echoTool{})

	prov := &fakeStreamingProvider{
		steps: []func(onEvent llm.StreamCallback) (*llm.ChatResponse, error){
			func(onEvent llm.StreamCallback) (*llm.ChatResponse, error) {
				onEvent(llm.StreamEvent{TextDelta: "Let me check"})
				onEvent(llm.StreamEvent{ToolName: "echo", ToolID: "tc_1"})
				return &llm.ChatResponse{
					Content: "Let me check",
					ToolCalls: []llm.ToolCall{{
						ID:       "tc_1",
						Type:     "function",
						Function: llm.FunctionCall{Name: "echo", Arguments: `{"text":"ping"}`},
					}},
					Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
				}, nil
			},
			func(onEvent llm.StreamCallback) (*llm.ChatResponse, error) {
				onEvent(llm.StreamEvent{TextDelta: "All "})
				onEvent(llm.StreamEvent{TextDelta: "done."})
				return &llm.ChatResponse{
					Content: "All done.",
					Usage:   llm.Usage{PromptTokens: 12, CompletionTokens: 4, TotalTokens: 16},
				}, nil
			},
		},
	}

	ag := &Agent{
		threadID: "stream-test",
		provider: prov,
		registry: reg,
		model:    "test-model",
		maxSteps: 5,
		timeout:  5 * time.Second,
		lineHook: func(string) {},
	}

	var mu sync.Mutex
	var deltas []string
	var toolsSeen []string
	ag.SetStreamHook(func(ev llm.StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		if ev.TextDelta != "" {
			deltas = append(deltas, ev.TextDelta)
		}
		if ev.ToolName != "" {
			toolsSeen = append(toolsSeen, ev.ToolName)
		}
	})

	conv := agentctx.NewConversationContext("sys")
	reply, err := ag.Run(context.Background(), conv, "run echo")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reply != "All done." {
		t.Fatalf("reply = %q, want %q", reply, "All done.")
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(deltas, "|") != "Let me check|All |done." {
		t.Fatalf("deltas = %v", deltas)
	}
	if strings.Join(toolsSeen, ",") != "echo" {
		t.Fatalf("toolsSeen = %v", toolsSeen)
	}
}

type echoTool struct{}

func (e *echoTool) Name() string        { return "echo" }
func (e *echoTool) Description() string { return "echoes input" }
func (e *echoTool) Schema() tools.ToolSchema {
	return tools.ToolSchema{Type: "object", Properties: map[string]tools.Property{"text": {Type: "string"}}}
}
func (e *echoTool) Execute(_ context.Context, args string) (string, error) {
	return "echo:" + args, nil
}
