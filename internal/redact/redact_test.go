package redact

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRedactSecrets(t *testing.T) {
	openAI := "sk-proj-" + strings.Repeat("a", 24)
	anthropic := "sk-ant-api03-" + strings.Repeat("b", 24)
	google := "AIza" + strings.Repeat("c", 28)
	aws := "AKIAIOSFODNN7EXAMPLE"
	privateKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nSUPERSECRETLINE\n-----END OPENSSH PRIVATE KEY-----"
	bearer := "Bearer " + strings.Repeat("d", 32)

	tests := []struct {
		name    string
		input   string
		secret  string
		wantHas string
	}{
		{name: "openai", input: "token is " + openAI + " please", secret: openAI, wantHas: "token is " + Placeholder + " please"},
		{name: "anthropic", input: "key " + anthropic, secret: anthropic, wantHas: "key " + Placeholder},
		{name: "google", input: google, secret: google, wantHas: Placeholder},
		{name: "aws", input: "id " + aws, secret: aws, wantHas: "id " + Placeholder},
		{name: "bearer", input: "Authorization: " + bearer, secret: strings.TrimPrefix(bearer, "Bearer "), wantHas: "Authorization: " + Placeholder},
		{name: "private key", input: "pem\n" + privateKey + "\nend", secret: "SUPERSECRETLINE", wantHas: "pem\n" + Placeholder + "\nend"},
		{name: "api key assignment", input: `OPENAI_API_KEY="hunter2"`, secret: "hunter2", wantHas: "OPENAI_API_KEY=" + Placeholder},
		{name: "password assignment", input: "password: hunter2", secret: "hunter2", wantHas: "password: " + Placeholder},
		{name: "quoted token", input: "GITHUB_TOKEN = 'ghp_example'", secret: "ghp_example", wantHas: "GITHUB_TOKEN = " + Placeholder},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.input)
			if strings.Contains(got, tt.secret) {
				t.Fatal("redaction left a secret in the output")
			}
			if got != tt.wantHas {
				t.Fatalf("redacted form = %q", got)
			}
		})
	}
}

func TestRedactAssignments(t *testing.T) {
	const hidden = "hunter2"
	const second = "second"
	tests := []struct {
		name   string
		input  string
		secret string
		want   string
	}{
		{
			name:   "json quoted key",
			input:  `{"SERVICE_PASSWORD":"` + hidden + `"}`,
			secret: hidden,
			want:   `{"SERVICE_PASSWORD":` + Placeholder + `}`,
		},
		{
			name:   "rest of line",
			input:  "API_KEY: first " + second,
			secret: second,
			want:   "API_KEY: " + Placeholder,
		},
		{
			name:   "non secret preserves later assignment",
			input:  "notes = hello API_KEY=" + hidden,
			secret: hidden,
			want:   "notes = hello API_KEY=" + Placeholder,
		},
		{
			name:   "double quoted key",
			input:  `"OPENAI_API_KEY"=` + hidden,
			secret: hidden,
			want:   `"OPENAI_API_KEY"=` + Placeholder,
		},
		{
			name:   "single quoted yaml key",
			input:  `'SERVICE_PASSWORD': ` + hidden,
			secret: hidden,
			want:   `'SERVICE_PASSWORD': ` + Placeholder,
		},
		{
			name:   "session token suffix",
			input:  "SESSIONTOKEN=" + hidden,
			secret: hidden,
			want:   "SESSIONTOKEN=" + Placeholder,
		},
		{
			name:   "max tokens not secret key",
			input:  "max_tokens: 128",
			secret: hidden,
			want:   "max_tokens: 128",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.input)
			if strings.Contains(got, tt.secret) {
				t.Fatal("redaction left a secret in the output")
			}
			if got != tt.want {
				t.Fatalf("redacted form = %q", got)
			}
		})
	}
}

func TestRedactLeavesOrdinaryText(t *testing.T) {
	samples := []string{
		"",
		"sketch the design",
		"Ask about the token budget",
		"max_tokens: 128",
		"the word secret appears in prose",
		"notes = hello",
	}
	for _, sample := range samples {
		if got := Redact(sample); got != sample {
			t.Fatalf("changed ordinary text %q", sample)
		}
	}
}

func TestRedactConcurrent(t *testing.T) {
	secret := "sk-proj-" + strings.Repeat("e", 24)
	input := "prefix " + secret + " OPENAI_API_KEY=hunter2 suffix"
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	var failed atomic.Bool
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				got := Redact(input)
				if strings.Contains(got, secret) || strings.Contains(got, "hunter2") || !strings.Contains(got, Placeholder) {
					failed.Store(true)
					cancel()
					return
				}
			}
		}()
	}
	wg.Wait()
	if failed.Load() {
		t.Fatal("concurrent redaction leaked a secret or dropped the placeholder")
	}
}
