package roles

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestShippedEngineeringCatalog(t *testing.T) {
	catalog := loadDir(t, shippedRolesDir(t))

	dept, ok := catalog.Department("engineering")
	if !ok {
		t.Fatal("missing engineering department")
	}
	if dept.Name != "Engineering" || dept.DefaultLead != "software_engineer" {
		t.Fatalf("department = %+v", dept)
	}
	if strings.Join(dept.Roles, ",") != "software_engineer,code_review" {
		t.Fatalf("role order = %v", dept.Roles)
	}

	engineer, ok := catalog.Role("software_engineer")
	if !ok || engineer.Name != "Software Engineer" {
		t.Fatalf("software engineer = %+v", engineer)
	}
	if engineer.Description == "" {
		t.Fatal("expected shipped software_engineer description")
	}
	if strings.Join(engineer.Tools, ",") != "view_file,list_dir,grep_search,edit_file,write_file,run_command" {
		t.Fatalf("engineer tools = %v", engineer.Tools)
	}

	review, ok := catalog.Role("code_review")
	if !ok || review.Name != "Code Review" {
		t.Fatalf("code review = %+v", review)
	}
	if review.Description == "" {
		t.Fatal("expected shipped code_review description")
	}
	for _, tool := range review.Tools {
		switch tool {
		case "edit_file", "write_file", "run_command":
			t.Fatalf("code review has write tool %s", tool)
		}
	}
}

func TestLoadPreservesYAMLOrder(t *testing.T) {
	dir := t.TempDir()
	writeEngineering(t, dir)
	catalog := loadDir(t, dir)

	roles := catalog.Roles()
	if len(roles) != 2 || roles[0].ID != "code_review" || roles[1].ID != "software_engineer" {
		t.Fatalf("file order = %v", roleIDs(roles))
	}
	dept, ok := catalog.Department("engineering")
	if !ok {
		t.Fatal("missing engineering department")
	}
	if strings.Join(dept.Roles, ",") != "software_engineer,code_review" {
		t.Fatalf("yaml order = %v", dept.Roles)
	}
}

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name    string
		write   func(t *testing.T, dir string)
		wantErr error
		text    string
	}{
		{
			name: "unknown field",
			write: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "solo.yml"), `
id: solo
name: Solo
department: research
system: hello
tools: []
extra: true
`)
			},
			text: "not found",
		},
		{
			name: "bad id",
			write: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "solo.yml"), `
id: SoftwareEngineer
name: Solo
department: research
system: hello
`)
			},
			wantErr: ErrInvalidID,
		},
		{
			name: "duplicate id",
			write: func(t *testing.T, dir string) {
				body := `
id: solo
name: Solo
department: research
system: hello
`
				writeFile(t, filepath.Join(dir, "a.yml"), body)
				writeFile(t, filepath.Join(dir, "b.yml"), body)
			},
			wantErr: ErrDuplicateID,
		},
		{
			name: "role department id clash",
			write: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "engineering.yml"), `
id: engineering
name: Engineering Role
department: engineering
system: hello
tools:
  - view_file
`)
				writeFile(t, filepath.Join(dir, "departments", "engineering.yml"), `
id: engineering
name: Engineering
roles:
  - engineering
default_lead: engineering
`)
			},
			wantErr: ErrDuplicateID,
		},
		{
			name: "unknown tool",
			write: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "solo.yml"), `
id: solo
name: Solo
department: research
system: hello
tools:
  - bash
`)
			},
			wantErr: ErrUnknownTool,
		},
		{
			name: "duplicate tool",
			write: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "solo.yml"), `
id: solo
name: Solo
department: research
system: hello
tools:
  - view_file
  - view_file
`)
			},
			wantErr: ErrDuplicateID,
		},
		{
			name: "missing role",
			write: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "software_engineer.yml"), engineerYAML)
				writeFile(t, filepath.Join(dir, "departments", "engineering.yml"), `
id: engineering
name: Engineering
roles:
  - software_engineer
  - missing
default_lead: software_engineer
`)
			},
			wantErr: ErrUnknownRole,
		},
		{
			name: "bad lead",
			write: func(t *testing.T, dir string) {
				writeEngineering(t, dir)
				writeFile(t, filepath.Join(dir, "departments", "engineering.yml"), `
id: engineering
name: Engineering
roles:
  - software_engineer
  - code_review
default_lead: missing
`)
			},
			wantErr: ErrBadLead,
		},
		{
			name: "oversized",
			write: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "big.yml"), string(bytes.Repeat([]byte("a"), MaxFileBytes+1)))
			},
			wantErr: ErrTooLarge,
		},
		{
			name: "description control char",
			write: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "solo.yml"), "id: solo\nname: Solo\ndescription: \"line1\\nline2\"\ndepartment: research\nsystem: hello\n")
			},
			text: "control character",
		},
		{
			name: "description too long",
			write: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "solo.yml"), "id: solo\nname: Solo\ndescription: "+strings.Repeat("x", maxDescriptionRunes+1)+"\ndepartment: research\nsystem: hello\n")
			},
			text: "exceeds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.write(t, dir)
			_, err := Load(context.Background(), dir)
			if err == nil {
				t.Fatal("expected an error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.text != "" && !strings.Contains(err.Error(), tt.text) {
				t.Fatalf("error = %v, want text %q", err, tt.text)
			}
		})
	}
}

func TestLoadRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	const sentinel = "SENTINEL_SECRET_VALUE"
	writeFile(t, filepath.Join(outside, "secret.yml"), "id: leaked\nsecret: "+sentinel+"\n")
	if err := os.Symlink(filepath.Join(outside, "secret.yml"), filepath.Join(dir, "escape.yml")); err != nil {
		t.Fatal(err)
	}

	_, err := Load(context.Background(), dir)
	if !errors.Is(err, ErrEscape) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatal("symlink error included file contents")
	}
}

func TestLoadRejectsDepartmentSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	const sentinel = "DEPT_SENTINEL_SECRET"
	writeFile(t, filepath.Join(outside, "engineering.yml"), "secret: "+sentinel+"\n")
	if err := os.Symlink(outside, filepath.Join(dir, "departments")); err != nil {
		t.Fatal(err)
	}

	_, err := Load(context.Background(), dir)
	if !errors.Is(err, ErrEscape) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatal("department symlink error included file contents")
	}
}

func TestLoadAllowsInternalSymlink(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "engineer.txt"), `
id: solo
name: Solo
department: research
system: hello
tools:
  - view_file
`)
	if err := os.Symlink("engineer.txt", filepath.Join(dir, "solo.yml")); err != nil {
		t.Fatal(err)
	}

	catalog := loadDir(t, dir)
	role, ok := catalog.Role("solo")
	if !ok {
		t.Fatal("missing solo role")
	}
	if len(role.Tools) != 1 || role.Tools[0] != "view_file" {
		t.Fatalf("tools = %v", role.Tools)
	}
}

func TestLoadIgnoresNonYAML(t *testing.T) {
	dir := t.TempDir()
	writeEngineering(t, dir)
	writeFile(t, filepath.Join(dir, "notes.txt"), "this is not: [valid")
	catalog := loadDir(t, dir)
	if _, ok := catalog.Role("software_engineer"); !ok {
		t.Fatal("missing software engineer")
	}
}

func TestLoadCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Load(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestStandardToolNamesAreACopy(t *testing.T) {
	names := StandardToolNames()
	names[0] = "mutated"
	again := StandardToolNames()
	if again[0] != "edit_file" {
		t.Fatalf("allowlist changed to %v", again)
	}
	if len(again) != len(standardToolSet) {
		t.Fatalf("names = %d, set = %d", len(again), len(standardToolSet))
	}
	for _, name := range again {
		if _, ok := standardToolSet[name]; !ok {
			t.Fatalf("name %s missing from allowlist", name)
		}
	}
}

func TestLoadConcurrent(t *testing.T) {
	dir := t.TempDir()
	writeEngineering(t, dir)

	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			catalog, err := Load(context.Background(), dir)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
			dept, ok := catalog.Department("engineering")
			if !ok || strings.Join(dept.Roles, ",") != "software_engineer,code_review" {
				select {
				case errCh <- errors.New("department order changed under concurrent load"):
				default:
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func shippedRolesDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "roles")
}

func loadDir(t *testing.T, dir string) *Catalog {
	t.Helper()
	catalog, err := Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("load roles: %v", err)
	}
	return catalog
}

func writeEngineering(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "software_engineer.yml"), engineerYAML)
	writeFile(t, filepath.Join(dir, "code_review.yml"), reviewYAML)
	writeFile(t, filepath.Join(dir, "departments", "engineering.yml"), engineeringYAML)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func roleIDs(roles []Role) []string {
	ids := make([]string, len(roles))
	for i := range roles {
		ids[i] = roles[i].ID
	}
	return ids
}

const engineerYAML = `
id: software_engineer
name: Software Engineer
department: engineering
system: Build the change.
tools:
  - view_file
  - list_dir
`

const reviewYAML = `
id: code_review
name: Code Review
department: engineering
system: Review the change.
tools:
  - view_file
`

const engineeringYAML = `
id: engineering
name: Engineering
roles:
  - software_engineer
  - code_review
default_lead: software_engineer
`
