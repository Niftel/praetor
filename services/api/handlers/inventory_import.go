package handlers

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/render"
	"gopkg.in/yaml.v3"
)

// ImportInventoryRequest contains the import data
type ImportInventoryRequest struct {
	Content string `json:"content"` // File content as string
	Format  string `json:"format"`  // "ini" or "yaml"
}

// ImportInventoryResponse returns the import results
type ImportInventoryResponse struct {
	HostsCreated  int      `json:"hosts_created"`
	GroupsCreated int      `json:"groups_created"`
	Errors        []string `json:"errors,omitempty"`
}

// ImportInventory POST /api/v1/inventories/{inventoryId}/import
func (rs *InventoriesResource) ImportInventory(w http.ResponseWriter, r *http.Request) {
	inventoryIdStr := chi.URLParam(r, "inventoryId")
	inventoryId, err := parseInt64(inventoryIdStr)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Importing hosts/groups mutates the inventory.
	if !rs.authorize(w, r, rbac.ContentTypeInventory, inventoryId, actAdmin) {
		return
	}

	var input ImportInventoryRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var hosts []string
	var groups map[string][]string
	var parseErrors []string

	switch strings.ToLower(input.Format) {
	case "ini":
		hosts, groups, parseErrors = parseINIInventory(input.Content)
	case "yaml", "yml":
		hosts, groups, parseErrors = parseYAMLInventory(input.Content)
	default:
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	response := ImportInventoryResponse{
		Errors: parseErrors,
	}

	// Create hosts
	hostIDs := make(map[string]int64)
	for _, hostname := range hosts {
		// Check if host already exists
		if existingHost, err := rs.store.HostByName(r.Context(), inventoryId, hostname); err == nil {
			hostIDs[hostname] = existingHost.ID
			continue
		}

		// Create new host
		createdHost, err := rs.store.CreateImportHost(r.Context(), inventoryId, hostname)
		if err != nil {
			response.Errors = append(response.Errors, "Failed to create host: "+hostname)
			continue
		}
		hostIDs[hostname] = createdHost.ID
		response.HostsCreated++
	}

	// Create groups and assign hosts
	for groupName, groupHosts := range groups {
		// Check if group already exists
		var groupID int64
		if existingGroup, err := rs.store.GroupByName(r.Context(), inventoryId, groupName); err == nil {
			groupID = existingGroup.ID
		} else {
			// Create new group
			createdGroup, err := rs.store.CreateImportGroup(r.Context(), inventoryId, groupName)
			if err != nil {
				response.Errors = append(response.Errors, "Failed to create group: "+groupName)
				continue
			}
			groupID = createdGroup.ID
			response.GroupsCreated++
		}

		// Assign hosts to group
		for _, hostname := range groupHosts {
			hostID, ok := hostIDs[hostname]
			if !ok {
				continue
			}
			_ = rs.store.LinkHostGroup(r.Context(), hostID, groupID)
		}
	}

	render.JSON(w, r, response)
}

// parseINIInventory parses Ansible INI-style inventory
func parseINIInventory(content string) (hosts []string, groups map[string][]string, errors []string) {
	groups = make(map[string][]string)
	hostSet := make(map[string]bool)
	currentGroup := ""

	// Regex patterns
	groupPattern := regexp.MustCompile(`^\[([^\]:]+)\]`)
	hostPattern := regexp.MustCompile(`^([a-zA-Z0-9\-_.]+)`)

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Check for group header
		if match := groupPattern.FindStringSubmatch(line); match != nil {
			groupName := match[1]
			// Skip special groups like :vars, :children
			if strings.Contains(groupName, ":") {
				currentGroup = ""
				continue
			}
			currentGroup = groupName
			if _, exists := groups[currentGroup]; !exists {
				groups[currentGroup] = []string{}
			}
			continue
		}

		// Parse host line
		if match := hostPattern.FindStringSubmatch(line); match != nil {
			hostname := match[1]
			if !hostSet[hostname] {
				hosts = append(hosts, hostname)
				hostSet[hostname] = true
			}
			if currentGroup != "" {
				groups[currentGroup] = append(groups[currentGroup], hostname)
			}
		}
	}

	return hosts, groups, errors
}

// parseYAMLInventory parses Ansible YAML-style inventory
func parseYAMLInventory(content string) (hosts []string, groups map[string][]string, errors []string) {
	groups = make(map[string][]string)
	hostSet := make(map[string]bool)

	var inventory map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &inventory); err != nil {
		errors = append(errors, "Failed to parse YAML: "+err.Error())
		return
	}

	// Traverse the inventory structure
	var parseGroup func(name string, data interface{})
	parseGroup = func(name string, data interface{}) {
		groupData, ok := data.(map[string]interface{})
		if !ok {
			return
		}

		// Parse hosts in this group
		if hostsData, ok := groupData["hosts"]; ok {
			if hostsMap, ok := hostsData.(map[string]interface{}); ok {
				for hostname := range hostsMap {
					if !hostSet[hostname] {
						hosts = append(hosts, hostname)
						hostSet[hostname] = true
					}
					if name != "all" {
						groups[name] = append(groups[name], hostname)
					}
				}
			}
		}

		// Parse children groups
		if childrenData, ok := groupData["children"]; ok {
			if childrenMap, ok := childrenData.(map[string]interface{}); ok {
				for childName, childData := range childrenMap {
					parseGroup(childName, childData)
				}
			}
		}
	}

	// Start from "all" group or root level
	if allData, ok := inventory["all"]; ok {
		parseGroup("all", allData)
	} else {
		// Try to parse as direct group structure
		for groupName, groupData := range inventory {
			parseGroup(groupName, groupData)
		}
	}

	return hosts, groups, errors
}

func parseInt64(s string) (int64, error) {
	var id int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			id = id*10 + int64(c-'0')
		} else {
			break
		}
	}
	if id == 0 && s != "0" {
		return 0, io.EOF
	}
	return id, nil
}
