package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Connection is an API endpoint entered in the app, instead of a .env file.
// The API key is held in memory and in the system keystore, not in the config file.
type Connection struct {
	ID              string
	Name            string
	Protocol        string
	BaseURL         string
	APIKey          string
	Model           string
	PromptPrice     float64
	CompletionPrice float64
	HasPrice        bool
}

// APISummary is one saved endpoint without its key.
type APISummary struct {
	ID              string
	Name            string
	BaseURL         string
	Model           string
	Protocol        string
	Current         bool
	HasKey          bool
	HasPrice        bool
	PromptPrice     string
	CompletionPrice string
}

// ConnectionForm is the editable view of a connection. The API key is never included.
type ConnectionForm struct {
	BaseURL         string
	Model           string
	HasKey          bool
	HasPrice        bool
	PromptPrice     string
	CompletionPrice string
}

// ParseConnection checks a URL, key, and model. Blank price fields mean no price.
func ParseConnection(baseURL, apiKey, model, promptRaw, completionRaw string) (Connection, error) {
	conn := Connection{
		BaseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:   strings.TrimSpace(apiKey),
		Model:    strings.TrimSpace(model),
		Protocol: ProtocolForURL(baseURL),
	}
	conn.ID = ConnectionID(conn.BaseURL, conn.Model)
	if conn.BaseURL == "" {
		return Connection{}, fmt.Errorf("api url is required")
	}
	if !strings.HasPrefix(conn.BaseURL, "http://") && !strings.HasPrefix(conn.BaseURL, "https://") {
		return Connection{}, fmt.Errorf("api url must start with http:// or https://")
	}
	if conn.APIKey == "" {
		return Connection{}, fmt.Errorf("api key is required")
	}
	if conn.Model == "" {
		return Connection{}, fmt.Errorf("model is required")
	}
	prompt, promptSet, err := parseOptionalPrice(promptRaw)
	if err != nil {
		return Connection{}, fmt.Errorf("prompt price: %w", err)
	}
	completion, completionSet, err := parseOptionalPrice(completionRaw)
	if err != nil {
		return Connection{}, fmt.Errorf("completion price: %w", err)
	}
	if promptSet || completionSet {
		conn.HasPrice = true
		conn.PromptPrice = prompt
		conn.CompletionPrice = completion
	}
	return conn, nil
}

func parseOptionalPrice(raw string) (float64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 {
		return 0, false, fmt.Errorf("must be a number, or left blank")
	}
	return v, true, nil
}

// ConnectionID is stable for a URL and model, so saving the same pair updates it.
func ConnectionID(baseURL, model string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	model = strings.TrimSpace(model)
	sum := sha256.Sum256([]byte(baseURL + "\n" + model))
	return hex.EncodeToString(sum[:8])
}

// ProtocolForURL picks the request format. Anthropic and Google Gemini use
// their native APIs; every other URL uses the OpenAI-compatible chat format.
func ProtocolForURL(baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	host := ""
	if err == nil {
		host = strings.ToLower(parsed.Host)
	}
	switch {
	case strings.Contains(host, "anthropic.com"):
		return "anthropic"
	case strings.Contains(host, "generativelanguage.googleapis.com"):
		return "gemini"
	default:
		return "openai"
	}
}

