package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type connectionFile struct {
	ID              string   `toml:"id"`
	Name            string   `toml:"name,omitempty"`
	Protocol        string   `toml:"protocol"`
	BaseURL         string   `toml:"base_url"`
	APIKey          string   `toml:"api_key,omitempty"`
	Model           string   `toml:"model"`
	PromptPrice     *float64 `toml:"prompt_price,omitempty"`
	CompletionPrice *float64 `toml:"completion_price,omitempty"`
}

type connectionsFile struct {
	Active      string           `toml:"active"`
	Connections []connectionFile `toml:"connections"`
}

// ConnectionPath returns the default connections config path (~/.config/mobius/connections.toml).
// API keys are stored in the system keystore rather than in that file.
func ConnectionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory: %w", err)
	}
	return filepath.Join(home, ".config", "mobius", "connections.toml"), nil
}

// LoadConnection reads the active saved connection. A missing file returns an error.
func LoadConnection(path string) (Connection, error) {
	conns, active, err := LoadSaved(path)
	if err != nil {
		return Connection{}, err
	}
	for _, conn := range conns {
		if conn.ID == active {
			return conn, nil
		}
	}
	if len(conns) == 0 {
		return Connection{}, os.ErrNotExist
	}
	return conns[len(conns)-1], nil
}

// LoadSaved reads every saved API. Keys are loaded from the system keystore.
func LoadSaved(path string) ([]Connection, string, error) {
	path, err := resolveConnectionPath(path)
	if err != nil {
		return nil, "", err
	}
	if err := migrateLegacyFile(path); err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var file connectionsFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return nil, "", fmt.Errorf("parse connections: %w", err)
	}
	var out []Connection
	dirty := false
	for _, item := range file.Connections {
		if strings.TrimSpace(item.APIKey) != "" {
			dirty = true
		}
		conn, err := connectionFromFile(item)
		if err != nil {
			return nil, "", err
		}
		out = append(out, conn)
	}
	if dirty {
		_ = writeConnectionFile(path, file)
	}
	return out, file.Active, nil
}

// SaveConnection adds or updates one API and stores its key in the system keystore.
func SaveConnection(path string, conn Connection) error {
	if conn.ID == "" {
		conn.ID = ConnectionID(conn.BaseURL, conn.Model)
	}
	if conn.Protocol == "" {
		conn.Protocol = ProtocolForURL(conn.BaseURL)
	}
	path, err := resolveConnectionPath(path)
	if err != nil {
		return err
	}
	if err := migrateLegacyFile(path); err != nil {
		return err
	}
	if err := putAPIKey(conn.ID, conn.APIKey); err != nil {
		return fmt.Errorf("save api key in the system keystore: %w", err)
	}
	existing, err := readConnectionFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	replaced := false
	for i := range existing.Connections {
		if existing.Connections[i].ID == conn.ID {
			existing.Connections[i] = fileRecord(conn)
			replaced = true
			break
		}
	}
	if conn.Name != "" {
		base := strings.TrimRight(conn.BaseURL, "/")
		for i := range existing.Connections {
			if strings.TrimRight(existing.Connections[i].BaseURL, "/") == base {
				existing.Connections[i].Name = conn.Name
			}
		}
	}
	if !replaced {
		existing.Connections = append(existing.Connections, fileRecord(conn))
	}
	existing.Active = conn.ID
	return writeConnectionFile(path, existing)
}

func resolveConnectionPath(path string) (string, error) {
	if strings.TrimSpace(path) != "" {
		return path, nil
	}
	return ConnectionPath()
}

func readConnectionFile(path string) (connectionsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return connectionsFile{}, err
	}
	var file connectionsFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return connectionsFile{}, fmt.Errorf("parse connections: %w", err)
	}
	return file, nil
}

func writeConnectionFile(path string, file connectionsFile) error {
	for i := range file.Connections {
		file.Connections[i].APIKey = ""
	}
	data, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode connections: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create connection directory: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

func fileRecord(conn Connection) connectionFile {
	item := connectionFile{
		ID:       conn.ID,
		Name:     conn.Name,
		Protocol: conn.Protocol,
		BaseURL:  conn.BaseURL,
		Model:    conn.Model,
	}
	if conn.HasPrice {
		prompt := conn.PromptPrice
		completion := conn.CompletionPrice
		item.PromptPrice = &prompt
		item.CompletionPrice = &completion
	}
	return item
}

func connectionFromFile(item connectionFile) (Connection, error) {
	conn := Connection{
		ID:       item.ID,
		Name:     strings.TrimSpace(item.Name),
		Protocol: item.Protocol,
		BaseURL:  strings.TrimRight(strings.TrimSpace(item.BaseURL), "/"),
		Model:    strings.TrimSpace(item.Model),
		APIKey:   strings.TrimSpace(item.APIKey),
	}
	if conn.Protocol == "" {
		conn.Protocol = ProtocolForURL(conn.BaseURL)
	}
	if conn.ID == "" {
		conn.ID = ConnectionID(conn.BaseURL, conn.Model)
	}
	if conn.APIKey != "" {
		if err := putAPIKey(conn.ID, conn.APIKey); err != nil {
			return Connection{}, fmt.Errorf("move api key into the system keystore: %w", err)
		}
	} else {
		key, err := getAPIKey(conn.ID)
		if err != nil {
			return Connection{}, fmt.Errorf("read api key from the system keystore: %w", err)
		}
		conn.APIKey = key
	}
	if item.PromptPrice != nil || item.CompletionPrice != nil {
		conn.HasPrice = true
		if item.PromptPrice != nil {
			conn.PromptPrice = *item.PromptPrice
		}
		if item.CompletionPrice != nil {
			conn.CompletionPrice = *item.CompletionPrice
		}
	}
	return conn, nil
}

func migrateLegacyFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	legacy := filepath.Join(filepath.Dir(path), "connection.toml")
	if legacy == path {
		return nil
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var old connectionFile
	if err := toml.Unmarshal(data, &old); err != nil {
		return fmt.Errorf("parse saved connection: %w", err)
	}
	if strings.TrimSpace(old.BaseURL) == "" || strings.TrimSpace(old.Model) == "" {
		return nil
	}
	conn, err := connectionFromFile(old)
	if err != nil {
		return err
	}
	if err := writeConnectionFile(path, connectionsFile{Active: conn.ID, Connections: []connectionFile{fileRecord(conn)}}); err != nil {
		return err
	}
	return os.Remove(legacy)
}
