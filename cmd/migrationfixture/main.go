// Command migrationfixture prepares and verifies historical database states for
// the executable migration compatibility matrix. It is test tooling; production
// upgrades continue to run through cmd/migrator.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: migrationfixture <prepare VERSION|assert|rollback VERSION>")
	}
	db, err := sql.Open("postgres", requiredEnv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("connect database: %v", err)
	}

	switch os.Args[1] {
	case "prepare":
		version := requiredVersion()
		must(prepare(db, version))
	case "assert":
		must(assertUpgrade(db))
	case "rollback":
		version := requiredVersion()
		must(rollback(db, version))
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}

func requiredVersion() int {
	if len(os.Args) != 3 {
		log.Fatal("migration version is required")
	}
	version, err := strconv.Atoi(os.Args[2])
	if err != nil || version < 1 {
		log.Fatalf("invalid migration version %q", os.Args[2])
	}
	return version
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func prepare(db *sql.DB, target int) error {
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		return fmt.Errorf("reset schema: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	files, err := migrationFiles(".up.sql")
	if err != nil {
		return err
	}
	for _, name := range files {
		version, numbered := numberedVersion(name)
		if numbered && version > target {
			continue
		}
		if !numbered && name != "20240503000000_rbac_tables.up.sql" {
			continue
		}
		if err := executeFile(db, name); err != nil {
			return err
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES ($1)`, name); err != nil {
			return fmt.Errorf("record %s: %w", name, err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE migration_compat_fixture (
		id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
		starting_version INTEGER NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO migration_compat_fixture(starting_version) VALUES ($1)`, target); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO organizations(name, description)
		VALUES ('compatibility-sentinel', 'must survive supported upgrades')`); err != nil {
		return fmt.Errorf("seed organization: %w", err)
	}
	if _, err := db.Exec(`INSERT INTO users(username, password_hash, email, is_superuser, is_active)
		VALUES ('compatibility-user', 'not-a-login-secret', 'compatibility@example.invalid', false, true)`); err != nil {
		return fmt.Errorf("seed user: %w", err)
	}
	if _, err := db.Exec(`INSERT INTO credential_types(name, description, inputs, injectors)
		VALUES ('Compatibility Credential Type', 'must survive supported upgrades', '{}', '{}')`); err != nil {
		return fmt.Errorf("seed credential type: %w", err)
	}
	credentialInsert := `INSERT INTO credentials(
			organization_id, credential_type_id, name, description, inputs%s
		)
		SELECT o.id, ct.id, 'Compatibility Credential', 'must survive supported upgrades', '{}'::jsonb%s
		FROM organizations o, credential_types ct
		WHERE o.name='compatibility-sentinel' AND ct.name='Compatibility Credential Type'`
	columns, values := "", ""
	if target >= 63 {
		columns = ", secrets_service_id, secrets_service_version"
		values = ", '11111111-1111-4111-8111-111111111111'::uuid, 7"
	}
	if _, err := db.Exec(fmt.Sprintf(credentialInsert, columns, values)); err != nil {
		return fmt.Errorf("seed credential: %w", err)
	}
	if target >= 55 {
		if _, err := db.Exec(`INSERT INTO role_definitions(name, description, managed)
			VALUES ('Compatibility Sentinel Role', 'must survive supported upgrades', false)`); err != nil {
			return fmt.Errorf("seed role definition: %w", err)
		}
	}
	if target >= 66 {
		if _, err := db.Exec(`INSERT INTO service_principals(
				organization_id, name, description, enabled, created_by_user_id
			)
			SELECT o.id, 'Compatibility Service Principal', 'must survive supported upgrades',
			       true, u.id
			FROM organizations o, users u
			WHERE o.name='compatibility-sentinel' AND u.username='compatibility-user'`); err != nil {
			return fmt.Errorf("seed service principal: %w", err)
		}
	}
	log.Printf("prepared historical schema at migration %d", target)
	return nil
}

func assertUpgrade(db *sql.DB) error {
	var starting int
	if err := db.QueryRow(`SELECT starting_version FROM migration_compat_fixture WHERE id`).Scan(&starting); err != nil {
		return fmt.Errorf("load fixture state: %w", err)
	}
	checks := []struct {
		name  string
		query string
	}{
		{"organization sentinel", `SELECT EXISTS(SELECT 1 FROM organizations WHERE name='compatibility-sentinel')`},
		{"user sentinel", `SELECT EXISTS(SELECT 1 FROM users WHERE username='compatibility-user')`},
		{"credential sentinel", `SELECT EXISTS(
			SELECT 1 FROM credentials WHERE name='Compatibility Credential')`},
		{"latest migration", `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='000068_delegated_workflow_launches.up.sql')`},
		{"credential secret reference", `SELECT EXISTS(
			SELECT 1 FROM information_schema.columns
			WHERE table_schema='public' AND table_name='credentials' AND column_name='secrets_service_id')`},
		{"service principals", `SELECT to_regclass('public.service_principals') IS NOT NULL`},
		{"delegated launches", `SELECT to_regclass('public.delegated_launch_idempotency') IS NOT NULL`},
	}
	if starting >= 55 {
		checks = append(checks, struct {
			name  string
			query string
		}{"custom role definition", `SELECT EXISTS(
			SELECT 1 FROM role_definitions WHERE name='Compatibility Sentinel Role' AND managed=false)`})
	}
	if starting >= 63 {
		checks = append(checks, struct {
			name  string
			query string
		}{"credential reference data", `SELECT EXISTS(
			SELECT 1 FROM credentials
			WHERE name='Compatibility Credential'
			  AND secrets_service_id='11111111-1111-4111-8111-111111111111'
			  AND secrets_service_version=7)`})
	}
	if starting >= 66 {
		checks = append(checks, struct {
			name  string
			query string
		}{"service principal data", `SELECT EXISTS(
			SELECT 1 FROM service_principals WHERE name='Compatibility Service Principal' AND enabled)`})
	}
	for _, check := range checks {
		var ok bool
		if err := db.QueryRow(check.query).Scan(&ok); err != nil {
			return fmt.Errorf("%s: %w", check.name, err)
		}
		if !ok {
			return fmt.Errorf("%s did not survive migration from %d", check.name, starting)
		}
	}
	log.Printf("verified upgrade from migration %d", starting)
	return nil
}

func rollback(db *sql.DB, version int) error {
	matches, err := filepath.Glob(filepath.Join("db/migrations", fmt.Sprintf("%06d_*.down.sql", version)))
	if err != nil {
		return err
	}
	if len(matches) != 1 {
		return fmt.Errorf("rollback migration %d requires exactly one down file, found %d", version, len(matches))
	}
	name := filepath.Base(matches[0])
	if err := executeFile(db, name); err != nil {
		return err
	}
	up := strings.TrimSuffix(name, ".down.sql") + ".up.sql"
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=$1`, up); err != nil {
		return err
	}
	log.Printf("rolled back migration %d; cmd/migrator must reapply it", version)
	return nil
}

func migrationFiles(suffix string) ([]string, error) {
	entries, err := os.ReadDir("db/migrations")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), suffix) {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func numberedVersion(name string) (int, bool) {
	if len(name) < 7 || name[6] != '_' {
		return 0, false
	}
	version, err := strconv.Atoi(name[:6])
	return version, err == nil
}

func executeFile(db *sql.DB, name string) error {
	content, err := os.ReadFile(filepath.Join("db/migrations", name))
	if err != nil {
		return err
	}
	if _, err := db.Exec(string(content)); err != nil {
		return fmt.Errorf("execute %s: %w", name, err)
	}
	return nil
}
