package roles

import (
	"context"
	"testing"
)

func TestLoadBuiltin(t *testing.T) {
	catalog, err := LoadBuiltin(context.Background())
	if err != nil {
		t.Fatalf("LoadBuiltin failed: %v", err)
	}

	dept, ok := catalog.Department("engineering")
	if !ok {
		t.Fatal("missing engineering department in builtin")
	}
	if dept.DefaultLead != "software_engineer" {
		t.Fatalf("unexpected default lead: %s", dept.DefaultLead)
	}

	swe, ok := catalog.Role("software_engineer")
	if !ok {
		t.Fatal("missing software_engineer in builtin")
	}
	if len(swe.Tools) == 0 {
		t.Fatal("software_engineer has no tools")
	}
}

func TestOpenFallbackToBuiltin(t *testing.T) {
	// Opening a nonexistent directory should smoothly fall back to builtin roles without error
	store, err := Open(context.Background(), "/nonexistent/path/to/roles")
	if err != nil {
		t.Fatalf("Open with nonexistent path failed: %v", err)
	}

	swe, ok := store.Role("software_engineer")
	if !ok {
		t.Fatal("missing software_engineer from fallback store")
	}
	if swe.Name != "Software Engineer" {
		t.Fatalf("unexpected name: %s", swe.Name)
	}
}

func TestBuiltinMatchesShippedCatalog(t *testing.T) {
	builtin, err := LoadBuiltin(context.Background())
	if err != nil {
		t.Fatalf("LoadBuiltin failed: %v", err)
	}
	shipped := loadDir(t, shippedRolesDir(t))

	builtinRoles := builtin.Roles()
	shippedRoles := shipped.Roles()
	if len(builtinRoles) != len(shippedRoles) {
		t.Fatalf("roles count mismatch: builtin %d vs shipped %d", len(builtinRoles), len(shippedRoles))
	}
	for i := range builtinRoles {
		if builtinRoles[i].ID != shippedRoles[i].ID {
			t.Errorf("role[%d] ID mismatch: %s vs %s", i, builtinRoles[i].ID, shippedRoles[i].ID)
		}
	}

	builtinDepts := builtin.Departments()
	shippedDepts := shipped.Departments()
	if len(builtinDepts) != len(shippedDepts) {
		t.Fatalf("dept count mismatch: builtin %d vs shipped %d", len(builtinDepts), len(shippedDepts))
	}
	for i := range builtinDepts {
		if builtinDepts[i].ID != shippedDepts[i].ID {
			t.Errorf("dept[%d] ID mismatch: %s vs %s", i, builtinDepts[i].ID, shippedDepts[i].ID)
		}
	}
}
