package agentctx

import (
	"mobius/pkg/llm"
	"sync"
)

// ConversationContext stores the ordered, concurrency-safe message history for an agent turn loop.
type ConversationContext struct {
	mu       sync.RWMutex
	messages []llm.Message
}

// NewConversationContext initializes a ConversationContext, seeding a system message when systemPrompt is non-empty.
func NewConversationContext(systemPrompt string) *ConversationContext {
	ctx := &ConversationContext{
		messages: make([]llm.Message, 0),
	}

	if systemPrompt != "" {
		ctx.messages = append(ctx.messages, llm.Message{
			Role:    llm.RoleSystem,
			Content: systemPrompt,
		})
	}
	return ctx
}

// AddUserMessage appends a user-role message to the conversation history.
func (c *ConversationContext) AddUserMessage(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, llm.Message{
		Role:    llm.RoleUser,
		Content: content,
	})
}

// AddAssistantMessage appends an assistant-role message and a copy of its tool calls.
func (c *ConversationContext) AddAssistantMessage(content string, toolCalls []llm.ToolCall) {
	var copied []llm.ToolCall
	if toolCalls != nil {
		copied = make([]llm.ToolCall, len(toolCalls))
		copy(copied, toolCalls)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, llm.Message{
		Role:      llm.RoleAssistant,
		Content:   content,
		ToolCalls: copied,
	})
}

// AddToolResult appends the output of an executed tool.
func (c *ConversationContext) AddToolResult(toolCallID, output string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, llm.Message{
		Role:       llm.RoleTool,
		Content:    output,
		ToolCallID: toolCallID,
	})
}

// Messages returns a deep-copied snapshot of the active message slice.
func (c *ConversationContext) Messages() []llm.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]llm.Message, len(c.messages))
	for i, m := range c.messages {
		out[i] = m
		if m.ToolCalls != nil {
			tc := make([]llm.ToolCall, len(m.ToolCalls))
			copy(tc, m.ToolCalls)
			out[i].ToolCalls = tc
		}
	}
	return out
}

// SetMessages replaces the active message slice (used during context compaction).
func (c *ConversationContext) SetMessages(messages []llm.Message) {
	copied := make([]llm.Message, len(messages))
	copy(copied, messages)
	for i, m := range copied {
		if m.ToolCalls != nil {
			tc := make([]llm.ToolCall, len(m.ToolCalls))
			copy(tc, m.ToolCalls)
			copied[i].ToolCalls = tc
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = copied
}
