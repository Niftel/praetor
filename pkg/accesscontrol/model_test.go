package accesscontrol

import "testing"

func TestCatalogIsClosedAndUnique(t *testing.T) {
	if err := ValidateCatalog(); err != nil {
		t.Fatal(err)
	}
	if got := len(Catalog()); got != 49 {
		t.Fatalf("catalog contains %d capabilities, want 49", got)
	}
	if IsCapability(Inventory, Execute) {
		t.Fatal("inventory execute must not enter the closed vocabulary")
	}
	if !IsCapability(JobTemplate, Execute) {
		t.Fatal("job-template execute capability missing")
	}
}

func TestBuiltinRolesUseCatalogCapabilities(t *testing.T) {
	known := make(map[string]struct{})
	for _, definition := range Catalog() {
		known[definition.Codename] = struct{}{}
	}
	roles := BuiltinRoles()
	if len(roles) != 35 {
		t.Fatalf("built-in role count = %d, want 35", len(roles))
	}
	names := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if _, duplicate := names[role.Name]; duplicate {
			t.Fatalf("duplicate built-in role %q", role.Name)
		}
		names[role.Name] = struct{}{}
		for _, capability := range role.Capabilities {
			if _, ok := known[capability]; !ok {
				t.Fatalf("role %q references unknown capability %q", role.Name, capability)
			}
		}
	}
}
