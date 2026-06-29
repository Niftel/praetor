package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// Syncer synchronizes LDAP entries with the database.
type Syncer struct {
	client *LDAPClient
	db     *sqlx.DB
	config *LDAPConfig
}

// NewSyncer creates a new LDAP syncer.
func NewSyncer(client *LDAPClient, db *sqlx.DB, config *LDAPConfig) *Syncer {
	return &Syncer{
		client: client,
		db:     db,
		config: config,
	}
}

// SyncAll performs a full synchronization of all entities.
func (s *Syncer) SyncAll(ctx context.Context) (*LDAPSyncResult, error) {
	result := &LDAPSyncResult{
		SyncType:  "full",
		StartedAt: time.Now(),
		Status:    "running",
	}

	// Connect to LDAP
	if err := s.client.Connect(); err != nil {
		result.Status = "failed"
		result.Errors = append(result.Errors, err.Error())
		result.FinishedAt = time.Now()
		return result, err
	}
	defer s.client.Close()

	if err := s.client.Bind(); err != nil {
		result.Status = "failed"
		result.Errors = append(result.Errors, err.Error())
		result.FinishedAt = time.Now()
		return result, err
	}

	// Sync organizations first (teams depend on them)
	if s.config.Organizations.Enabled {
		orgResult, err := s.syncOrganizations(ctx)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("organizations: %v", err))
		}
		result.ItemsProcessed += orgResult.ItemsProcessed
		result.ItemsCreated += orgResult.ItemsCreated
		result.ItemsUpdated += orgResult.ItemsUpdated
		result.ItemsFailed += orgResult.ItemsFailed
		result.Items = append(result.Items, orgResult.Items...)
	}

	// Sync users
	userResult, err := s.syncUsers(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("users: %v", err))
	}
	result.ItemsProcessed += userResult.ItemsProcessed
	result.ItemsCreated += userResult.ItemsCreated
	result.ItemsUpdated += userResult.ItemsUpdated
	result.ItemsFailed += userResult.ItemsFailed
	result.Items = append(result.Items, userResult.Items...)

	// Sync teams
	if s.config.Teams.Enabled {
		teamResult, err := s.syncTeams(ctx)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("teams: %v", err))
		}
		result.ItemsProcessed += teamResult.ItemsProcessed
		result.ItemsCreated += teamResult.ItemsCreated
		result.ItemsUpdated += teamResult.ItemsUpdated
		result.ItemsFailed += teamResult.ItemsFailed
		result.Items = append(result.Items, teamResult.Items...)
	}

	result.FinishedAt = time.Now()
	if len(result.Errors) > 0 {
		result.Status = "partial"
	} else {
		result.Status = "success"
	}

	// Log sync result
	if _, err := s.logSyncResult(result); err != nil {
		log.Printf("failed to log sync result: %v", err)
	}

	return result, nil
}

// SyncUsers synchronizes users from LDAP to the database.
func (s *Syncer) SyncUsers(ctx context.Context) (*LDAPSyncResult, error) {
	if err := s.client.Connect(); err != nil {
		return nil, err
	}
	defer s.client.Close()

	if err := s.client.Bind(); err != nil {
		return nil, err
	}

	return s.syncUsers(ctx)
}

