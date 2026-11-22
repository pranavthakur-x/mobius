package agent

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type AgentConfig struct {
	MaxSteps       int     `toml:"max_steps"`
	MaxCost        float64 `toml:"max_cost"`
	TimeoutSeconds int     `toml:"timeout_seconds"`
}

// DefaultAgentConfig returns standard execution limits when no config file is present.
func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		MaxSteps:       50,
		MaxCost:        2.00,
		TimeoutSeconds: 300,
	}
}

func LoadAgentConfig(path string) (*AgentConfig, error) {
	if path == "" {
		path = "config/agent.toml"
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultAgentConfig(), nil
		}
		return nil, fmt.Errorf("failed to read agent config file '%s': %w", path, err)
	}
	var fileData struct {
		Agent AgentConfig `toml:"agent"`
	}
	if err := toml.Unmarshal(content, &fileData); err != nil {
		return nil, fmt.Errorf("failed to parse agent config '%s': %w", path, err)
	}
	cfg := fileData.Agent
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 50
	}
	if cfg.MaxCost <= 0 {
		cfg.MaxCost = 2.00
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 300
	}
	return &cfg, nil
}
