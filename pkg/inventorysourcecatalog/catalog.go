// Package inventorysourcecatalog defines the server-owned contract for
// supported dynamic inventory sources. It contains metadata and validation
// only; provider execution remains the responsibility of ansible-inventory.
package inventorysourcecatalog

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const Version = "v1"

type Field struct {
	ID          string      `json:"id"`
	Label       string      `json:"label"`
	Type        string      `json:"type"`
	Required    bool        `json:"required"`
	Description string      `json:"description,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

type ReconciliationOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
}

type SourceType struct {
	ID                        string                 `json:"id"`
	Version                   string                 `json:"version"`
	Name                      string                 `json:"name"`
	Description               string                 `json:"description"`
	Plugin                    string                 `json:"plugin,omitempty"`
	Enabled                   bool                   `json:"enabled"`
	Advanced                  bool                   `json:"advanced"`
	Fields                    []Field                `json:"fields"`
	CompatibleCredentialTypes []string               `json:"compatible_credential_types"`
	ReconciliationOptions     []ReconciliationOption `json:"reconciliation_options"`
	Example                   string                 `json:"example"`
}

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

var reconciliationOptions = []ReconciliationOption{
	{ID: "disable", Label: "Disable missing hosts", Description: "Disable hosts no longer returned by the source without deleting their history.", Default: true},
	{ID: "retain", Label: "Retain missing hosts", Description: "Keep hosts that are no longer returned by the source enabled.", Default: false},
}

var sourceTypes = []SourceType{
	{
		ID: "aws_ec2", Version: Version, Name: "Amazon EC2", Enabled: true,
		Description: "Discover EC2 instances with the amazon.aws.aws_ec2 inventory plugin.",
		Plugin:      "amazon.aws.aws_ec2", CompatibleCredentialTypes: []string{"Amazon Web Services"},
		Fields: []Field{
			{ID: "regions", Label: "Regions", Type: "string_list", Required: false, Description: "AWS regions to query; empty uses the provider default."},
			{ID: "hostnames", Label: "Host name rules", Type: "string_list", Required: false, Description: "Ordered EC2 attributes used to name hosts."},
		},
		ReconciliationOptions: reconciliationOptions,
		Example:               "plugin: amazon.aws.aws_ec2\nregions:\n  - eu-west-1\n",
	},
	{
		ID: "azure_rm", Version: Version, Name: "Microsoft Azure", Enabled: true,
		Description: "Discover Azure virtual machines with the azure.azcollection.azure_rm inventory plugin.",
		Plugin:      "azure.azcollection.azure_rm", CompatibleCredentialTypes: []string{"Microsoft Azure Resource Manager"},
		Fields: []Field{
			{ID: "include_vm_resource_groups", Label: "Resource groups", Type: "string_list", Required: false, Description: "Limit discovery to named resource groups."},
			{ID: "plain_host_names", Label: "Use plain host names", Type: "boolean", Required: false, Default: false},
		},
		ReconciliationOptions: reconciliationOptions,
		Example:               "plugin: azure.azcollection.azure_rm\ninclude_vm_resource_groups:\n  - example-group\n",
	},
	{
		ID: "gcp_compute", Version: Version, Name: "Google Compute Engine", Enabled: true,
		Description: "Discover Compute Engine instances with the google.cloud.gcp_compute inventory plugin.",
		Plugin:      "google.cloud.gcp_compute", CompatibleCredentialTypes: []string{"Google Compute Engine"},
		Fields: []Field{
			{ID: "projects", Label: "Projects", Type: "string_list", Required: true, Description: "Google Cloud project IDs to query."},
			{ID: "zones", Label: "Zones", Type: "string_list", Required: false, Description: "Optional Compute Engine zones."},
		},
		ReconciliationOptions: reconciliationOptions,
		Example:               "plugin: google.cloud.gcp_compute\nprojects:\n  - example-project\nauth_kind: serviceaccount\n",
	},
	{
		ID: "vmware_vm_inventory", Version: Version, Name: "VMware vCenter", Enabled: true,
		Description: "Discover vCenter virtual machines with the community.vmware.vmware_vm_inventory plugin.",
		Plugin:      "community.vmware.vmware_vm_inventory", CompatibleCredentialTypes: []string{"VMware vCenter"},
		Fields: []Field{
			{ID: "hostname", Label: "vCenter host", Type: "string", Required: true, Description: "vCenter hostname; authentication is supplied by the selected credential."},
			{ID: "validate_certs", Label: "Validate TLS certificates", Type: "boolean", Required: false, Default: true},
		},
		ReconciliationOptions: reconciliationOptions,
		Example:               "plugin: community.vmware.vmware_vm_inventory\nhostname: vcenter.example.invalid\nvalidate_certs: true\n",
	},
	{
		ID: "custom", Version: Version, Name: "Custom Ansible inventory", Enabled: true, Advanced: true,
		Description:               "Advanced raw inventory plugin configuration. Validation permits supported Ansible plugin configuration but does not execute it.",
		Fields:                    []Field{{ID: "source", Label: "Inventory source", Type: "yaml", Required: true, Description: "Raw Ansible inventory plugin YAML."}},
		CompatibleCredentialTypes: []string{}, ReconciliationOptions: reconciliationOptions,
		Example: "plugin: namespace.collection.inventory_plugin\n",
	},
}

func List() []SourceType {
	out := make([]SourceType, 0, len(sourceTypes))
	for _, sourceType := range sourceTypes {
		if sourceType.Enabled {
			out = append(out, sourceType)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func Get(id string) (SourceType, bool) {
	for _, sourceType := range sourceTypes {
		if sourceType.Enabled && sourceType.ID == id {
			return sourceType, true
		}
	}
	return SourceType{}, false
}

func SupportsCredentialType(sourceType SourceType, credentialType string) bool {
	if len(sourceType.CompatibleCredentialTypes) == 0 {
		return true
	}
	for _, allowed := range sourceType.CompatibleCredentialTypes {
		if credentialType == allowed {
			return true
		}
	}
	return false
}

// Validate checks catalog identity, YAML shape, the curated plugin identifier,
// and required catalog fields. It deliberately does not execute plugin code.
func Validate(id, source string) []FieldError {
	sourceType, ok := Get(id)
	if !ok {
		return []FieldError{{Field: "source_type", Code: "unsupported", Message: fmt.Sprintf("unsupported inventory source type %q", id)}}
	}
	if strings.TrimSpace(source) == "" {
		return []FieldError{{Field: "source", Code: "required", Message: "inventory source YAML is required"}}
	}
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(source), &config); err != nil {
		return []FieldError{{Field: "source", Code: "invalid_yaml", Message: "inventory source must be valid YAML"}}
	}
	if config == nil {
		return []FieldError{{Field: "source", Code: "invalid_type", Message: "inventory source must be a YAML object"}}
	}
	plugin, _ := config["plugin"].(string)
	if plugin == "" {
		return []FieldError{{Field: "plugin", Code: "required", Message: "plugin is required"}}
	}
	if !sourceType.Advanced && plugin != sourceType.Plugin {
		return []FieldError{{Field: "plugin", Code: "unsupported", Message: fmt.Sprintf("source type %q requires plugin %q", id, sourceType.Plugin)}}
	}
	errors := make([]FieldError, 0)
	for _, field := range sourceType.Fields {
		if field.ID == "source" {
			continue
		}
		value, exists := config[field.ID]
		if field.Required && (!exists || value == nil || value == "") {
			errors = append(errors, FieldError{Field: field.ID, Code: "required", Message: field.Label + " is required"})
			continue
		}
		if !exists || value == nil {
			continue
		}
		validType := true
		switch field.Type {
		case "string":
			_, validType = value.(string)
		case "boolean":
			_, validType = value.(bool)
		case "string_list":
			items, ok := value.([]interface{})
			validType = ok
			for _, item := range items {
				if _, ok := item.(string); !ok {
					validType = false
				}
			}
		}
		if !validType {
			errors = append(errors, FieldError{Field: field.ID, Code: "invalid_type", Message: field.Label + " has an invalid value type"})
		}
	}
	return errors
}