func (s *Syncer) syncUsers(ctx context.Context) (*LDAPSyncResult, error) {
	result := &LDAPSyncResult{
		SyncType:  "users",
		StartedAt: time.Now(),
	}

	entries, err := s.client.SearchUsers()
	if err != nil {
		result.Status = "failed"
		result.Errors = append(result.Errors, err.Error())
		result.FinishedAt = time.Now()
		return result, err
	}

	for _, entry := range entries {
		result.ItemsProcessed++

		username := entry.GetAttribute(s.config.Users.Attributes.Username)
		if username == "" {
			result.ItemsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("entry %s: missing username attribute", entry.DN))
			continue
		}

		email := entry.GetAttribute(s.config.Users.Attributes.Email)
		firstName := entry.GetAttribute(s.config.Users.Attributes.FirstName)
		lastName := entry.GetAttribute(s.config.Users.Attributes.LastName)

		if s.config.Sync.DryRun {
			log.Printf("[DRY RUN] Would sync user: %s (email=%s, firstName=%s, lastName=%s)",
				username, email, firstName, lastName)
			continue
		}

		created, updated, err := s.upsertUser(ctx, entry.DN, username, email, firstName, lastName)
		if err != nil {
			result.ItemsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("user %s: %v", username, err))
			errMsg := err.Error()
			result.Items = append(result.Items, LDAPSyncItem{
				EntityType:     "user",
				EntityName:     username,
				LdapDN:         entry.DN,
				LdapAttributes: entry.Attributes,
				Action:         "failed",
				ErrorMessage:   &errMsg,
			})
			continue
		}

		action := "unchanged"
		if created {
			result.ItemsCreated++
			action = "created"
		} else if updated {
			result.ItemsUpdated++
			action = "updated"
		}
		result.Items = append(result.Items, LDAPSyncItem{
			EntityType:     "user",
			EntityName:     username,
			LdapDN:         entry.DN,
			LdapAttributes: entry.Attributes,
			Action:         action,
		})
	}

	result.Status = "success"
	result.FinishedAt = time.Now()
	return result, nil
}

// SyncOrganizations synchronizes organizations from LDAP to the database.
func (s *Syncer) SyncOrganizations(ctx context.Context) (*LDAPSyncResult, error) {
	if err := s.client.Connect(); err != nil {
		return nil, err
	}
	defer s.client.Close()

	if err := s.client.Bind(); err != nil {
		return nil, err
	}

	return s.syncOrganizations(ctx)
}

func (s *Syncer) syncOrganizations(ctx context.Context) (*LDAPSyncResult, error) {
	result := &LDAPSyncResult{
		SyncType:  "organizations",
		StartedAt: time.Now(),
	}

	entries, err := s.client.SearchOrganizations()
	if err != nil {
		result.Status = "failed"
		result.Errors = append(result.Errors, err.Error())
		result.FinishedAt = time.Now()
		return result, err
	}

	for _, entry := range entries {
		result.ItemsProcessed++

		name := entry.GetAttribute(s.config.Organizations.Attributes.Name)
		if name == "" {
			result.ItemsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("entry %s: missing name attribute", entry.DN))
			continue
		}

		description := entry.GetAttribute(s.config.Organizations.Attributes.Description)

		if s.config.Sync.DryRun {
			log.Printf("[DRY RUN] Would sync organization: %s (description=%s)", name, description)
			continue
		}

		created, updated, err := s.upsertOrganization(ctx, entry.DN, name, description)
		if err != nil {
			result.ItemsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("org %s: %v", name, err))
			errMsg := err.Error()
			result.Items = append(result.Items, LDAPSyncItem{
				EntityType:     "organization",
				EntityName:     name,
				LdapDN:         entry.DN,
				LdapAttributes: entry.Attributes,
				Action:         "failed",
				ErrorMessage:   &errMsg,
			})
			continue
		}

		action := "unchanged"
		if created {
			result.ItemsCreated++
			action = "created"
		} else if updated {
			result.ItemsUpdated++
			action = "updated"
		}
		result.Items = append(result.Items, LDAPSyncItem{
			EntityType:     "organization",
			EntityName:     name,
			LdapDN:         entry.DN,
			LdapAttributes: entry.Attributes,
			Action:         action,
		})
	}

	result.Status = "success"
	result.FinishedAt = time.Now()
	return result, nil
}

// SyncTeams synchronizes teams from LDAP to the database.
func (s *Syncer) SyncTeams(ctx context.Context) (*LDAPSyncResult, error) {
	if err := s.client.Connect(); err != nil {
		return nil, err
	}
	defer s.client.Close()

	if err := s.client.Bind(); err != nil {
		return nil, err
	}

	return s.syncTeams(ctx)
}

