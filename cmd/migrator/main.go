package main

import (
	"log"
	"os"
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

	// Read migrations dir
	files, err := os.ReadDir("db/migrations")
	if err != nil {
		log.Fatalf("Read dir failed: %v", err)
	}

	var ups []string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".up.sql") {
			ups = append(ups, "db/migrations/"+f.Name())
		}
	}
	sort.Strings(ups)

	for _, f := range ups {
		log.Printf("Applying %s...", f)
		content, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("Read file %s failed: %v", f, err)
		}

		if _, err := database.Exec(string(content)); err != nil {
			log.Fatalf("Exec %s failed: %v", f, err)
		}
	}
	log.Println("Migration complete.")

	// Seed Credential Types
	seedCredentialTypes(database)
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
