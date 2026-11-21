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
	RegisterProviderFactory("openai", func(apiKey, baseURL string) Provider {
		return NewOpenAIProvider(apiKey, baseURL)
	})
}

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs.
type OpenAIProvider struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// Internal response unmarshaling structs
type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

// Internal response unmarshaling structs
type openAIMessage struct {
	Content          string           `json:"content"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"` // <-- We capture the raw JSON here
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// NewOpenAIProvider creates and configures a new OpenAI-compatible provider.
func NewOpenAIProvider(apiKey, baseURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	return &OpenAIProvider{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Client:  newHTTPClient(),
	}
}

// Name returns the provider identifier.
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Generate sends a chat completion request to the OpenAI-compatible endpoint.
func (p *OpenAIProvider) Generate(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("chat request cannot be nil")
	}

	payload := *req
	payload.Message = NormalizeMessages(req.Message)

	body, err := json.Marshal(&payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := strings.TrimSuffix(p.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request to LLM failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API returned status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	return decodeOpenAISingle(resp.Body)
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIStreamRequest struct {
	Model         string               `json:"model"`
	Message       []Message            `json:"messages"`
	Tools         []ToolDefinition     `json:"tools,omitempty"`
	Stream        bool                 `json:"stream"`
	StreamOptions *openAIStreamOptions `json:"stream_options,omitempty"`
}

type openAIStreamDeltaToolCall struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string                      `json:"content"`
			ToolCalls []openAIStreamDeltaToolCall `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

type openAIToolAcc struct {
	id       string
	typ      string
	name     strings.Builder
	args     strings.Builder
	notified bool
}

// GenerateStream sends a streaming SSE chat completion request to any OpenAI-compatible API
// (OpenAI, DeepSeek, OpenRouter, Ollama, vLLM, Groq, etc.).
func (p *OpenAIProvider) GenerateStream(ctx context.Context, req *ChatRequest, onEvent StreamCallback) (*ChatResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("chat request cannot be nil")
	}
	resp, err := p.openStreamRequest(ctx, req, true)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusBadRequest {
		errBody := readErrorBody(resp.Body)
		resp.Body.Close()
		if strings.Contains(strings.ToLower(errBody), "stream_options") {
			resp, err = p.openStreamRequest(ctx, req, false)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("LLM API returned status %d: %s", resp.StatusCode, errBody)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API returned status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		out, err := decodeOpenAISingle(resp.Body)
		if err != nil {
			return nil, err
		}
		emitCompletedResponse(out, onEvent)
		return out, nil
	}

	var text strings.Builder
	toolMap := make(map[int]*openAIToolAcc)
	var usage Usage

	err = readSSE(ctx, resp.Body, func(_, data string) error {
		trimmed := strings.TrimSpace(data)
		if trimmed == "" {
			return nil
		}
		if trimmed == "[DONE]" {
			return errSSEStop
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(trimmed), &chunk); err != nil {
			return fmt.Errorf("failed to decode LLM stream chunk: %w", err)
		}
		if chunk.Usage != nil {
			usage = Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
			if usage.TotalTokens == 0 {
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}
		}
		if len(chunk.Choices) == 0 {
			return nil
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			text.WriteString(delta.Content)
			if onEvent != nil {
				onEvent(StreamEvent{TextDelta: delta.Content})
			}
		}
		for pos, dtc := range delta.ToolCalls {
			idx := pos
			if dtc.Index != nil {
				idx = *dtc.Index
			}
			acc, ok := toolMap[idx]
			if !ok {
				acc = &openAIToolAcc{typ: "function"}
				toolMap[idx] = acc
			}
			if dtc.ID != "" {
				acc.id = dtc.ID
			}
			if dtc.Type != "" {
				acc.typ = dtc.Type
			}
			if dtc.Function.Name != "" {
				acc.name.WriteString(dtc.Function.Name)
			}
			if dtc.Function.Arguments != "" {
				acc.args.WriteString(dtc.Function.Arguments)
			}
			if !acc.notified && acc.name.Len() > 0 {
				acc.notified = true
				if onEvent != nil {
					onEvent(StreamEvent{ToolName: acc.name.String(), ToolID: acc.id})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

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
		typ := acc.typ
		if typ == "" {
			typ = "function"
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:   acc.id,
			Type: typ,
			Function: FunctionCall{
				Name:      acc.name.String(),
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

func (p *OpenAIProvider) openStreamRequest(ctx context.Context, req *ChatRequest, includeUsage bool) (*http.Response, error) {
	payload := openAIStreamRequest{
		Model:   req.Model,
		Message: NormalizeMessages(req.Message),
		Tools:   req.Tools,
		Stream:  true,
	}
	if includeUsage {
		payload.StreamOptions = &openAIStreamOptions{IncludeUsage: true}
	}
	body, err := json.Marshal(&payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	url := strings.TrimSuffix(p.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, err := p.client().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request to LLM failed: %w", err)
	}
	return resp, nil
}

func (p *OpenAIProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return defaultHTTPClient
}

func decodeOpenAISingle(r io.Reader) (*ChatResponse, error) {
	var result openAIResponse
	if err := json.NewDecoder(r).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %w", err)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no response choices returned from LLM")
	}
	msg := result.Choices[0].Message
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		content = strings.TrimSpace(msg.ReasoningContent)
	}
	var toolCalls []ToolCall
	for _, tc := range msg.ToolCalls {
		toolCalls = append(toolCalls, ToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return &ChatResponse{
		Content:   content,
		ToolCalls: toolCalls,
		Usage: Usage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		},
	}, nil
}