func (s *Syncer) syncTeams(ctx context.Context) (*LDAPSyncResult, error) {
	result := &LDAPSyncResult{
		SyncType:  "teams",
		StartedAt: time.Now(),
	}

	entries, err := s.client.SearchTeams()
	if err != nil {
		result.Status = "failed"
		result.Errors = append(result.Errors, err.Error())
		result.FinishedAt = time.Now()
		return result, err
	}

	for _, entry := range entries {
		result.ItemsProcessed++

		name := entry.GetAttribute(s.config.Teams.Attributes.Name)
		if name == "" {
			result.ItemsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("entry %s: missing name attribute", entry.DN))
			continue
		}

		description := entry.GetAttribute(s.config.Teams.Attributes.Description)
		orgName := entry.GetAttribute(s.config.Teams.OrganizationAttribute)

		if s.config.Sync.DryRun {
			log.Printf("[DRY RUN] Would sync team: %s (org=%s, description=%s)", name, orgName, description)
			continue
		}

		created, updated, err := s.upsertTeam(ctx, entry.DN, name, description, orgName)
		if err != nil {
			result.ItemsFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("team %s: %v", name, err))
			errMsg := err.Error()
			result.Items = append(result.Items, LDAPSyncItem{
				EntityType:     "team",
				EntityName:     name,
				LdapDN:         entry.DN,
				LdapAttributes: entry.Attributes,
				Action:         "failed",
				ErrorMessage:   &errMsg,
			})
			continue
		}

		action := "unchanged"
		if created {
			result.ItemsCreated++
			action = "created"
		} else if updated {
			result.ItemsUpdated++
			action = "updated"
		}
		result.Items = append(result.Items, LDAPSyncItem{
			EntityType:     "team",
			EntityName:     name,
			LdapDN:         entry.DN,
			LdapAttributes: entry.Attributes,
			Action:         action,
		})

		// Sync team members
		members := entry.GetAttributes(s.config.Teams.MemberAttribute)
		if err := s.syncTeamMembers(ctx, name, members); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("team %s members: %v", name, err))
		}
	}

	result.Status = "success"
	result.FinishedAt = time.Now()
	return result, nil
}

