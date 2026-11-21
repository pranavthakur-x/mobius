package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

func init() {
	RegisterProviderFactory("anthropic", func(apiKey, baseURL string) Provider {
		return NewAnthropicProvider(apiKey, baseURL)
	})
}

// AnthropicProvider calls Anthropic's Messages API.
type AnthropicProvider struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// NewAnthropicProvider creates and configures a new Anthropic Messages API provider.
func NewAnthropicProvider(apiKey, baseURL string) *AnthropicProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &AnthropicProvider{APIKey: apiKey, BaseURL: baseURL, Client: newHTTPClient()}
}

// Name returns the provider identifier.
func (p *AnthropicProvider) Name() string { return "anthropic" }

// Generate sends a non-streaming message request to Anthropic's Messages API.
func (p *AnthropicProvider) Generate(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("chat request cannot be nil")
	}
	payload, err := buildAnthropicPayload(req)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicMessagesURL(p.BaseURL), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if p.APIKey != "" {
		httpReq.Header.Set("x-api-key", p.APIKey)
	}
	resp, err := p.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request to LLM failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API returned status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read LLM response: %w", err)
	}
	var parsed anthropicResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %w", err)
	}
	return anthropicResponseToChat(&parsed), nil
}

func anthropicMessagesURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}

type anthropicPayload struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stream    bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type anthropicResponse struct {
	Content []anthropicBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicToolResult struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

type anthropicBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

func buildAnthropicPayload(req *ChatRequest) (anthropicPayload, error) {
	msgs := NormalizeMessages(req.Message)
	payload := anthropicPayload{Model: req.Model, MaxTokens: 8192}
	var system strings.Builder
	var pending []anthropicToolResult
	flushTools := func() {
		if len(pending) == 0 {
			return
		}
		payload.Messages = append(payload.Messages, anthropicMessage{Role: "user", Content: pending})
		pending = nil
	}
	for _, msg := range msgs {
		switch msg.Role {
		case RoleSystem:
			if system.Len() > 0 {
				system.WriteString("\n\n")
			}
			system.WriteString(msg.Content)
		case RoleUser:
			flushTools()
			payload.Messages = append(payload.Messages, anthropicMessage{Role: "user", Content: msg.Content})
		case RoleAssistant:
			flushTools()
			var blocks []anthropicBlock
			if strings.TrimSpace(msg.Content) != "" {
				blocks = append(blocks, anthropicBlock{Type: "text", Text: msg.Content})
			}
			for _, call := range msg.ToolCalls {
				raw := json.RawMessage(call.Function.Arguments)
				if !json.Valid(raw) {
					encoded, _ := json.Marshal(call.Function.Arguments)
					raw = encoded
				}
				blocks = append(blocks, anthropicBlock{Type: "tool_use", ID: call.ID, Name: call.Function.Name, Input: raw})
			}
			if len(blocks) == 0 {
				blocks = []anthropicBlock{{Type: "text", Text: msg.Content}}
			}
			payload.Messages = append(payload.Messages, anthropicMessage{Role: "assistant", Content: blocks})
		case RoleTool:
			pending = append(pending, anthropicToolResult{Type: "tool_result", ToolUseID: msg.ToolCallID, Content: msg.Content})
		}
	}
	flushTools()
	payload.System = system.String()
	for _, tool := range req.Tools {
		schema := tool.Function.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		payload.Tools = append(payload.Tools, anthropicTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: schema,
		})
	}
	if len(payload.Messages) == 0 {
		return payload, fmt.Errorf("anthropic request has no messages")
	}
	return payload, nil
}

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message,omitempty"`
	ContentBlock *anthropicBlock `json:"content_block,omitempty"`
	Delta        *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta,omitempty"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type anthropicToolAcc struct {
	id   string
	name string
	args strings.Builder
}