// UseConnection makes this endpoint active and keeps every other saved API.
func (c *Config) UseConnection(conn Connection) error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if strings.TrimSpace(conn.BaseURL) == "" || strings.TrimSpace(conn.Model) == "" {
		return fmt.Errorf("api url and model are required")
	}
	if conn.ID == "" {
		conn.ID = ConnectionID(conn.BaseURL, conn.Model)
	}
	if conn.Protocol == "" {
		conn.Protocol = ProtocolForURL(conn.BaseURL)
	}
	if strings.TrimSpace(conn.APIKey) == "" {
		return fmt.Errorf("api key is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.upsert(conn)
	if conn.Name != "" {
		for i := range c.saved {
			if c.saved[i].BaseURL == conn.BaseURL {
				c.saved[i].Name = conn.Name
			}
		}
	}
	copied := conn
	c.manual = &copied
	c.ActiveModel = copied.Model
	return nil
}

// ImportConnections loads saved APIs. The active id is used when its key is present.
func (c *Config) ImportConnections(conns []Connection, activeID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saved = append([]Connection(nil), conns...)
	for _, conn := range c.saved {
		if conn.ID == activeID && strings.TrimSpace(conn.APIKey) != "" {
			copied := conn
			c.manual = &copied
			c.ActiveModel = copied.Model
			return
		}
	}
}

// KeyFor returns the key already saved for this URL and model.
func (c *Config) KeyFor(id string) string {
	if c == nil || id == "" {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, conn := range c.saved {
		if conn.ID == id && conn.APIKey != "" {
			return conn.APIKey
		}
	}
	return ""
}

// SavedAPIs lists endpoints without revealing keys.
func (c *Config) SavedAPIs() []APISummary {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]APISummary, 0, len(c.saved))
	active := ""
	if c.manual != nil {
		active = c.manual.ID
	}
	for _, conn := range c.saved {
		item := APISummary{
			ID:       conn.ID,
			Name:     conn.Name,
			BaseURL:  conn.BaseURL,
			Model:    conn.Model,
			Protocol: conn.Protocol,
			Current:  conn.ID == active,
			HasKey:   conn.APIKey != "",
			HasPrice: conn.HasPrice,
		}
		if conn.HasPrice {
			item.PromptPrice = formatPrice(conn.PromptPrice)
			item.CompletionPrice = formatPrice(conn.CompletionPrice)
		}
		out = append(out, item)
	}
	return out
}

func (c *Config) upsert(conn Connection) {
	for i := range c.saved {
		if c.saved[i].ID == conn.ID {
			c.saved[i] = conn
			return
		}
	}
	c.saved = append(c.saved, conn)
}

func (c *Config) matchSaved(modelName string) (Connection, bool) {
	if c.manual != nil && (modelName == "" || c.manual.Model == modelName) {
		return *c.manual, true
	}
	var found Connection
	ok := false
	for _, conn := range c.saved {
		if conn.Model == modelName {
			found = conn
			ok = true
		}
	}
	return found, ok
}

// CurrentAPIKey returns the key entered for the active connection, or the env key for the active model.
func (c *Config) CurrentAPIKey() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.manual != nil && c.manual.APIKey != "" {
		return c.manual.APIKey
	}
	for _, prov := range c.Providers {
		for _, model := range prov.Models {
			if model == c.ActiveModel && prov.EnvKey != "" {
				if key := os.Getenv(prov.EnvKey); key != "" {
					return key
				}
			}
		}
	}
	return ""
}

// ConnectionForm returns the fields to show in the connect screen.
func (c *Config) ConnectionForm() ConnectionForm {
	if c == nil {
		return ConnectionForm{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	form := ConnectionForm{Model: c.ActiveModel}
	if c.manual != nil {
		form.BaseURL = c.manual.BaseURL
		form.Model = c.manual.Model
		form.HasKey = c.manual.APIKey != ""
		form.HasPrice = c.manual.HasPrice
		if c.manual.HasPrice {
			form.PromptPrice = formatPrice(c.manual.PromptPrice)
			form.CompletionPrice = formatPrice(c.manual.CompletionPrice)
		}
		return form
	}
	for _, prov := range c.Providers {
		for _, model := range prov.Models {
			if model == c.ActiveModel {
				form.BaseURL = prov.BaseURL
				form.HasKey = prov.EnvKey != "" && os.Getenv(prov.EnvKey) != ""
			}
		}
	}
	if c.pricesKnown(c.ActiveModel) {
		form.HasPrice = true
		form.PromptPrice = formatPrice(c.Pricing.Prompt[c.ActiveModel])
		form.CompletionPrice = formatPrice(c.Pricing.Completion[c.ActiveModel])
	}
	return form
}

func formatPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