// upsertUser creates or updates a user in the database.
// Returns: created (new), updated (changed), or unchanged (skip)
func (s *Syncer) upsertUser(ctx context.Context, dn, username, email, firstName, lastName string) (created bool, updated bool, err error) {
	// Check if user exists and get current values
	var existing struct {
		ID        int64   `db:"id"`
		Email     *string `db:"email"`
		FirstName *string `db:"first_name"`
		LastName  *string `db:"last_name"`
		LdapDN    *string `db:"ldap_dn"`
	}
	err = s.db.GetContext(ctx, &existing,
		`SELECT id, email, first_name, last_name, ldap_dn FROM users WHERE username = $1 OR ldap_dn = $2 LIMIT 1`,
		username, dn)

	if err == sql.ErrNoRows {
		// Create new user
		if !s.config.Sync.CreateUsers {
			return false, false, nil
		}

		// LDAP users have no local password (they auth via LDAP)
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO users (username, password_hash, email, first_name, last_name, ldap_dn, ldap_synced_at)
			VALUES ($1, '', $2, $3, $4, $5, NOW())`,
			username, nullString(email), nullString(firstName), nullString(lastName), dn)
		if err != nil {
			return false, false, fmt.Errorf("failed to create user: %w", err)
		}
		return true, false, nil
	}

	if err != nil {
		return false, false, fmt.Errorf("failed to check existing user: %w", err)
	}

	// Check if anything actually changed
	if ptrEquals(existing.Email, email) &&
		ptrEquals(existing.FirstName, firstName) &&
		ptrEquals(existing.LastName, lastName) &&
		ptrEquals(existing.LdapDN, dn) {
		// Only update ldap_synced_at timestamp
		_, _ = s.db.ExecContext(ctx, `UPDATE users SET ldap_synced_at = NOW() WHERE id = $1`, existing.ID)
		return false, false, nil // unchanged
	}

	// Update existing user
	_, err = s.db.ExecContext(ctx, `
		UPDATE users 
		SET email = $2, first_name = $3, last_name = $4, ldap_dn = $5, ldap_synced_at = NOW(), modified_at = NOW()
		WHERE id = $1`,
		existing.ID, nullString(email), nullString(firstName), nullString(lastName), dn)
	if err != nil {
		return false, false, fmt.Errorf("failed to update user: %w", err)
	}

	return false, true, nil
}

// upsertOrganization creates or updates an organization in the database.
func (s *Syncer) upsertOrganization(ctx context.Context, dn, name, description string) (created bool, updated bool, err error) {
	// Check if org exists and get current values
	var existing struct {
		ID          int64   `db:"id"`
		Description *string `db:"description"`
		LdapDN      *string `db:"ldap_dn"`
	}
	err = s.db.GetContext(ctx, &existing,
		`SELECT id, description, ldap_dn FROM organizations WHERE name = $1 OR ldap_dn = $2 LIMIT 1`,
		name, dn)

	if err == sql.ErrNoRows {
		// Create new org
		if !s.config.Sync.CreateOrgs {
			return false, false, nil
		}

		_, err = s.db.ExecContext(ctx, `
			INSERT INTO organizations (name, description, ldap_dn, ldap_synced_at)
			VALUES ($1, $2, $3, NOW())`,
			name, nullString(description), dn)
		if err != nil {
			return false, false, fmt.Errorf("failed to create organization: %w", err)
		}
		return true, false, nil
	}

	if err != nil {
		return false, false, fmt.Errorf("failed to check existing org: %w", err)
	}

	// Check if anything actually changed
	if ptrEquals(existing.Description, description) && ptrEquals(existing.LdapDN, dn) {
		// Only update ldap_synced_at timestamp
		_, _ = s.db.ExecContext(ctx, `UPDATE organizations SET ldap_synced_at = NOW() WHERE id = $1`, existing.ID)
		return false, false, nil // unchanged
	}

	// Update existing org
	_, err = s.db.ExecContext(ctx, `
		UPDATE organizations 
		SET description = $2, ldap_dn = $3, ldap_synced_at = NOW(), modified_at = NOW()
		WHERE id = $1`,
		existing.ID, nullString(description), dn)
	if err != nil {
		return false, false, fmt.Errorf("failed to update organization: %w", err)
	}

	return false, true, nil
}

// upsertTeam creates or updates a team in the database.
func (s *Syncer) upsertTeam(ctx context.Context, dn, name, description, orgName string) (created bool, updated bool, err error) {
	// Find organization ID
	var orgID int64
	err = s.db.GetContext(ctx, &orgID,
		`SELECT id FROM organizations WHERE name = $1 LIMIT 1`,
		orgName)
	if err == sql.ErrNoRows {
		// Use default organization if org not found
		err = s.db.GetContext(ctx, &orgID, `SELECT id FROM organizations ORDER BY id LIMIT 1`)
		if err != nil {
			return false, false, fmt.Errorf("no organizations exist")
		}
	} else if err != nil {
		return false, false, fmt.Errorf("failed to find organization %s: %w", orgName, err)
	}

	// Check if team exists and get current values
	var existing struct {
		ID          int64   `db:"id"`
		Description *string `db:"description"`
		LdapDN      *string `db:"ldap_dn"`
	}
	err = s.db.GetContext(ctx, &existing,
		`SELECT id, description, ldap_dn FROM teams WHERE (organization_id = $1 AND name = $2) OR ldap_dn = $3 LIMIT 1`,
		orgID, name, dn)

	if err == sql.ErrNoRows {
		// Create new team
		if !s.config.Sync.CreateTeams {
			return false, false, nil
		}

		_, err = s.db.ExecContext(ctx, `
			INSERT INTO teams (organization_id, name, description, ldap_dn, ldap_synced_at)
			VALUES ($1, $2, $3, $4, NOW())`,
			orgID, name, nullString(description), dn)
		if err != nil {
			return false, false, fmt.Errorf("failed to create team: %w", err)
		}
		return true, false, nil
	}

	if err != nil {
		return false, false, fmt.Errorf("failed to check existing team: %w", err)
	}

	// Check if anything actually changed
	if ptrEquals(existing.Description, description) && ptrEquals(existing.LdapDN, dn) {
		// Only update ldap_synced_at timestamp
		_, _ = s.db.ExecContext(ctx, `UPDATE teams SET ldap_synced_at = NOW() WHERE id = $1`, existing.ID)
		return false, false, nil // unchanged
	}

	// Update existing team
	_, err = s.db.ExecContext(ctx, `
		UPDATE teams 
		SET description = $2, ldap_dn = $3, ldap_synced_at = NOW(), modified_at = NOW()
		WHERE id = $1`,
		existing.ID, nullString(description), dn)
	if err != nil {
		return false, false, fmt.Errorf("failed to update team: %w", err)
	}

	return false, true, nil
}

// syncTeamMembers syncs team membership from LDAP member DNs.
func (s *Syncer) syncTeamMembers(ctx context.Context, teamName string, memberDNs []string) error {
	// Get team ID
	var teamID int64
	err := s.db.GetContext(ctx, &teamID, `SELECT id FROM teams WHERE name = $1 LIMIT 1`, teamName)
	if err != nil {
		return fmt.Errorf("team not found: %w", err)
	}

	// Get team's member role ID
	var memberRoleID int64
	err = s.db.GetContext(ctx, &memberRoleID,
		`SELECT id FROM roles WHERE content_type = 'team' AND object_id = $1 AND role_field = 'member_role'`,
		teamID)
	if err != nil {
		return fmt.Errorf("team member role not found: %w", err)
	}

	// For each member DN, find the user and add them to the team
	for _, dn := range memberDNs {
		var userID int64
		err := s.db.GetContext(ctx, &userID, `SELECT id FROM users WHERE ldap_dn = $1`, dn)
		if err == sql.ErrNoRows {
			// User not synced yet, skip
			continue
		}
		if err != nil {
			log.Printf("failed to find user with DN %s: %v", dn, err)
			continue
		}

		// Add user to team's member role (ignore if already exists)
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO role_members (role_id, user_id)
			VALUES ($1, $2)
			ON CONFLICT (role_id, user_id) DO NOTHING`,
			memberRoleID, userID)
		if err != nil {
			log.Printf("failed to add user %d to team %s: %v", userID, teamName, err)
		}
	}

	return nil
}

