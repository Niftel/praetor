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

func TestCredentialAccessIdentifiersArePublicCatalogVocabulary(t *testing.T) {
	var capability *CapabilityDefinition
	catalog := Catalog()
	for index := range catalog {
		definition := catalog[index]
		if definition.Codename == ManageCredentialType {
			capability = &definition
			break
		}
	}
	if capability == nil {
		t.Fatal("credential-type management capability missing")
	}
	if capability.Codename != "manage_credentialtype" || capability.ResourceKind != "credential_type" || capability.Verb != Manage {
		t.Fatalf("unexpected credential-type capability: %+v", capability)
	}
	name, ok := BuiltinRoleName(Organization, CredentialAdminRole)
	if !ok || CredentialAdminRole != "credential_admin_role" || name != "Organization Credential Admin" {
		t.Fatalf("unexpected public credential-admin role: kind=%q name=%q present=%v", CredentialAdminRole, name, ok)
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
