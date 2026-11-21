package guides

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Guide represents a loaded workspace instruction file and its priority.
type Guide struct {
	Source   string
	Path     string
	Content  string
	Priority int
}

// GuideSet holds an ordered collection of workspace guides.
type GuideSet struct {
	Guides []Guide
}

// NewGuideSet initializes an empty GuideSet.
func NewGuideSet() *GuideSet {
	return &GuideSet{
		Guides: make([]Guide, 0),
	}
}

// Add appends a guide to the set.
func (gs *GuideSet) Add(g Guide) {
	gs.Guides = append(gs.Guides, g)
}

// IsEmpty reports whether the set contains no guides.
func (gs *GuideSet) IsEmpty() bool {
	return len(gs.Guides) == 0
}

// SortByPriority stably orders guides from highest to lowest priority.
func (gs *GuideSet) SortByPriority() {
	sort.SliceStable(gs.Guides, func(i, j int) bool {
		return gs.Guides[i].Priority > gs.Guides[j].Priority
	})
}

// Candidate defines a guide file path and priority entry in guides TOML config.
type Candidate struct {
	Path     string `toml:"path"`
	Priority int
}

// GuidesConfig models the TOML configuration listing workspace guide candidates.
type GuidesConfig struct {
	Candidates []Candidate `toml:"candidates"`
}

// LoadFromWorkSpace loads configured guide candidates relative to workspaceRoot
// and returns them sorted by descending priority. Missing guide files are skipped.
func LoadFromWorkSpace(configPath string, workspaceRoot string) (*GuideSet, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read guides config '%s': %w", configPath, err)
	}

	var cfg GuidesConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse guides config '%s': %w", configPath, err)
	}

	gs := NewGuideSet()
	for _, c := range cfg.Candidates {
		targetPath := filepath.Join(workspaceRoot, c.Path)

		data, err := os.ReadFile(targetPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("failed to read markdown guide '%s': %w", targetPath, err)
		}

		var guide = Guide{
			Source:   c.Path,
			Path:     targetPath,
			Content:  string(data),
			Priority: c.Priority,
		}
		gs.Add(guide)
	}
	gs.SortByPriority()

	return gs, nil
}

// RenderSystemPrompt formats the guide set for inclusion in a system prompt,
// ordering guides from lowest to highest priority so later sections override earlier ones.
func (gs *GuideSet) RenderSystemPrompt() string {
	if gs.IsEmpty() {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n# WORKSPACE GUIDES & RULES\n")
	sb.WriteString("If guides conflict, the **last** section below takes precedence.\n\n")
	// Lowest priority first, highest last — later sections override earlier ones.
	for i := len(gs.Guides) - 1; i >= 0; i-- {
		g := gs.Guides[i]
		sb.WriteString("## Guide: ")
		sb.WriteString(g.Source)
		sb.WriteString("\n```markdown\n")
		sb.WriteString(strings.TrimSpace(g.Content))
		sb.WriteString("\n```\n\n")
	}
	return sb.String()
}