// logSyncResult saves the sync result to the database and returns the log ID.
func (s *Syncer) logSyncResult(result *LDAPSyncResult) (int64, error) {
	errorMsg := ""
	if len(result.Errors) > 0 {
		errorMsg = strings.Join(result.Errors, "; ")
	}

	var logID int64
	err := s.db.QueryRow(`
		INSERT INTO ldap_sync_log (sync_type, started_at, finished_at, status, items_processed, items_created, items_updated, items_failed, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		result.SyncType, result.StartedAt, result.FinishedAt, result.Status,
		result.ItemsProcessed, result.ItemsCreated, result.ItemsUpdated, result.ItemsFailed, nullString(errorMsg)).Scan(&logID)
	if err != nil {
		return 0, err
	}

	// Save individual items
	for _, item := range result.Items {
		// Convert attributes to JSON
		var attrsJSON []byte
		if item.LdapAttributes != nil {
			attrsJSON, _ = json.Marshal(item.LdapAttributes)
		}
		_, err := s.db.Exec(`
			INSERT INTO ldap_sync_items (sync_log_id, entity_type, entity_name, entity_id, ldap_dn, ldap_attributes, action, error_message)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			logID, item.EntityType, item.EntityName, item.EntityID, item.LdapDN, attrsJSON, item.Action, item.ErrorMessage)
		if err != nil {
			log.Printf("failed to save sync item: %v", err)
		}
	}

	result.ID = logID
	return logID, nil
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ptrEquals compares a string pointer to a string value
func ptrEquals(ptr *string, val string) bool {
	if ptr == nil {
		return val == ""
	}
	return *ptr == val
}
