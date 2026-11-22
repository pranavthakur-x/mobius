package llm

import (
	"os"
	"testing"
)

func TestLoadBuiltinConfig(t *testing.T) {
	cfg, err := LoadBuiltinConfig()
	if err != nil {
		t.Fatalf("LoadBuiltinConfig failed: %v", err)
	}
	if len(cfg.Providers) == 0 {
		t.Fatal("expected providers in builtin config")
	}
	if cfg.ActiveModel == "" {
		t.Fatal("expected default model in builtin config")
	}
}

func TestLoadConfigWithFallback_Nonexistent(t *testing.T) {
	cfg, err := LoadConfigWithFallback("/nonexistent/file.toml")
	if err != nil {
		t.Fatalf("LoadConfigWithFallback should succeed via builtin: %v", err)
	}
	if cfg.ActiveModel == "" {
		t.Fatal("expected active model")
	}
}

func TestAutoSelectModel_Anthropic(t *testing.T) {
	origAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	origOpenRouter := os.Getenv("OPENROUTER_API_KEY")
	defer func() {
		_ = os.Setenv("ANTHROPIC_API_KEY", origAnthropic)
		_ = os.Setenv("OPENROUTER_API_KEY", origOpenRouter)
	}()

	_ = os.Unsetenv("OPENROUTER_API_KEY")
	_ = os.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	cfg, err := LoadBuiltinConfig()
	if err != nil {
		t.Fatalf("LoadBuiltinConfig failed: %v", err)
	}
	cfg.AutoSelectModel()

	if cfg.ActiveModel != "claude-3-5-sonnet-20241022" {
		t.Fatalf("expected claude-3-5-sonnet-20241022, got %s", cfg.ActiveModel)
	}
}
