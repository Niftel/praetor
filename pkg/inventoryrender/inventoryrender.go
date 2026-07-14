// Package inventoryrender builds an Ansible INI inventory from Praetor's stored
// hosts and groups. It lives outside the scheduler so ingestion can render an
// inventory on demand: the manifest ships only the inventory id (by reference),
// and the executor fetches the rendered INI at dispatch (like the credential
// resolve flow), keeping the outbox row / NATS message small (#13).
package inventoryrender

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/models"
	"github.com/praetordev/store"
)

// Render returns the INI inventory for an inventory's enabled hosts and groups.
// An inventory with no hosts renders to the empty string (the executor treats
// that as localhost). Group memberships are fetched in a single batched query.
//
// The reads go through the shared store (github.com/praetordev/store), the same
// data-access layer the API uses, so the explicit dispatch-path column lists live
// in exactly one place (#91) instead of being duplicated here.
func Render(ctx context.Context, db *sqlx.DB, inventoryID int64) (string, error) {
	hosts := store.NewHostStore(db)
	groups := store.NewGroupStore(db)

	hostList, err := hosts.ListEnabledByInventory(ctx, inventoryID)
	if err != nil {
		return "", fmt.Errorf("fetch hosts: %w", err)
	}
	groupList, err := groups.ListByInventory(ctx, inventoryID)
	if err != nil {
		return "", fmt.Errorf("fetch groups: %w", err)
	}

	groupIDs := make([]int64, len(groupList))
	for i, g := range groupList {
		groupIDs[i] = g.ID
	}
	membersByGroup, err := groups.MembershipsByGroups(ctx, groupIDs)
	if err != nil {
		return "", fmt.Errorf("fetch memberships: %w", err)
	}

	return build(hostList, groupList, membersByGroup), nil
}

// Facts returns the stored ansible_facts for every host in an inventory, keyed by
// host name — the fact cache the host-runner preloads when a template enables it.
// Nil when the inventory has no stored facts. Fetched by reference at dispatch
// (like the INI) so it doesn't bloat the outbox/NATS message (#48).
func Facts(ctx context.Context, db *sqlx.DB, inventoryID int64) (map[string]json.RawMessage, error) {
	return store.NewHostStore(db).FactsByInventory(ctx, inventoryID)
}

// build renders the INI from already-fetched data (pure; O(hosts+members)).
func build(hosts []models.Host, groups []models.Group, membersByGroup map[int64][]int64) string {
	var sb strings.Builder

	hostByID := make(map[int64]models.Host, len(hosts))
	ungrouped := make(map[int64]bool, len(hosts))
	for _, h := range hosts {
		hostByID[h.ID] = h
		ungrouped[h.ID] = true
	}

	for _, g := range groups {
		members := membersByGroup[g.ID]
		if len(members) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("[%s]\n", g.Name))
		for _, hostID := range members {
			if h, ok := hostByID[hostID]; ok {
				sb.WriteString(formatHostLine(h))
				delete(ungrouped, h.ID)
			}
		}
		sb.WriteString("\n")
	}

	if len(ungrouped) > 0 {
		sb.WriteString("[ungrouped]\n")
		for _, h := range hosts {
			if ungrouped[h.ID] {
				sb.WriteString(formatHostLine(h))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatHostLine renders one host line: the name plus its connection vars, always
// forcing ControlMaster=no (a shared SSH control socket crashes in containers).
func formatHostLine(h models.Host) string {
	var sb strings.Builder
	sb.WriteString(h.Name)

	var vars map[string]interface{}
	if h.Variables != nil {
		_ = json.Unmarshal(h.Variables, &vars)
	}
	if vars == nil {
		vars = make(map[string]interface{})
	}
	if val, ok := vars["ansible_ssh_common_args"]; ok {
		vars["ansible_ssh_common_args"] = fmt.Sprintf("%v -o ControlMaster=no", val)
	} else {
		vars["ansible_ssh_common_args"] = "-o StrictHostKeyChecking=no -o ControlMaster=no"
	}

	for k, v := range vars {
		strVal := fmt.Sprintf("%v", v)
		if strings.Contains(strVal, " ") {
			sb.WriteString(fmt.Sprintf(" %s=\"%s\"", k, strVal))
		} else {
			sb.WriteString(fmt.Sprintf(" %s=%s", k, strVal))
		}
	}

	sb.WriteString("\n")
	return sb.String()
}
