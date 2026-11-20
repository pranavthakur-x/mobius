package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"mobius/pkg/agentctx"
	"mobius/pkg/llm"
	"mobius/pkg/tools"
)

type countingProvider struct {
	calls atomic.Int32
}

func (p *countingProvider) Name() string { return "counting" }

func (p *countingProvider) Generate(context.Context, *llm.ChatRequest) (*llm.ChatResponse, error) {
	p.calls.Add(1)
	return &llm.ChatResponse{Content: "ok", Usage: llm.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
}

func TestRunEmptyWithNoNotesReturnsWithoutLLM(t *testing.T) {
	chdirModuleRoot(t)
	reg := tools.NewRegistry()
	p := &countingProvider{}
	ag, err := NewAgent(p, reg, "m", 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	ag.SetLineHook(func(string) {})
	out, err := ag.Run(context.Background(), agentctx.NewConversationContext("sys"), "   ")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("expected empty result, got %q", out)
	}
	if p.calls.Load() != 0 {
		t.Fatalf("expected no LLM calls, got %d", p.calls.Load())
	}
}

func TestRunEmptyWithPendingNoteDrains(t *testing.T) {
	chdirModuleRoot(t)
	reg := tools.NewRegistry()
	p := &countingProvider{}
	ag, err := NewAgent(p, reg, "m", 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	ag.SetLineHook(func(string) {})
	ag.PushNote("follow-up only")
	ctx := agentctx.NewConversationContext("sys")
	_, err = ag.Run(context.Background(), ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 1 {
		t.Fatalf("expected one LLM call, got %d", p.calls.Load())
	}
	found := false
	for _, m := range ctx.Messages() {
		if m.Content == "follow-up only" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected pending note to be drained on continuation run")
	}
}
