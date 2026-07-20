package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServicePrincipalsToleratesNullableGrantScopes(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "web", "pages", "ServicePrincipalsPage.tsx")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, contract := range []string{
		"unwrap<ServicePrincipal>(ps)",
		"unwrap<Named>(ws)",
		"unwrap<Inventory>(invs)",
		"unwrap<Team>(ts)",
		"unwrap<DelegatedLaunchGrant>(gs)",
		"Array.isArray(grant.allowed_host_ids)",
		"Array.isArray(grant.allowed_group_ids)",
		"Array.isArray(grant.allowed_extra_var_keys)",
	} {
		if !strings.Contains(source, contract) {
			t.Errorf("ServicePrincipalsPage must retain nullable grant-scope guard %q", contract)
		}
	}
}
