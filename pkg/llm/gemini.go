package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func init() {
	RegisterProviderFactory("gemini", func(apiKey, baseURL string) Provider {
		return NewGeminiProvider(apiKey, baseURL)
	})
}

// GeminiProvider calls Google's Gemini generateContent / streamGenerateContent APIs.
type GeminiProvider struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

// NewGeminiProvider creates and configures a new Google Gemini API provider.
func NewGeminiProvider(apiKey, baseURL string) *GeminiProvider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &GeminiProvider{
		APIKey:  apiKey,
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Client:  newHTTPClient(),
	}
}

// Name returns the provider identifier.
func (p *GeminiProvider) Name() string { return "gemini" }

// Generate sends a non-streaming generateContent request to Google's Gemini API.
func (p *GeminiProvider) Generate(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("chat request cannot be nil")
	}
	payload, err := buildGeminiPayload(req)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiEndpointURL(p.BaseURL, req.Model, false), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("x-goog-api-key", p.APIKey)
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

	var parsed geminiResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode LLM response: %w", err)
	}
	return parseGeminiSingle(&parsed), nil
}

// GenerateStream streams tokens and tool calls from Gemini's SSE endpoint.
func (p *GeminiProvider) GenerateStream(ctx context.Context, req *ChatRequest, onEvent StreamCallback) (*ChatResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("chat request cannot be nil")
	}
	payload, err := buildGeminiPayload(req)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiEndpointURL(p.BaseURL, req.Model, true), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.APIKey != "" {
		httpReq.Header.Set("x-goog-api-key", p.APIKey)
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
		var parsed geminiResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("failed to decode LLM response: %w", err)
		}
		out := parseGeminiSingle(&parsed)
		emitCompletedResponse(out, onEvent)
		return out, nil
	}

	var text strings.Builder
	var toolCalls []ToolCall
	var usage Usage

	err = readSSE(ctx, resp.Body, func(_, data string) error {
		trimmed := strings.TrimSpace(data)
		if trimmed == "" || trimmed == "[DONE]" {
			return nil
		}
		var chunk geminiResponse
		if err := json.Unmarshal([]byte(trimmed), &chunk); err != nil {
			return fmt.Errorf("failed to decode gemini stream chunk: %w", err)
		}
		if chunk.UsageMetadata != nil {
			usage.PromptTokens = chunk.UsageMetadata.PromptTokenCount
			usage.CompletionTokens = chunk.UsageMetadata.CandidatesTokenCount
			usage.TotalTokens = chunk.UsageMetadata.TotalTokenCount
			if usage.TotalTokens == 0 {
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}
		}
		if len(chunk.Candidates) == 0 {
			return nil
		}
		for _, part := range chunk.Candidates[0].Content.Parts {
			if part.Text != "" {
				text.WriteString(part.Text)
				if onEvent != nil {
					onEvent(StreamEvent{TextDelta: part.Text})
				}
			}
			if part.FunctionCall != nil {
				tc := geminiCallToToolCall(part.FunctionCall, len(toolCalls))
				toolCalls = append(toolCalls, tc)
				if onEvent != nil {
					onEvent(StreamEvent{ToolName: tc.Function.Name, ToolID: tc.ID})
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &ChatResponse{
		Content:   text.String(),
		ToolCalls: toolCalls,
		Usage:     usage,
	}, nil
}

func (p *GeminiProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return defaultHTTPClient
}

func geminiEndpointURL(base, model string, stream bool) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta"
	}
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	if stream {
		return fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", base, model)
	}
	return fmt.Sprintf("%s/models/%s:generateContent", base, model)
}

type geminiPayload struct {
	SystemInstruction *geminiContent     `json:"systemInstruction,omitempty"`
	Contents          []geminiContent    `json:"contents"`
	Tools             []geminiToolBundle `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiToolBundle struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations"`
}

type geminiFunctionDecl struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func buildGeminiPayload(req *ChatRequest) (geminiPayload, error) {
	msgs := NormalizeMessages(req.Message)
	var payload geminiPayload
	var sysParts []string
	var pendingToolResp []geminiPart
	callNames := make(map[string]string)

	flushToolResps := func() {
		if len(pendingToolResp) == 0 {
			return
		}
		payload.Contents = append(payload.Contents, geminiContent{
			Role:  "user",
			Parts: pendingToolResp,
		})
		pendingToolResp = nil
	}

	for _, msg := range msgs {
		switch msg.Role {
		case RoleSystem:
			if strings.TrimSpace(msg.Content) != "" {
				sysParts = append(sysParts, msg.Content)
			}
		case RoleUser:
			flushToolResps()
			payload.Contents = append(payload.Contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: msg.Content}},
			})
		case RoleAssistant:
			flushToolResps()
			var parts []geminiPart
			if strings.TrimSpace(msg.Content) != "" {
				parts = append(parts, geminiPart{Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					callNames[tc.ID] = tc.Function.Name
				}
				args := map[string]any{}
				if strings.TrimSpace(tc.Function.Arguments) != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				}
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFunctionCall{
						ID:   tc.ID,
						Name: tc.Function.Name,
						Args: args,
					},
				})
			}
			if len(parts) == 0 {
				parts = []geminiPart{{Text: msg.Content}}
			}
			payload.Contents = append(payload.Contents, geminiContent{
				Role:  "model",
				Parts: parts,
			})
		case RoleTool:
			name := callNames[msg.ToolCallID]
			if name == "" {
				name = msg.ToolCallID
			}
			if name == "" {
				name = "tool"
			}
			pendingToolResp = append(pendingToolResp, geminiPart{
				FunctionResponse: &geminiFunctionResponse{
					ID:       msg.ToolCallID,
					Name:     name,
					Response: map[string]any{"output": msg.Content},
				},
			})
		}
	}
	flushToolResps()

	if len(sysParts) > 0 {
		payload.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: strings.Join(sysParts, "\n\n")}},
		}
	}
	if len(req.Tools) > 0 {
		decls := make([]geminiFunctionDecl, 0, len(req.Tools))
		for _, t := range req.Tools {
			decls = append(decls, geminiFunctionDecl{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			})
		}
		payload.Tools = []geminiToolBundle{{FunctionDeclarations: decls}}
	}
	if len(payload.Contents) == 0 {
		return payload, fmt.Errorf("gemini request has no messages")
	}
	return payload, nil
}

