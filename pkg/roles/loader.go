package roles

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

var (
	// ErrEscape means a role path resolved outside the roles directory.
	ErrEscape = errors.New("role path escapes the roles directory")
	// ErrTooLarge means a role file is larger than MaxFileBytes.
	ErrTooLarge = errors.New("role file exceeds 64KiB")
	// ErrInvalidID means an id failed ValidID.
	ErrInvalidID = errors.New("invalid id")
	// ErrDuplicateID means two files or one list used the same id.
	ErrDuplicateID = errors.New("duplicate id")
	// ErrUnknownTool means a role listed a tool outside the allowlist.
	ErrUnknownTool = errors.New("unknown tool")
	// ErrUnknownRole means a department listed a role that was not loaded.
	ErrUnknownRole = errors.New("unknown role")
	// ErrBadLead means default_lead is not a member of that department.
	ErrBadLead = errors.New("invalid default_lead")
	// ErrEmpty means a YAML file had no document.
	ErrEmpty = errors.New("empty role file")
)

type roleFile struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Department  string   `yaml:"department"`
	System      string   `yaml:"system"`
	Tools       []string `yaml:"tools"`
}

type departmentFile struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Roles       []string `yaml:"roles"`
	DefaultLead string   `yaml:"default_lead"`
}

// Catalog is an immutable snapshot of roles and departments.
// Lookup methods return copies so callers cannot mutate stored slices.
type Catalog struct {
	roles       []Role
	departments []Department
	roleIndex   map[string]int
	deptIndex   map[string]int
}

// Roles returns the loaded roles in file-name order.
func (c *Catalog) Roles() []Role {
	if c == nil {
		return []Role{}
	}
	out := make([]Role, len(c.roles))
	for i := range c.roles {
		out[i] = cloneRole(c.roles[i])
	}
	return out
}

// Departments returns the loaded departments in file-name order.
func (c *Catalog) Departments() []Department {
	if c == nil {
		return []Department{}
	}
	out := make([]Department, len(c.departments))
	for i := range c.departments {
		out[i] = cloneDepartment(c.departments[i])
	}
	return out
}

// Role returns a copy of the role with the given id.
func (c *Catalog) Role(id string) (Role, bool) {
	if c == nil {
		return Role{}, false
	}
	index, ok := c.roleIndex[id]
	if !ok {
		return Role{}, false
	}
	return cloneRole(c.roles[index]), true
}

// Department returns a copy of the department with the given id.
func (c *Catalog) Department(id string) (Department, bool) {
	if c == nil {
		return Department{}, false
	}
	index, ok := c.deptIndex[id]
	if !ok {
		return Department{}, false
	}
	return cloneDepartment(c.departments[index]), true
}

// Load reads roles/*.yml and roles/departments/*.yml from dir.
// Only .yml and .yaml files are decoded. Symlinks that resolve outside
// dir are rejected. The returned catalog is safe to read concurrently.
func Load(ctx context.Context, dir string) (*Catalog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve roles directory: %w", err)
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve roles directory: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("open roles directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("open roles directory: %s is not a directory", dir)
	}

	roleNames, err := yamlNames(ctx, root)
	if err != nil {
		return nil, err
	}
	roles := make([]Role, 0, len(roleNames))
	byID := make(map[string]Role, len(roleNames))
	ids := make(map[string]struct{}, len(roleNames))
	for _, name := range roleNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		role, err := loadRoleFile(root, filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		if _, exists := ids[role.ID]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateID, role.ID)
		}
		ids[role.ID] = struct{}{}
		byID[role.ID] = role
		roles = append(roles, role)
	}

	departments, err := loadDepartments(ctx, root, byID, ids)
	if err != nil {
		return nil, err
	}
	return newCatalog(roles, departments), nil
}

