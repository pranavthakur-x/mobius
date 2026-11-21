package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOpenAIStreamTextToolCallsAndUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode req: %v", err)
		}
		if stream, _ := req["stream"].(bool); !stream {
			t.Errorf("expected stream=true, got %v", req["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			": OPENROUTER PROCESSING\n\n",
			"data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\n",
			"data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n",
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"view_file\",\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n",
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"a.go\\\"}\"}}]}}]}\n\n",
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":8,\"total_tokens\":20}}\n\n",
			"data: [DONE]\n\n",
		}
		for _, c := range chunks {
			_, _ = fmt.Fprint(w, c)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	var deltas []string
	var startedTools []string
	resp, err := p.GenerateStream(context.Background(), &ChatRequest{
		Model:   "test-model",
		Message: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(ev StreamEvent) {
		if ev.TextDelta != "" {
			deltas = append(deltas, ev.TextDelta)
		}
		if ev.ToolName != "" {
			startedTools = append(startedTools, ev.ToolName+":"+ev.ToolID)
		}
	})
	if err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	if resp.Content != "Hello world" {
		t.Fatalf("content = %q", resp.Content)
	}
	if strings.Join(deltas, "|") != "Hello |world" {
		t.Fatalf("deltas = %v", deltas)
	}
	if strings.Join(startedTools, ",") != "view_file:call_1" {
		t.Fatalf("startedTools = %v", startedTools)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "view_file" || resp.ToolCalls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Fatalf("toolCalls = %+v", resp.ToolCalls)
	}
	if resp.Usage.PromptTokens != 12 || resp.Usage.CompletionTokens != 8 || resp.Usage.TotalTokens != 20 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestOpenAIStreamRetriesWithoutStreamOptions(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if n == 1 {
			if _, has := req["stream_options"]; !has {
				t.Errorf("expected stream_options on first call")
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":"unsupported field stream_options"}`)
			return
		}
		if _, has := req["stream_options"]; has {
			t.Errorf("retry should omit stream_options")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	resp, err := p.GenerateStream(context.Background(), &ChatRequest{
		Model:   "local",
		Message: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(StreamEvent) {})
	if err != nil {
		t.Fatalf("GenerateStream retry: %v", err)
	}
	if resp.Content != "ok" || calls.Load() != 2 {
		t.Fatalf("content=%q calls=%d", resp.Content, calls.Load())
	}
}

func TestAnthropicStreamTextToolCallsAndUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-ant" {
			t.Errorf("missing anthropic api key header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":30,\"output_tokens\":1}}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Checked \"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"it.\"}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"grep_search\",\"input\":{}}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"pattern\\\":\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"TODO\\\"}\"}}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":15}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, f := range frames {
			_, _ = fmt.Fprint(w, f)
		}
	}))
	defer srv.Close()

	p := NewAnthropicProvider("sk-ant", srv.URL)
	var deltas []string
	var tools []string
	resp, err := p.GenerateStream(context.Background(), &ChatRequest{
		Model:   "claude-3-5-sonnet-20241022",
		Message: []Message{{Role: RoleSystem, Content: "sys"}, {Role: RoleUser, Content: "check"}},
	}, func(ev StreamEvent) {
		if ev.TextDelta != "" {
			deltas = append(deltas, ev.TextDelta)
		}
		if ev.ToolName != "" {
			tools = append(tools, ev.ToolName+":"+ev.ToolID)
		}
	})
	if err != nil {
		t.Fatalf("Anthropic GenerateStream: %v", err)
	}
	if resp.Content != "Checked it." {
		t.Fatalf("content = %q", resp.Content)
	}
	if strings.Join(deltas, "|") != "Checked |it." {
		t.Fatalf("deltas = %v", deltas)
	}
	if strings.Join(tools, ",") != "grep_search:toolu_1" {
		t.Fatalf("tools = %v", tools)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Arguments != `{"pattern":"TODO"}` {
		t.Fatalf("toolCalls = %+v", resp.ToolCalls)
	}
	if resp.Usage.PromptTokens != 30 || resp.Usage.CompletionTokens != 15 || resp.Usage.TotalTokens != 45 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestAnthropicStreamErrorEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"overloaded\"}}\n\n")
	}))
	defer srv.Close()

	p := NewAnthropicProvider("sk-ant", srv.URL)
	_, err := p.GenerateStream(context.Background(), &ChatRequest{
		Model:   "claude",
		Message: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(StreamEvent) {})
	if err == nil || !strings.Contains(err.Error(), "overloaded") {
		t.Fatalf("expected overloaded error, got %v", err)
	}
}

func TestGeminiGenerateAndStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "gem-key" {
			t.Errorf("missing x-goog-api-key header")
		}
		if strings.Contains(r.URL.RawQuery, "gem-key") {
			t.Errorf("api key must not appear in URL query")
		}
		if strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
			if r.URL.Query().Get("alt") != "sse" {
				t.Errorf("expected alt=sse, got %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Part 1 \"}]}}]}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Part 2\"},{\"functionCall\":{\"name\":\"list_dir\",\"args\":{\"path\":\".\"}}}]}}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":6,\"totalTokenCount\":16}}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"sync ok"}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`)
	}))
	defer srv.Close()

	p := NewGeminiProvider("gem-key", srv.URL)
	req := &ChatRequest{
		Model: "gemini-2.5-flash",
		Message: []Message{
			{Role: RoleSystem, Content: "be terse"},
			{Role: RoleUser, Content: "list files"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_prev", Type: "function", Function: FunctionCall{Name: "list_dir", Arguments: `{"path":"."}`}}}},
			{Role: RoleTool, ToolCallID: "call_prev", Content: "a.go"},
		},
		Tools: []ToolDefinition{{Type: "function", Function: FunctionDef{Name: "list_dir", Description: "list"}}},
	}

	syncResp, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Gemini Generate: %v", err)
	}
	if syncResp.Content != "sync ok" || syncResp.Usage.TotalTokens != 7 {
		t.Fatalf("syncResp = %+v", syncResp)
	}

	var deltas []string
	var tools []string
	streamResp, err := GenerateWithStream(context.Background(), p, req, func(ev StreamEvent) {
		if ev.TextDelta != "" {
			deltas = append(deltas, ev.TextDelta)
		}
		if ev.ToolName != "" {
			tools = append(tools, ev.ToolName)
		}
	})
	if err != nil {
		t.Fatalf("Gemini GenerateStream: %v", err)
	}
	if streamResp.Content != "Part 1 Part 2" {
		t.Fatalf("content = %q", streamResp.Content)
	}
	if strings.Join(deltas, "|") != "Part 1 |Part 2" {
		t.Fatalf("deltas = %v", deltas)
	}
	if strings.Join(tools, ",") != "list_dir" || len(streamResp.ToolCalls) != 1 {
		t.Fatalf("tools=%v toolCalls=%+v", tools, streamResp.ToolCalls)
	}
	if streamResp.Usage.PromptTokens != 10 || streamResp.Usage.CompletionTokens != 6 || streamResp.Usage.TotalTokens != 16 {
		t.Fatalf("usage = %+v", streamResp.Usage)
	}
	if ProtocolForURL("https://generativelanguage.googleapis.com/v1beta") != "gemini" {
		t.Fatal("ProtocolForURL should detect gemini")
	}
}

func TestStreamHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := NewOpenAIProvider("sk-test", srv.URL)
	_, err := p.GenerateStream(ctx, &ChatRequest{
		Model:   "m",
		Message: []Message{{Role: RoleUser, Content: "hi"}},
	}, func(ev StreamEvent) {
		if ev.TextDelta == "first" {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}
