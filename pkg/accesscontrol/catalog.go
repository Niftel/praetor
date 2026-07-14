package accesscontrol

import (
	"fmt"
	"sort"
	"strings"
)

var verbsByResource = map[ResourceKind][]Verb{
	Organization:     {View, Add, Change, Delete, Manage},
	Team:             {View, Add, Change, Delete, Manage},
	Project:          {View, Add, Change, Delete, Manage, Use, Update},
	Inventory:        {View, Add, Change, Delete, Manage, Use, Update, Adhoc},
	Credential:       {View, Add, Change, Delete, Manage, Use},
	JobTemplate:      {View, Add, Change, Delete, Manage, Execute},
	WorkflowTemplate: {View, Add, Change, Delete, Manage, Execute, Approve},
}

var resourceOrder = []ResourceKind{
	Organization, Team, Project, Inventory, Credential, JobTemplate, WorkflowTemplate,
}

func Capability(kind ResourceKind, verb Verb) string {
	return string(verb) + "_" + string(kind)
}

func IsCapability(kind ResourceKind, verb Verb) bool {
	for _, candidate := range verbsByResource[kind] {
		if candidate == verb {
			return true
		}
	}
	return false
}

type CapabilityDefinition struct {
	Codename     string
	ResourceKind ResourceKind
	Verb         Verb
	Label        string
}

const (
	ManageUsers          = "manage_user"
	ViewActivityStream   = "view_activitystream"
	ManageExecutionPacks = "manage_executionpack"
	ManageCredentialType = "manage_credentialtype"
	ManageEventSources   = "manage_eventsource"
)

var globalCapabilities = []CapabilityDefinition{
	{ManageUsers, "user", Manage, "Manage users"},
	{ViewActivityStream, "activity_stream", View, "View activity stream"},
	{ManageExecutionPacks, "execution_pack", Manage, "Manage execution packs"},
	{ManageCredentialType, "credential_type", Manage, "Manage credential types"},
	{ManageEventSources, "event_source", Manage, "Manage event sources"},
}

func Catalog() []CapabilityDefinition {
	definitions := make([]CapabilityDefinition, 0, 54)
	for _, kind := range resourceOrder {
		for _, verb := range verbsByResource[kind] {
			word := string(verb)
			label := strings.ToUpper(word[:1]) + word[1:] + " " + strings.ReplaceAll(string(kind), "_", " ")
			definitions = append(definitions, CapabilityDefinition{Capability(kind, verb), kind, verb, label})
		}
	}
	definitions = append(definitions, globalCapabilities...)
	return definitions
}

func ResourceKinds() []ResourceKind {
	out := append([]ResourceKind(nil), resourceOrder...)
	return out
}

func ValidateCatalog() error {
	seen := make(map[string]struct{})
	for _, definition := range Catalog() {
		if definition.Codename == "" {
			return fmt.Errorf("empty capability codename")
		}
		if _, duplicate := seen[definition.Codename]; duplicate {
			return fmt.Errorf("duplicate capability %q", definition.Codename)
		}
		seen[definition.Codename] = struct{}{}
	}
	return nil
}

func SortedCapabilities(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
