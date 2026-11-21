package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPClientHardenedDefaults(t *testing.T) {
	c := newHTTPClient()
	if c == nil {
		t.Fatal("newHTTPClient returned nil")
	}
	if c.Timeout != 0 {
		t.Fatalf("Client.Timeout = %v, want 0 for SSE streaming", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", c.Transport)
	}
	if tr.DialContext == nil {
		t.Fatal("Transport.DialContext must be configured")
	}
	if tr.TLSHandshakeTimeout != 10*time.Second {
		t.Fatalf("TLSHandshakeTimeout = %v, want 10s", tr.TLSHandshakeTimeout)
	}
	if tr.ExpectContinueTimeout != 1*time.Second {
		t.Fatalf("ExpectContinueTimeout = %v, want 1s", tr.ExpectContinueTimeout)
	}
	if tr.IdleConnTimeout != 90*time.Second {
		t.Fatalf("IdleConnTimeout = %v, want 90s", tr.IdleConnTimeout)
	}
	if tr.MaxIdleConns != 32 {
		t.Fatalf("MaxIdleConns = %d, want 32", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 8 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 8", tr.MaxIdleConnsPerHost)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2 = false, want true")
	}
}

func TestProvidersUseHardenedHTTPClient(t *testing.T) {
	clients := map[string]*http.Client{
		"openai":    NewOpenAIProvider("k", "").Client,
		"anthropic": NewAnthropicProvider("k", "").Client,
		"gemini":    NewGeminiProvider("k", "").Client,
	}
	for name, c := range clients {
		if c == nil {
			t.Fatalf("%s provider Client is nil", name)
		}
		if c.Timeout != 0 {
			t.Fatalf("%s provider Client.Timeout = %v, want 0", name, c.Timeout)
		}
		if _, ok := c.Transport.(*http.Transport); !ok {
			t.Fatalf("%s provider Transport = %T, want *http.Transport", name, c.Transport)
		}
	}
}

func TestReadErrorBodyBoundedAndTrimmed(t *testing.T) {
	if got := readErrorBody(nil); got != "" {
		t.Fatalf("readErrorBody(nil) = %q, want empty", got)
	}
	if got := readErrorBody(strings.NewReader("  bad request \n")); got != "bad request" {
		t.Fatalf("readErrorBody trimmed = %q, want %q", got, "bad request")
	}
	huge := strings.Repeat("x", maxErrorBodyBytes+4096)
	got := readErrorBody(strings.NewReader(huge))
	if len(got) != maxErrorBodyBytes {
		t.Fatalf("len(readErrorBody(huge)) = %d, want %d", len(got), maxErrorBodyBytes)
	}
}

func TestProvidersBoundErrorBodiesOnNon200(t *testing.T) {
	hugeErr := strings.Repeat("E", maxErrorBodyBytes+2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(hugeErr))
	}))
	defer srv.Close()

	req := &ChatRequest{
		Model:   "m",
		Message: []Message{{Role: RoleUser, Content: "hi"}},
	}
	providers := []Provider{
		NewOpenAIProvider("k", srv.URL),
		NewAnthropicProvider("k", srv.URL),
		NewGeminiProvider("k", srv.URL),
	}
	for _, p := range providers {
		_, err := p.Generate(context.Background(), req)
		if err == nil {
			t.Fatalf("%s Generate expected error", p.Name())
		}
		if len(err.Error()) > maxErrorBodyBytes+128 {
			t.Fatalf("%s Generate error body was not bounded: len=%d", p.Name(), len(err.Error()))
		}
	}
}