func newCatalog(roles []Role, departments []Department) *Catalog {
	catalog := &Catalog{
		roles:       roles,
		departments: departments,
		roleIndex:   make(map[string]int, len(roles)),
		deptIndex:   make(map[string]int, len(departments)),
	}
	for i := range roles {
		catalog.roleIndex[roles[i].ID] = i
	}
	for i := range departments {
		catalog.deptIndex[departments[i].ID] = i
	}
	return catalog
}

func yamlNames(ctx context.Context, dir string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read roles directory: %w", err)
	}
	return yamlEntryNames(ctx, entries)
}

func yamlNamesFromDir(ctx context.Context, dir *os.File) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("read departments directory: %w", err)
	}
	return yamlEntryNames(ctx, entries)
}

func yamlEntryNames(ctx context.Context, entries []os.DirEntry) ([]string, error) {
	names := make([]string, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if !isYAML(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func loadRoleFile(root, path string) (Role, error) {
	opened, err := openRegularInside(root, path)
	if err != nil {
		return Role{}, err
	}
	defer opened.Close()
	data, err := readLimitedFile(opened, path)
	if err != nil {
		return Role{}, err
	}
	var doc roleFile
	if err := decodeYAML(path, data, &doc); err != nil {
		return Role{}, err
	}
	role, err := validateRole(doc)
	if err != nil {
		return Role{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return role, nil
}

func loadDepartments(ctx context.Context, root string, roles map[string]Role, ids map[string]struct{}) ([]Department, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deptDir := filepath.Join(root, "departments")
	dirFile, err := openDirInside(root, deptDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Department{}, nil
		}
		return nil, err
	}
	defer dirFile.Close()

	names, err := yamlNamesFromDir(ctx, dirFile)
	if err != nil {
		return nil, err
	}
	departments := make([]Department, 0, len(names))
	claimed := make(map[string]string)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(deptDir, name)
		opened, err := openRegularInside(root, path)
		if err != nil {
			return nil, err
		}
		data, err := readLimitedFile(opened, path)
		opened.Close()
		if err != nil {
			return nil, err
		}
		var doc departmentFile
		if err := decodeYAML(path, data, &doc); err != nil {
			return nil, err
		}
		dept, err := validateDepartment(doc, roles, claimed)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
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
	return departments, nil
}

func validateRole(file roleFile) (Role, error) {
	if !ValidID(file.ID) {
		return Role{}, fmt.Errorf("%w: %q", ErrInvalidID, file.ID)
	}
	name, err := cleanDisplayName(file.Name)
	if err != nil {
		return Role{}, fmt.Errorf("role %s: %w", file.ID, err)
	}
	description, err := cleanOptionalDescription(file.Description)
	if err != nil {
		return Role{}, fmt.Errorf("role %s: %w", file.ID, err)
	}
	if !ValidID(file.Department) {
		return Role{}, fmt.Errorf("role %s: %w: %q", file.ID, ErrInvalidID, file.Department)
	}
	if strings.ContainsRune(file.System, '\x00') {
		return Role{}, fmt.Errorf("role %s: system prompt contains a NUL byte", file.ID)
	}
	tools := make([]string, 0, len(file.Tools))
	seen := make(map[string]struct{}, len(file.Tools))
	for _, tool := range file.Tools {
		if !ValidID(tool) {
			return Role{}, fmt.Errorf("role %s: %w: %q", file.ID, ErrUnknownTool, tool)
		}
		if _, ok := standardToolSet[tool]; !ok {
			return Role{}, fmt.Errorf("role %s: %w: %q", file.ID, ErrUnknownTool, tool)
		}
		if _, exists := seen[tool]; exists {
			return Role{}, fmt.Errorf("role %s: %w: %q", file.ID, ErrDuplicateID, tool)
		}
		seen[tool] = struct{}{}
		tools = append(tools, tool)
	}
	return Role{
		ID:          file.ID,
		Name:        name,
		Description: description,
		Department:  file.Department,
		System:      file.System,
		Tools:       tools,
	}, nil
}

func validateDepartment(file departmentFile, roles map[string]Role, claimed map[string]string) (Department, error) {
	if !ValidID(file.ID) {
		return Department{}, fmt.Errorf("%w: %q", ErrInvalidID, file.ID)
	}
	name, err := cleanDisplayName(file.Name)
	if err != nil {
		return Department{}, fmt.Errorf("department %s: %w", file.ID, err)
	}
	if len(file.Roles) == 0 {
		return Department{}, fmt.Errorf("department %s: roles list is empty", file.ID)
	}
	if !ValidID(file.DefaultLead) {
		return Department{}, fmt.Errorf("department %s: %w: %q", file.ID, ErrBadLead, file.DefaultLead)
	}
	members := make([]string, 0, len(file.Roles))
	seen := make(map[string]struct{}, len(file.Roles))
	for _, roleID := range file.Roles {
		if !ValidID(roleID) {
			return Department{}, fmt.Errorf("department %s: %w: %q", file.ID, ErrUnknownRole, roleID)
		}
		role, ok := roles[roleID]
		if !ok {
			return Department{}, fmt.Errorf("department %s: %w: %q", file.ID, ErrUnknownRole, roleID)
		}
		if role.Department != file.ID {
			return Department{}, fmt.Errorf("department %s: role %s belongs to %s", file.ID, roleID, role.Department)
		}
		if prev, ok := claimed[roleID]; ok {
			return Department{}, fmt.Errorf("department %s: role %s is already listed in %s", file.ID, roleID, prev)
		}
		if _, exists := seen[roleID]; exists {
			return Department{}, fmt.Errorf("department %s: %w: %q", file.ID, ErrDuplicateID, roleID)
		}
		seen[roleID] = struct{}{}
		members = append(members, roleID)
	}
	if _, ok := seen[file.DefaultLead]; !ok {
		return Department{}, fmt.Errorf("department %s: %w: %q", file.ID, ErrBadLead, file.DefaultLead)
	}
	return Department{
		ID:          file.ID,
		Name:        name,
		Roles:       members,
		DefaultLead: file.DefaultLead,
	}, nil
}

func cleanDisplayName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("name is required")
	}
	for _, r := range trimmed {
		if r == '\n' || r == '\r' || unicode.IsControl(r) {
			return "", fmt.Errorf("name contains a control character")
		}
	}
	return trimmed, nil
}

const maxDescriptionRunes = 240

func cleanOptionalDescription(desc string) (string, error) {
	trimmed := strings.TrimSpace(desc)
	if trimmed == "" {
		return "", nil
	}
	n := 0
	for _, r := range trimmed {
		if r == '\n' || r == '\r' || unicode.IsControl(r) {
			return "", fmt.Errorf("description contains a control character")
		}
		n++
		if n > maxDescriptionRunes {
			return "", fmt.Errorf("description exceeds %d characters", maxDescriptionRunes)
		}
	}
	return trimmed, nil
}

func decodeYAML(path string, data []byte, dest any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(dest); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s: %w", filepath.Base(path), ErrEmpty)
		}
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	var extra any
	err := dec.Decode(&extra)
	if err == nil {
		return fmt.Errorf("%s: multiple yaml documents", filepath.Base(path))
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

func readLimitedFile(file *os.File, path string) ([]byte, error) {
	label := filepath.Base(path)
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", label, err)
	}
	if info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("%s: %w", label, ErrTooLarge)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	if int64(len(data)) > MaxFileBytes {
		return nil, fmt.Errorf("%s: %w", label, ErrTooLarge)
	}
	return data, nil
}

func isYAML(name string) bool {
	switch filepath.Ext(name) {
	case ".yml", ".yaml":
		return true
	default:
		return false
	}
}

func cloneRole(role Role) Role {
	role.Tools = cloneStrings(role.Tools)
	return role
}

func cloneDepartment(dept Department) Department {
	dept.Roles = cloneStrings(dept.Roles)
	return dept
}

func cloneStrings(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	return out
}
