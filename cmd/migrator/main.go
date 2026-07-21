package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/praetordev/db"
	"github.com/praetordev/praetor/pkg/accesscontrol"
	"golang.org/x/crypto/bcrypt"
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
		seedRBACPermissions(database)
		seedManagedRoleDefinitions(database)
		seedBootstrapAdmin(database)
		seedSystemRoleAssignments(database)
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

	// Seed the DAB capability catalog + managed-mirror role definitions (idempotent).
	seedRBACPermissions(database)
	seedManagedRoleDefinitions(database)

	// Optionally ensure a break-glass local superuser (opt-in via env).
	seedBootstrapAdmin(database)

	// Sync global System Administrator / Auditor assignments from the user flags.
	seedSystemRoleAssignments(database)
}

// seedRBACPermissions upserts the DAB capability catalog (dab_permissions) from the
// canonical list in pkg/accesscontrol. Idempotent: run on every migrate, it inserts
// new capabilities and refreshes labels without disturbing existing rows or their ids
// (so role_definition_permissions references stay valid). No-ops cleanly if the table
// isn't present yet (e.g. a DB predating migration 000055).
func seedRBACPermissions(database *sqlx.DB) {
	if !tableExists(database, "dab_permissions") {
		return
	}
	catalog := accesscontrol.Catalog()
	for _, p := range catalog {
		if _, err := database.Exec(`
			INSERT INTO dab_permissions (codename, content_type, action, name)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (codename) DO UPDATE SET
				content_type = EXCLUDED.content_type,
				action = EXCLUDED.action,
				name = EXCLUDED.name`,
			p.Codename, string(p.ResourceKind), string(p.Verb), p.Label); err != nil {
			log.Printf("Failed to seed capability %s: %v", p.Codename, err)
		}
	}
	log.Printf("Seeded %d RBAC capabilities", len(catalog))
}

// seedManagedRoleDefinitions upserts Praetor's built-in RoleDefinitions from
// pkg/accesscontrol. Idempotent and id-stable (upsert by
// unique name keeps the row's id so role_definition_permissions references survive), and
// it refreshes each definition's permission set to exactly the declaration. Depends on
// dab_permissions being seeded first; no-ops if the tables are absent.
func seedManagedRoleDefinitions(database *sqlx.DB) {
	if !tableExists(database, "role_definitions") {
		return
	}
	roles := accesscontrol.BuiltinRoles()
	for _, mr := range roles {
		var ct interface{}
		if mr.ResourceKind != nil {
			ct = string(*mr.ResourceKind)
		}
		var defID int64
		if err := database.Get(&defID, `
			INSERT INTO role_definitions (name, description, managed, content_type)
			VALUES ($1, $2, true, $3)
			ON CONFLICT (name) DO UPDATE SET
				description = EXCLUDED.description,
				managed = true,
				content_type = EXCLUDED.content_type,
				modified_at = now()
			RETURNING id`, mr.Name, mr.Description, ct); err != nil {
			log.Printf("Failed to seed managed role %q: %v", mr.Name, err)
			continue
		}
		// Refresh the permission set to exactly the declaration.
		if _, err := database.Exec(
			`DELETE FROM role_definition_permissions WHERE role_definition_id = $1`, defID); err != nil {
			log.Printf("Managed role %q: clearing permissions failed: %v", mr.Name, err)
			continue
		}
		for _, cn := range mr.Capabilities {
			if _, err := database.Exec(`
				INSERT INTO role_definition_permissions (role_definition_id, permission_id)
				SELECT $1, p.id FROM dab_permissions p WHERE p.codename = $2
				ON CONFLICT DO NOTHING`, defID, cn); err != nil {
				log.Printf("Managed role %q: attaching %q failed: %v", mr.Name, cn, err)
			}
		}
	}
	log.Printf("Seeded %d managed role definitions", len(roles))
}

