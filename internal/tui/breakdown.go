package tui

import (
	"encoding/json"
	"strings"

	"mobius/pkg/agentctx"
	"mobius/pkg/llm"
)

// ContextBreakdown holds honest token estimates for the sidebar legend.
type ContextBreakdown struct {
	Capacity     int
	Used         int
	Remaining    int
	System       int
	ToolDefs     int
	Rules        int
	Conversation int
	CostUSD      float64
	HasPrice     bool
}

// TokenCapacity is the sidebar bar denominator.
func TokenCapacity() int {
	return agentctx.DefaultMaxTokens
}

// ComputeBreakdown estimates categories from messages and optional tool definitions.
func ComputeBreakdown(msgs []llm.Message, toolDefs []llm.ToolDefinition, costUSD float64) ContextBreakdown {
	var system, rules, conv int
	for _, m := range msgs {
		if m.Role == llm.RoleSystem {
			sysTok, ruleTok := splitSystemRuleTokens(m)
			system += sysTok
			rules += ruleTok
			continue
		}
		conv += agentctx.EstimateMessageTokens(m)
	}
	var toolTok int
	for _, td := range toolDefs {
		schema, _ := json.Marshal(td.Function.Parameters)
		toolTok += agentctx.EstimateTokens(td.Function.Name + td.Function.Description + string(schema))
	}
	used := system + toolTok + rules + conv
	cap := TokenCapacity()
	rem := cap - used
	if rem < 0 {
		rem = 0
	}
	return ContextBreakdown{
		Capacity:     cap,
		Used:         used,
		Remaining:    rem,
		System:       system,
		ToolDefs:     toolTok,
		Rules:        rules,
		Conversation: conv,
		CostUSD:      costUSD,
	}
}

// NoteConversation raises the conversation count when the visible transcript
// holds more tokens than the stored context, and keeps used/remaining in step.
func (b *ContextBreakdown) NoteConversation(tokens int) {
	if tokens <= b.Conversation {
		return
	}
	b.Used += tokens - b.Conversation
	b.Conversation = tokens
	if b.Capacity <= 0 {
		b.Capacity = TokenCapacity()
	}
	if b.Used >= b.Capacity {
		b.Remaining = 0
		return
	}
	b.Remaining = b.Capacity - b.Used
}

// EstimateTranscript counts user, reply, step, and tool text in the chat log.
func EstimateTranscript(blocks []TranscriptBlock) int {
	n := 0
	for _, bl := range blocks {
		switch bl.Kind {
		case BlockUser, BlockFinal, BlockThought, BlockStep, BlockSensor, BlockToolGroup:
			n += agentctx.RoleOverhead + agentctx.EstimateTokens(bl.Text)
			for _, tool := range bl.Tools {
				n += agentctx.EstimateTokens(tool.Name + tool.Args)
			}
		}
	}
	return n
}

func splitSystemRuleTokens(m llm.Message) (system, rules int) {
	full := agentctx.EstimateMessageTokens(m)
	_, ruleText := splitSystemRules(m.Content)
	rules = agentctx.EstimateTokens(ruleText)
	if rules > full {
		rules = full
	}
	return full - rules, rules
}

func splitSystemRules(content string) (system, rules string) {
	if i := strings.Index(content, "\n\n# WORKSPACE GUIDES & RULES\n"); i >= 0 {
		return content[:i], content[i:]
	}
	if i := strings.Index(content, "\nRules:\n"); i >= 0 {
		return content[:i], content[i:]
	}
	return content, ""
}

// BarSegment is one colored slice inside the filled portion of the usage bar.
type BarSegment struct {
	Count int
	Style int // 0 system, 1 tools, 2 rules, 3 conversation
}

// UsageBarSegments splits filled width across the four categories proportional to their counts.
func UsageBarSegments(b ContextBreakdown, width int) []BarSegment {
	if width <= 0 {
		return nil
	}
	cap := b.Capacity
	if cap <= 0 {
		cap = TokenCapacity()
	}
	used := b.Used
	if used > cap {
		used = cap
	}
	filled := (width * used) / cap
	if used > 0 && filled == 0 {
		filled = 1
	}
	if filled == 0 {
		return nil
	}
	cats := []int{b.System, b.ToolDefs, b.Rules, b.Conversation}
	total := 0
	for _, c := range cats {
		total += c
	}
	if total == 0 {
		return []BarSegment{{Count: filled, Style: 3}}
	}
	type share struct {
		style int
		count int
		rem   int
	}
	var parts []share
	alloc := 0
	for i, c := range cats {
		if c <= 0 {
			continue
		}
		seg := (filled * c) / total
		parts = append(parts, share{style: i, count: seg, rem: (filled * c) % total})
		alloc += seg
	}
	for alloc < filled && len(parts) > 0 {
		best := 0
		for i := 1; i < len(parts); i++ {
			if parts[i].rem > parts[best].rem {
				best = i
			}
		}
		parts[best].count++
		parts[best].rem = -1
		alloc++
	}
	out := make([]BarSegment, 0, len(parts))
	for _, p := range parts {
		if p.count <= 0 {
			continue
		}
		out = append(out, BarSegment{Count: p.count, Style: p.style})
	}
	if len(out) == 0 {
		return []BarSegment{{Count: filled, Style: 3}}
	}
	return out
}

// UsagePercent returns floor(100 * used / capacity).
func UsagePercent(b ContextBreakdown) int {
	if b.Capacity <= 0 {
		return 0
	}
	p := (100 * b.Used) / b.Capacity
	if p > 100 {
		return 100
	}
	return p
}
