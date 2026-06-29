package main

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/db"
)

func main() {
	log.Println("Starting migration...")

	database, err := db.InitDB()
	if err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}
	defer database.Close()

	// Track which migrations have run so each is applied exactly once. Without
	// this the migrator re-ran every file on every start; some are destructive
	// (e.g. 000011 drops and recreates the roles table), so rebuilds wiped role
	// data and dropped constraints.
	if _, err := database.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		log.Fatalf("Create schema_migrations failed: %v", err)
	}

	applied := loadApplied(database)

	// Discover migration files (sorted by name; duplicate numeric prefixes are
	// distinguished by their full filename).
	entries, err := os.ReadDir("db/migrations")
	if err != nil {
		log.Fatalf("Read dir failed: %v", err)
	}
	var ups []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			ups = append(ups, e.Name())
		}
	}
	sort.Strings(ups)

	// Baseline: a database already migrated by the previous (untracked)
	// migrator has the schema but no history. Record everything as applied
	// without re-running it, so we don't replay destructive migrations once.
	if len(applied) == 0 && tableExists(database, "organizations") {
		log.Println("Existing schema with no migration history detected; baselining (recording as applied without re-running).")
		for _, name := range ups {
			recordApplied(database, name)
		}
		seedCredentialTypes(database)
		log.Println("Migration complete (baselined).")
		return
	}

	for _, name := range ups {
		if applied[name] {
			continue
		}
		log.Printf("Applying %s...", name)
		content, err := os.ReadFile(filepath.Join("db/migrations", name))
		if err != nil {
			log.Fatalf("Read file %s failed: %v", name, err)
		}
		if _, err := database.Exec(string(content)); err != nil {
			log.Fatalf("Exec %s failed: %v", name, err)
		}
		recordApplied(database, name)
	}
	log.Println("Migration complete.")

	// Seed Credential Types (idempotent).
	seedCredentialTypes(database)
}

// loadApplied returns the set of migration versions already recorded.
func loadApplied(database *sqlx.DB) map[string]bool {
	var versions []string
	if err := database.Select(&versions, `SELECT version FROM schema_migrations`); err != nil {
		log.Fatalf("Load applied migrations failed: %v", err)
	}
	set := make(map[string]bool, len(versions))
	for _, v := range versions {
		set[v] = true
	}
	return set
}

func recordApplied(database *sqlx.DB, version string) {
	if _, err := database.Exec(
		`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT (version) DO NOTHING`, version,
	); err != nil {
		log.Fatalf("Record migration %s failed: %v", version, err)
	}
}

func tableExists(database *sqlx.DB, name string) bool {
	var exists bool
	if err := database.Get(&exists, `SELECT to_regclass($1) IS NOT NULL`, name); err != nil {
		return false
	}
	return exists
}

func seedCredentialTypes(db *sqlx.DB) {
	types := []struct {
		Name        string
		Description string
		Inputs      string
		Injectors   string
	}{
		{
			Name:        "Machine",
			Description: "SSH authentication for remote hosts",
			Inputs: `{
				"fields": [
					{"id": "username", "label": "Username", "type": "text"},
					{"id": "password", "label": "Password", "type": "password", "secret": true},
					{"id": "ssh_private_key", "label": "SSH Private Key", "type": "textarea", "secret": true}
				]
			}`,
			Injectors: `{
				"env": {
					"ANSIBLE_REMOTE_USER": "{{ username }}",
					"ANSIBLE_PASSWORD": "{{ password }}"
				},
				"file": {
					"ANSIBLE_PRIVATE_KEY_FILE": "{{ ssh_private_key }}"
				}
			}`,
		},
		{
			Name:        "Source Control",
			Description: "Authentication for Git repositories",
			Inputs: `{
				"fields": [
					{"id": "username", "label": "Username", "type": "text"},
					{"id": "password", "label": "Password/Token", "type": "password", "secret": true},
					{"id": "ssh_private_key", "label": "SSH Private Key", "type": "textarea", "secret": true}
				]
			}`,
			Injectors: `{
				"env": {
					"GIT_USERNAME": "{{ username }}",
					"GIT_PASSWORD": "{{ password }}"
				},
				"file": {
					"GIT_SSH_KEY": "{{ ssh_private_key }}"
				}
			}`,
		},
		{
			Name:        "Ansible Galaxy/Automation Hub API Token",
			Description: "Authentication for a private Ansible Galaxy or Automation Hub server",
			Inputs: `{
				"fields": [
					{"id": "url", "label": "Galaxy Server URL", "type": "text"},
					{"id": "auth_url", "label": "Auth Server URL", "type": "text"},
					{"id": "token", "label": "API Token", "type": "password", "secret": true}
				]
			}`,
			Injectors: `{}`,
		},
	}

	for _, t := range types {
		_, err := db.Exec(`
			INSERT INTO credential_types (name, description, inputs, injectors)
			VALUES ($1, $2, $3::jsonb, $4::jsonb)
			ON CONFLICT (name) DO NOTHING
		`, t.Name, t.Description, t.Inputs, t.Injectors)
		if err != nil {
			log.Printf("Failed to seed credential type %s: %v", t.Name, err)
		} else {
			log.Printf("Seeded credential type: %s", t.Name)
		}
	}
}