// seedSystemRoleAssignments keeps the global System Administrator / System Auditor
// RoleDefinition assignments in sync with the is_superuser / is_system_auditor user flags
// (Gitea #99 cleanup): the flags are the input, the global object-role assignment is the
// capability-model representation enforcement reads. Idempotent — assigns matching users
// and prunes the rest on every migrate.
func seedSystemRoleAssignments(database *sqlx.DB) {
	if !tableExists(database, "object_roles") || !tableExists(database, "role_definitions") {
		return
	}
	ctx := context.Background()
	syncSystemRole(ctx, database, "System Administrator", "is_superuser = true")
	syncSystemRole(ctx, database, "System Auditor", "COALESCE(is_system_auditor, false) = true AND is_superuser = false")
}

// syncSystemRole assigns the named global role to every user matching predicate (a trusted
// literal) and removes it from all others.
func syncSystemRole(ctx context.Context, database *sqlx.DB, defName, predicate string) {
	var ids []int64
	if err := database.Select(&ids, `SELECT id FROM users WHERE `+predicate); err != nil {
		log.Printf("system role %q: select users: %v", defName, err)
		return
	}
	for _, uid := range ids {
		if err := accesscontrol.SetGlobalUserRole(ctx, database, defName, uid, true); err != nil {
			log.Printf("system role %q: assign user %d: %v", defName, uid, err)
		}
	}
	if _, err := database.Exec(`
		DELETE FROM role_user_assignments ua USING object_roles orl, role_definitions d
		WHERE ua.object_role_id = orl.id AND orl.role_definition_id = d.id
		  AND d.name = $1 AND orl.content_type IS NULL
		  AND ua.user_id NOT IN (SELECT id FROM users WHERE `+predicate+`)`, defName); err != nil {
		log.Printf("system role %q: prune: %v", defName, err)
	}
	log.Printf("Synced %q global assignments (%d holders)", defName, len(ids))
}

