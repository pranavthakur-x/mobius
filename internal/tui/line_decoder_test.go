package tui

import (
	"strings"
	"testing"
)

func TestBudgetNeverTranscriptEvent(t *testing.T) {
	dec := NewLineDecoder()
	src := SourceRef{ThreadID: "t1"}
	raw := "[Budget] Tokens: 10 In, 5 Out | Cost: $0.00100 (₹0.0950)"
	evs := dec.Decode(src, raw)
	if len(evs) != 1 {
		t.Fatalf("expected 1 budget event, got %d", len(evs))
	}
	if _, ok := evs[0].(BudgetEvent); !ok {
		t.Fatalf("expected BudgetEvent, got %T", evs[0])
	}
	plain := "[Budget] Tokens: 10 In, 5 Out"
	evs = dec.Decode(src, plain)
	if len(evs) != 1 {
		t.Fatalf("expected 1 budget event without cost, got %#v", evs)
	}
	if _, ok := evs[0].(BudgetEvent); !ok {
		t.Fatalf("expected BudgetEvent, got %T", evs[0])
	}
}

func TestThinkingStepIsHidden(t *testing.T) {
	dec := NewLineDecoder()
	if evs := dec.Decode(SourceRef{Name: "Software Engineer"}, "[Step 2/25] Thinking..."); evs != nil {
		t.Fatalf("thinking step should not become transcript text, got %#v", evs)
	}
}

func TestGoalDiscarded(t *testing.T) {
	dec := NewLineDecoder()
	if evs := dec.Decode(SourceRef{}, "[Goal] hi"); evs != nil {
		t.Fatalf("goal should be discarded")
	}
}

func TestClassifyStepThoughtToolSensor(t *testing.T) {
	dec := NewLineDecoder()
	src := SourceRef{Name: "worker"}
	cases := []struct {
		in   string
		want string
	}{
		{"[Step 1/5] Summarizing...\n", "AgentStepEvent"},
		{"[Thought]\nhello\n", "AgentThoughtEvent"},
		{"[Tool] run_command({\"x\":1})", "AgentToolEvent"},
		{"[Sensor] Running verification checks...\n", "AgentSensorEvent"},
	}
	for _, c := range cases {
		evs := dec.Decode(src, c.in)
		if len(evs) != 1 {
			t.Fatalf("%q: expected 1 event", c.in)
		}
		if got := typeName(evs[0]); got != c.want {
			t.Fatalf("%q: want %s got %s", c.in, c.want, got)
		}
	}
}

func TestANSIStripped(t *testing.T) {
	dec := NewLineDecoder()
	evs := dec.Decode(SourceRef{}, "[Thought]\n\x1b[31mred\x1b[0m")
	th, ok := evs[0].(AgentThoughtEvent)
	if !ok || strings.Contains(th.Text, "\x1b") {
		t.Fatalf("ansi not stripped: %q", th.Text)
	}
}

func typeName(ev LiveEvent) string {
	switch ev.(type) {
	case AgentStepEvent:
		return "AgentStepEvent"
	case AgentThoughtEvent:
		return "AgentThoughtEvent"
	case AgentToolEvent:
		return "AgentToolEvent"
	case AgentSensorEvent:
		return "AgentSensorEvent"
	default:
		return "other"
	}
}