func parseGeminiSingle(parsed *geminiResponse) *ChatResponse {
	out := &ChatResponse{}
	if parsed.UsageMetadata != nil {
		out.Usage = Usage{
			PromptTokens:     parsed.UsageMetadata.PromptTokenCount,
			CompletionTokens: parsed.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      parsed.UsageMetadata.TotalTokenCount,
		}
		if out.Usage.TotalTokens == 0 {
			out.Usage.TotalTokens = out.Usage.PromptTokens + out.Usage.CompletionTokens
		}
	}
	if len(parsed.Candidates) == 0 {
		return out
	}
	var text strings.Builder
	for _, part := range parsed.Candidates[0].Content.Parts {
		if part.Text != "" {
			text.WriteString(part.Text)
		}
		if part.FunctionCall != nil {
			out.ToolCalls = append(out.ToolCalls, geminiCallToToolCall(part.FunctionCall, len(out.ToolCalls)))
		}
	}
	out.Content = text.String()
	return out
}

func geminiCallToToolCall(fc *geminiFunctionCall, idx int) ToolCall {
	id := fc.ID
	if id == "" {
		id = fmt.Sprintf("call_%d", idx)
	}
	argsBytes, err := json.Marshal(fc.Args)
	args := "{}"
	if err == nil && len(argsBytes) > 0 && string(argsBytes) != "null" {
		args = string(argsBytes)
	}
	return ToolCall{
		ID:   id,
		Type: "function",
		Function: FunctionCall{
			Name:      fc.Name,
			Arguments: args,
		},
	}
}

func emitCompletedResponse(out *ChatResponse, onEvent StreamCallback) {
	if onEvent == nil || out == nil {
		return
	}
	if out.Content != "" {
		onEvent(StreamEvent{TextDelta: out.Content})
	}
	for _, tc := range out.ToolCalls {
		onEvent(StreamEvent{ToolName: tc.Function.Name, ToolID: tc.ID})
	}
}
