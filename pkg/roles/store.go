package roles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store is a thread-safe handle on a roles directory.
// Reload swaps in a new catalog only after a successful load, so readers
// keep the previous snapshot when a file is invalid.
type Store struct {
	mu      sync.RWMutex
	dir     string
	catalog *Catalog
}

// Open loads dir and returns a store that can be reloaded while readers run.
// If dir does not exist or cannot be opened, it gracefully falls back to the
// embedded built-in roles catalog.
func Open(ctx context.Context, dir string) (*Store, error) {
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err == nil {
			if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
				catalog, loadErr := Load(ctx, abs)
				if loadErr == nil {
					return &Store{dir: abs, catalog: catalog}, nil
				}
			}
		}
	}
	catalog, err := LoadBuiltin(ctx)
	if err != nil {
		return nil, fmt.Errorf("load builtin roles: %w", err)
	}
	return &Store{dir: "", catalog: catalog}, nil
}

// Reload reads the roles directory again.
// On failure the previous catalog stays in place.
func (s *Store) Reload(ctx context.Context) error {
	if s.dir == "" {
		catalog, err := LoadBuiltin(ctx)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.catalog = catalog
		s.mu.Unlock()
		return nil
	}
	catalog, err := Load(ctx, s.dir)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.catalog = catalog
	s.mu.Unlock()
	return nil
}

// Role returns a copy of one role from the current snapshot.
func (s *Store) Role(id string) (Role, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return Role{}, false
	}
	return s.catalog.Role(id)
}

// Department returns a copy of one department from the current snapshot.
func (s *Store) Department(id string) (Department, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return Department{}, false
	}
	return s.catalog.Department(id)
}

// Roles returns a copy of every role in the current snapshot.
func (s *Store) Roles() []Role {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return []Role{}
	}
	return s.catalog.Roles()
}

// Departments returns a copy of every department in the current snapshot.
func (s *Store) Departments() []Department {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return []Department{}
	}
	return s.catalog.Departments()
}
