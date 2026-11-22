package llm

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

//go:embed builtin_config.toml
var builtinConfigBytes []byte

// ProviderConfig defines the TOML configuration block for an LLM provider.
type ProviderConfig struct {
	Name    string   `toml:"name"`     // Display name (e.g. "DeepSeek", "OpenAI")
	Type    string   `toml:"type"`     // Protocol type used for factory lookup (e.g. "openai", "anthropic")
	BaseURL string   `toml:"base_url"` // Endpoint base URL
	EnvKey  string   `toml:"env_key"`  // Environment variable containing the API key
	Models  []string `toml:"models"`   // List of model IDs supported by this provider
}

// Config represents the top-level configuration loaded from config.toml.
type Config struct {
	mu          sync.RWMutex
	ActiveModel string                    `toml:"default_model"`
	Providers   map[string]ProviderConfig `toml:"providers"`
	Pricing     struct {
		Prompt     map[string]float64 `toml:"prompt"`
		Completion map[string]float64 `toml:"completion"`
	} `toml:"pricing"`

	manual *Connection
	saved  []Connection
}

// ModelInfo describes a model and its backing provider endpoint.
type ModelInfo struct {
	ProviderName string
	Model        string
	BaseURL      string
}

// GetPrices returns the configured prompt and completion prices per unit for modelName.
func (c *Config) GetPrices(modelName string) (float64, float64) {
	if c == nil {
		return 0, 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if modelName == "" {
		modelName = c.ActiveModel
	}
	if c.manual != nil && c.manual.Model == modelName {
		if !c.manual.HasPrice {
			return 0, 0
		}
		return c.manual.PromptPrice, c.manual.CompletionPrice
	}
	return c.Pricing.Prompt[modelName], c.Pricing.Completion[modelName]
}

// PricesKnown reports whether a price was entered or configured for the model.
// A missing price is not the same as a cost of zero.
func (c *Config) PricesKnown(modelName string) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pricesKnown(modelName)
}

func (c *Config) pricesKnown(modelName string) bool {
	if modelName == "" {
		modelName = c.ActiveModel
	}
	if c.manual != nil && c.manual.Model == modelName {
		return c.manual.HasPrice
	}
	_, prompt := c.Pricing.Prompt[modelName]
	_, completion := c.Pricing.Completion[modelName]
	return prompt || completion
}

// LoadConfigBytes parses TOML bytes into a Config.
func LoadConfigBytes(data []byte) (*Config, error) {
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse TOML config: %w", err)
	}
	return &cfg, nil
}

// LoadBuiltinConfig parses the embedded configuration.
func LoadBuiltinConfig() (*Config, error) {
	return LoadConfigBytes(builtinConfigBytes)
}

// LoadConfigWithFallback attempts to load a TOML configuration from:
// 1. specified path (if provided and exists)
// 2. config/model_config.toml
// 3. model_config.toml
// 4. ~/.mobius/model_config.toml
// 5. Embedded builtin configuration
func LoadConfigWithFallback(path string) (*Config, error) {
	candidates := make([]string, 0, 4)
	if strings.TrimSpace(path) != "" {
		candidates = append(candidates, strings.TrimSpace(path))
	}
	candidates = append(candidates, "config/model_config.toml", "model_config.toml")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".mobius", "model_config.toml"))
		candidates = append(candidates, filepath.Join(home, ".mobius", "config.toml"))
	}

	for _, cand := range candidates {
		cfg, err := LoadConfig(cand)
		if err == nil {
			return cfg, nil
		}
	}

	return LoadBuiltinConfig()
}

// AutoSelectModel inspects available API keys in the environment. If the current
// active model lacks an API key, it switches to a model whose provider key is set.
func (c *Config) AutoSelectModel() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.hasKeyForModelLocked(c.ActiveModel) {
		return
	}

	type candidate struct {
		envVar string
		model  string
	}
	candidates := []candidate{
		{"ANTHROPIC_API_KEY", "claude-3-5-sonnet-20241022"},
		{"OPENAI_API_KEY", "gpt-4o"},
		{"GEMINI_API_KEY", "gemini-2.5-flash"},
		{"DEEPSEEK_API_KEY", "deepseek-chat"},
		{"OPENROUTER_API_KEY", "deepseek/deepseek-v4.1-flash"},
	}

	for _, cand := range candidates {
		if val := strings.TrimSpace(os.Getenv(cand.envVar)); val != "" {
			c.ActiveModel = cand.model
			return
		}
	}
}

func (c *Config) hasKeyForModelLocked(modelName string) bool {
	if _, ok := c.matchSaved(modelName); ok {
		return true
	}
	for _, prov := range c.Providers {
		for _, m := range prov.Models {
			if m == modelName {
				if prov.EnvKey == "" {
					return true // e.g. local Ollama
				}
				return strings.TrimSpace(os.Getenv(prov.EnvKey)) != ""
			}
		}
	}
	return false
}

// LoadConfig reads and unmarshals a TOML configuration file from the specified path.
func LoadConfig(path string) (*Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("config.toml path is required")
	}

	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current directory: %w", err)
		}
		path = filepath.Join(cwd, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file at %s: %w", path, err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse TOML config: %w", err)
	}
	return &cfg, nil
}

// ListAllModels extracts and returns a flat list of all model names across all configured providers.
func (c *Config) ListAllModels() (map[string]*ModelInfo, error) {
	if c == nil {
		return nil, fmt.Errorf("config is nil")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	allModels := make(map[string]*ModelInfo)
	for _, provider := range c.Providers {
		models := provider.Models
		for _, model := range models {
			allModels[model] = &ModelInfo{
				ProviderName: provider.Name,
				Model:        model,
				BaseURL:      provider.BaseURL,
			}
		}
	}
	for _, conn := range c.saved {
		if conn.Model == "" {
			continue
		}
		name := "Custom"
		if conn.Protocol != "" {
			name = conn.Protocol
		}
		allModels[conn.Model] = &ModelInfo{ProviderName: name, Model: conn.Model, BaseURL: conn.BaseURL}
	}

	if len(allModels) == 0 {
		return nil, fmt.Errorf("no models found in config")
	}

	return allModels, nil
}

// GetProviderForModel resolves the model name to its provider configuration and instantiates the Provider via factory.
func (c *Config) GetProviderForModel(modelName string) (Provider, error) {
	if c == nil {
		return nil, fmt.Errorf("config is nil")
	}
	c.mu.RLock()
	if modelName == "" {
		modelName = c.ActiveModel
	}
	if conn, ok := c.matchSaved(modelName); ok {
		key, base, protocol := conn.APIKey, conn.BaseURL, conn.Protocol
		c.mu.RUnlock()
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("api key for %s is not in the system keystore", conn.Model)
		}
		if protocol == "" {
			protocol = "openai"
		}
		factory, ok := providerFactories[protocol]
		if !ok {
			return nil, fmt.Errorf("unsupported API protocol %q for %s", protocol, base)
		}
		return factory(key, base), nil
	}
	var (
		found  bool
		prov   ProviderConfig
		apiKey string
	)
	for _, candidate := range c.Providers {
		for _, m := range candidate.Models {
			if modelName == m {
				found = true
				prov = candidate
				apiKey = os.Getenv(candidate.EnvKey)
			}
		}
	}
	c.mu.RUnlock()
	if !found {
		return nil, fmt.Errorf("no provider configured for model %q", modelName)
	}
	factory, ok := providerFactories[prov.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported provider type: %s", prov.Type)
	}
	return factory(apiKey, prov.BaseURL), nil
}
