// Package roles loads drop-in role and department YAML files.
// File contents are static data: they are decoded and validated, never executed.
package roles

import "regexp"

// MaxFileBytes is the largest role or department file Load will read.
const MaxFileBytes = 64 * 1024

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// standardToolNames is the only tool allowlist a role file may name.
// It matches the tools registered by pkg/tools.NewDefaultRegistry.
var standardToolNames = []string{
	"edit_file",
	"grep_search",
	"list_dir",
	"run_command",
	"view_file",
	"write_file",
}

var standardToolSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(standardToolNames))
	for _, name := range standardToolNames {
		set[name] = struct{}{}
	}
	return set
}()

// Role is one drop-in teammate loaded from roles/<id>.yml.
type Role struct {
	ID          string
	Name        string
	Description string
	Department  string
	System      string
	Tools       []string
}

// Department is a team loaded from roles/departments/<id>.yml.
// Roles keeps the YAML list order.
type Department struct {
	ID          string
	Name        string
	Roles       []string
	DefaultLead string
}

// ValidID reports whether id matches ^[a-z0-9][a-z0-9_-]*$.
func ValidID(id string) bool {
	return idPattern.MatchString(id)
}

// StandardToolNames returns a copy of the tool names a role YAML file may list.
func StandardToolNames() []string {
	out := make([]string, len(standardToolNames))
	copy(out, standardToolNames)
	return out
}
