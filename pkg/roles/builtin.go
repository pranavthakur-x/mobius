package roles

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

//go:embed builtin/*.yml builtin/departments/*.yml
var builtinFS embed.FS

// LoadBuiltin loads the roles and departments embedded in the binary.
func LoadBuiltin(ctx context.Context) (*Catalog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	roleEntries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return nil, fmt.Errorf("read embedded roles: %w", err)
	}

	var roleNames []string
	for _, entry := range roleEntries {
		if !entry.IsDir() && isYAML(entry.Name()) {
			roleNames = append(roleNames, entry.Name())
		}
	}
	sort.Strings(roleNames)

	roles := make([]Role, 0, len(roleNames))
	byID := make(map[string]Role, len(roleNames))
	ids := make(map[string]struct{}, len(roleNames))

	for _, name := range roleNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join("builtin", name)
		data, err := builtinFS.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", path, err)
		}
		var doc roleFile
		if err := decodeYAML(path, data, &doc); err != nil {
			return nil, err
		}
		role, err := validateRole(doc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if _, exists := ids[role.ID]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateID, role.ID)
		}
		ids[role.ID] = struct{}{}
		byID[role.ID] = role
		roles = append(roles, role)
	}

	deptEntries, err := fs.ReadDir(builtinFS, "builtin/departments")
	if err != nil {
		return newCatalog(roles, nil), nil
	}

	var deptNames []string
	for _, entry := range deptEntries {
		if !entry.IsDir() && isYAML(entry.Name()) {
			deptNames = append(deptNames, entry.Name())
		}
	}
	sort.Strings(deptNames)

	departments := make([]Department, 0, len(deptNames))
	claimed := make(map[string]string)

	for _, name := range deptNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join("builtin/departments", name)
		data, err := builtinFS.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", path, err)
		}
		var doc departmentFile
		if err := decodeYAML(path, data, &doc); err != nil {
			return nil, err
		}
		dept, err := validateDepartment(doc, byID, claimed)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if _, exists := ids[dept.ID]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateID, dept.ID)
		}
		ids[dept.ID] = struct{}{}
		for _, roleID := range dept.Roles {
			claimed[roleID] = dept.ID
		}
		departments = append(departments, dept)
	}

	return newCatalog(roles, departments), nil
}