// GenerateStream sends a streaming SSE request to Anthropic's Messages API.
func (p *AnthropicProvider) GenerateStream(ctx context.Context, req *ChatRequest, onEvent StreamCallback) (*ChatResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("chat request cannot be nil")
	}
	payload, err := buildAnthropicPayload(req)
	if err != nil {
		return nil, err
	}
	payload.Stream = true
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicMessagesURL(p.BaseURL), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if p.APIKey != "" {
		httpReq.Header.Set("x-api-key", p.APIKey)
	}
	resp, err := p.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request to LLM failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API returned status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read LLM response: %w", err)
		}
		var parsed anthropicResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("failed to decode LLM response: %w", err)
		}
		out := anthropicResponseToChat(&parsed)
		emitCompletedResponse(out, onEvent)
		return out, nil
	}

	var text strings.Builder
	toolMap := make(map[int]*anthropicToolAcc)
	var usage Usage

	err = readSSE(ctx, resp.Body, func(eventName, data string) error {
		trimmed := strings.TrimSpace(data)
		if trimmed == "" {
			return nil
		}
		var ev anthropicStreamEvent
		if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
			return fmt.Errorf("failed to decode anthropic stream event: %w", err)
		}
		kind := ev.Type
		if kind == "" {
			kind = eventName
		}
		switch kind {
		case "message_start":
			if ev.Message != nil {
				usage.PromptTokens = ev.Message.Usage.InputTokens
				if ev.Message.Usage.OutputTokens > 0 {
					usage.CompletionTokens = ev.Message.Usage.OutputTokens
				}
			}
		case "content_block_start":
			if ev.ContentBlock == nil {
				return nil
			}
			switch ev.ContentBlock.Type {
			case "text":
				if text.Len() > 0 {
					text.WriteByte('\n')
					if onEvent != nil {
						onEvent(StreamEvent{TextDelta: "\n"})
					}
				}
				if ev.ContentBlock.Text != "" {
					text.WriteString(ev.ContentBlock.Text)
					if onEvent != nil {
						onEvent(StreamEvent{TextDelta: ev.ContentBlock.Text})
					}
				}
			case "tool_use":
				acc := &anthropicToolAcc{
					id:   ev.ContentBlock.ID,
					name: ev.ContentBlock.Name,
				}
				if raw := strings.TrimSpace(string(ev.ContentBlock.Input)); raw != "" && raw != "null" && raw != "{}" {
					acc.args.WriteString(raw)
				}
				toolMap[ev.Index] = acc
				if onEvent != nil {
					onEvent(StreamEvent{ToolName: acc.name, ToolID: acc.id})
				}
			}
		case "content_block_delta":
			if ev.Delta == nil {
				return nil
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					text.WriteString(ev.Delta.Text)
					if onEvent != nil {
						onEvent(StreamEvent{TextDelta: ev.Delta.Text})
					}
				}
			case "input_json_delta":
				if acc, ok := toolMap[ev.Index]; ok && ev.Delta.PartialJSON != "" {
					acc.args.WriteString(ev.Delta.PartialJSON)
				}
			}
		case "message_delta":
			if ev.Usage != nil {
				if ev.Usage.InputTokens > 0 {
					usage.PromptTokens = ev.Usage.InputTokens
				}
				if ev.Usage.OutputTokens > 0 {
					usage.CompletionTokens = ev.Usage.OutputTokens
				}
			}
		case "message_stop":
			return errSSEStop
		case "error":
			if ev.Error != nil {
				return fmt.Errorf("anthropic stream error (%s): %s", ev.Error.Type, ev.Error.Message)
			}
			return fmt.Errorf("anthropic stream error: %s", trimmed)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	indices := make([]int, 0, len(toolMap))
	for idx := range toolMap {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	toolCalls := make([]ToolCall, 0, len(indices))
	for _, idx := range indices {
		acc := toolMap[idx]
		args := strings.TrimSpace(acc.args.String())
		if args == "" {
			args = "{}"
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:   acc.id,
			Type: "function",
			Function: FunctionCall{
				Name:      acc.name,
				Arguments: args,
			},
		})
	}

	return &ChatResponse{
		Content:   text.String(),
		ToolCalls: toolCalls,
		Usage:     usage,
	}, nil
}

func (p *AnthropicProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return defaultHTTPClient
}

func anthropicResponseToChat(parsed *anthropicResponse) *ChatResponse {
	out := &ChatResponse{Usage: Usage{
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
		TotalTokens:      parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}}
	var text strings.Builder
	for _, block := range parsed.Content {
		switch block.Type {
		case "text":
			if text.Len() > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(block.Text)
		case "tool_use":
			args := string(block.Input)
			if args == "" || args == "null" {
				args = "{}"
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      block.Name,
					Arguments: args,
				},
			})
		}
	}
	out.Content = text.String()
	return out
}
