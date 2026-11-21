package tui

import (
	"os"

	"github.com/pelletier/go-toml/v2"
)

// Settings controls transcript and layout visibility from config/tui.toml.
type Settings struct {
	ShowSteps   bool `toml:"show_steps"`
	ShowTrace   bool `toml:"show_trace"`
	ShowContext bool `toml:"show_context"`
}

// DefaultSettings returns all flags enabled; omitted show_context in TOML stays true after merge.
func DefaultSettings() Settings {
	return Settings{
		ShowSteps:   true,
		ShowTrace:   true,
		ShowContext: true,
	}
}

// LoadSettings decodes TOML over defaults. Missing file yields defaults; malformed TOML is an error.
func LoadSettings(path string) (Settings, error) {
	s := DefaultSettings()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return Settings{}, err
	}
	if err := toml.Unmarshal(data, &s); err != nil {
		return Settings{}, err
	}
	if !settingsHasShowContextKey(data) && !settingsExplicitShowContextFalse(data) {
		s.ShowContext = true
	}
	return s, nil
}

func settingsHasShowContextKey(data []byte) bool {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw["show_context"]
	return ok
}

func settingsExplicitShowContextFalse(data []byte) bool {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return false
	}
	v, ok := raw["show_context"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && !b
}

// ValidateSettings guards impossible combinations (placeholder for future constraints).
func ValidateSettings(s Settings) error {
	_ = s
	return nil
}
