package artifact

import (
	"fmt"
	"mobius/internal/id"
	"os"
	"path/filepath"
)

// Store manages large tool outputs on disk.
type Store struct {
	dir string
}

// NewStore creates an artifact store rooted at dir.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		dir = ".mobius/artifacts"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create artifact dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Save writes full content to disk and returns the artifact ID and file path.
func (s *Store) Save(threadID, content string) (artID string, refPath string, err error) {
	artID = id.NewArtifactID()
	filename := fmt.Sprintf("%s_%s.txt", threadID, artID)
	refPath = filepath.Join(s.dir, filename)

	if err := os.WriteFile(refPath, []byte(content), 0644); err != nil {
		return "", "", fmt.Errorf("failed to write artifact: %w", err)
	}
	return artID, fmt.Sprintf("artifact://%s", filename), nil
}

// Read loads the full content of an artifact from disk.
func (s *Store) Read(ref string) (string, error) {
	// Strip "artifact://" prefix if present
	filename := ref
	if len(ref) > 11 && ref[:11] == "artifact://" {
		filename = ref[11:]
	}
	data, err := os.ReadFile(filepath.Join(s.dir, filename))
	if err != nil {
		return "", fmt.Errorf("failed to read artifact: %w", err)
	}
	return string(data), nil
}
