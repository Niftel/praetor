package inventorysourcecatalog

import "testing"

func TestCatalogContracts(t *testing.T) {
	want := map[string]string{
		"aws_ec2":             "amazon.aws.aws_ec2",
		"azure_rm":            "azure.azcollection.azure_rm",
		"gcp_compute":         "google.cloud.gcp_compute",
		"vmware_vm_inventory": "community.vmware.vmware_vm_inventory",
		"custom":              "",
	}
	got := List()
	if len(got) != len(want) {
		t.Fatalf("List() returned %d source types, want %d", len(got), len(want))
	}
	seen := map[string]bool{}
	for _, sourceType := range got {
		if seen[sourceType.ID] {
			t.Fatalf("duplicate source type ID %q", sourceType.ID)
		}
		seen[sourceType.ID] = true
		if sourceType.Version != Version || sourceType.Name == "" || sourceType.Description == "" || sourceType.Example == "" {
			t.Errorf("source type %q has incomplete metadata", sourceType.ID)
		}
		if sourceType.Plugin != want[sourceType.ID] {
			t.Errorf("source type %q plugin = %q, want %q", sourceType.ID, sourceType.Plugin, want[sourceType.ID])
		}
		if sourceType.ID != "custom" && len(sourceType.CompatibleCredentialTypes) == 0 {
			t.Errorf("source type %q has no compatible credential type", sourceType.ID)
		}
		if len(sourceType.ReconciliationOptions) != 2 {
			t.Errorf("source type %q has incomplete reconciliation options", sourceType.ID)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name, id, source string
		wantField        string
	}{
		{"aws", "aws_ec2", "plugin: amazon.aws.aws_ec2\nregions: [eu-west-1]", ""},
		{"azure", "azure_rm", "plugin: azure.azcollection.azure_rm", ""},
		{"gcp", "gcp_compute", "plugin: google.cloud.gcp_compute\nprojects: [example]", ""},
		{"vmware", "vmware_vm_inventory", "plugin: community.vmware.vmware_vm_inventory\nhostname: vcenter.example.invalid", ""},
		{"custom", "custom", "plugin: example.collection.custom", ""},
		{"unknown type", "openstack", "plugin: openstack.cloud.openstack", "source_type"},
		{"invalid yaml", "aws_ec2", "plugin: [", "source"},
		{"missing plugin", "aws_ec2", "regions: [eu-west-1]", "plugin"},
		{"wrong plugin", "aws_ec2", "plugin: community.aws.aws_ec2", "plugin"},
		{"missing required field", "gcp_compute", "plugin: google.cloud.gcp_compute", "projects"},
		{"invalid list field", "gcp_compute", "plugin: google.cloud.gcp_compute\nprojects: example", "projects"},
		{"invalid boolean field", "vmware_vm_inventory", "plugin: community.vmware.vmware_vm_inventory\nhostname: vcenter.example.invalid\nvalidate_certs: sometimes", "validate_certs"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			errors := Validate(test.id, test.source)
			if test.wantField == "" {
				if len(errors) != 0 {
					t.Fatalf("Validate() errors = %v", errors)
				}
				return
			}
			if len(errors) == 0 || errors[0].Field != test.wantField {
				t.Fatalf("Validate() errors = %v, want first field %q", errors, test.wantField)
			}
		})
	}
}