// seedBootstrapAdmin ensures a break-glass LOCAL superuser exists when
// PRAETOR_BOOTSTRAP_ADMIN_USERNAME + _PASSWORD are set. This account authenticates
// locally (never via LDAP), so a misconfigured or unreachable directory can't lock
// everyone out. Idempotent, and it never clobbers an LDAP-managed row: the upsert
// only touches a row whose ldap_dn IS NULL.
func seedBootstrapAdmin(database *sqlx.DB) {
	username := os.Getenv("PRAETOR_BOOTSTRAP_ADMIN_USERNAME")
	password := os.Getenv("PRAETOR_BOOTSTRAP_ADMIN_PASSWORD")
	if username == "" || password == "" {
		return // opt-in only
	}
	email := os.Getenv("PRAETOR_BOOTSTRAP_ADMIN_EMAIL")

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Bootstrap admin: hashing password failed: %v", err)
		return
	}

	// Create the local superuser, or reset it to the configured password on
	// re-run — but ONLY when it's a local account (ldap_dn IS NULL). A same-named
	// LDAP user is left untouched (the ON CONFLICT WHERE guard skips it).
	res, err := database.Exec(`
		INSERT INTO users (username, password_hash, email, is_superuser, is_active)
		VALUES ($1, $2, NULLIF($3, ''), true, true)
		ON CONFLICT (username) DO UPDATE
			SET password_hash = EXCLUDED.password_hash,
			    is_superuser = true,
			    is_active = true,
			    modified_at = NOW()
			WHERE users.ldap_dn IS NULL`,
		username, string(hash), email)
	if err != nil {
		log.Printf("Bootstrap admin %q: %v", username, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		log.Printf("Bootstrap admin %q: a same-named LDAP user exists; left untouched", username)
		return
	}
	log.Printf("Bootstrap local superuser ensured: %s", username)
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
			Description: "SSH authentication and privilege escalation for remote hosts",
			Inputs: `{
				"fields": [
					{"id": "username", "label": "Username", "type": "text"},
					{"id": "password", "label": "Password", "type": "password", "secret": true},
					{"id": "ssh_private_key", "label": "SSH Private Key", "type": "textarea", "secret": true},
					{"id": "become_method", "label": "Privilege Escalation Method", "type": "text"},
					{"id": "become_username", "label": "Privilege Escalation Username", "type": "text"},
					{"id": "become_password", "label": "Privilege Escalation Password", "type": "password", "secret": true}
				]
			}`,
			Injectors: `{
				"env": {
					"ANSIBLE_REMOTE_USER": "{{ username }}",
					"ANSIBLE_PASSWORD": "{{ password }}",
					"ANSIBLE_BECOME_METHOD": "{{ become_method }}",
					"ANSIBLE_BECOME_USER": "{{ become_username }}"
				},
				"file": {
					"ANSIBLE_PRIVATE_KEY_FILE": "{{ ssh_private_key }}",
					"ANSIBLE_BECOME_PASSWORD_FILE": "{{ become_password }}"
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
		{
			Name:        "Amazon Web Services",
			Description: "Access keys for AWS dynamic inventory (aws_ec2) and modules",
			Inputs: `{
				"fields": [
					{"id": "access_key", "label": "Access Key ID", "type": "text"},
					{"id": "secret_key", "label": "Secret Access Key", "type": "password", "secret": true},
					{"id": "security_token", "label": "STS Session Token", "type": "password", "secret": true}
				]
			}`,
			Injectors: `{
				"env": {
					"AWS_ACCESS_KEY_ID": "{{ access_key }}",
					"AWS_SECRET_ACCESS_KEY": "{{ secret_key }}",
					"AWS_SECURITY_TOKEN": "{{ security_token }}"
				}
			}`,
		},
		{
			Name:        "Microsoft Azure Resource Manager",
			Description: "Service principal for Azure dynamic inventory (azure_rm) and modules",
			Inputs: `{
				"fields": [
					{"id": "client", "label": "Client ID", "type": "text"},
					{"id": "secret", "label": "Client Secret", "type": "password", "secret": true},
					{"id": "tenant", "label": "Tenant ID", "type": "text"},
					{"id": "subscription", "label": "Subscription ID", "type": "text"}
				]
			}`,
			Injectors: `{
				"env": {
					"AZURE_CLIENT_ID": "{{ client }}",
					"AZURE_SECRET": "{{ secret }}",
					"AZURE_TENANT": "{{ tenant }}",
					"AZURE_SUBSCRIPTION_ID": "{{ subscription }}"
				}
			}`,
		},
		{
			Name:        "Google Compute Engine",
			Description: "Service account JSON for GCP dynamic inventory (gcp_compute) and modules",
			Inputs: `{
				"fields": [
					{"id": "service_account_content", "label": "Service Account JSON", "type": "textarea", "secret": true}
				]
			}`,
			Injectors: `{
				"env": {
					"GCP_AUTH_KIND": "serviceaccount"
				},
				"file": {
					"GCP_SERVICE_ACCOUNT_FILE": "{{ service_account_content }}"
				}
			}`,
		},
		{
			Name:        "VMware vCenter",
			Description: "Username and password for VMware vCenter dynamic inventory",
			Inputs: `{
				"fields": [
					{"id": "username", "label": "Username", "type": "text"},
					{"id": "password", "label": "Password", "type": "password", "secret": true}
				]
			}`,
			Injectors: `{
				"env": {
					"VMWARE_USER": "{{ username }}",
					"VMWARE_PASSWORD": "{{ password }}"
				}
			}`,
		},
	}

	for _, t := range types {
		// Upsert: the built-in types are system-managed, so re-seeding keeps their
		// inputs/injectors current (e.g. adding become fields to Machine) on every
		// migrator run rather than only on first insert.
		_, err := db.Exec(`
			INSERT INTO credential_types (name, description, inputs, injectors, managed)
			VALUES ($1, $2, $3::jsonb, $4::jsonb, true)
			ON CONFLICT (name) DO UPDATE SET
				description = EXCLUDED.description,
				inputs = EXCLUDED.inputs,
				injectors = EXCLUDED.injectors,
				managed = true,
				modified_at = now()
		`, t.Name, t.Description, t.Inputs, t.Injectors)
		if err != nil {
			log.Printf("Failed to seed credential type %s: %v", t.Name, err)
		} else {
			log.Printf("Seeded credential type: %s", t.Name)
		}
	}
}
