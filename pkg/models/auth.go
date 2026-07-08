package models

import (
	"encoding/json"
	"time"
)

type Organization struct {
	ID          int64     `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description *string   `json:"description,omitempty" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	ModifiedAt  time.Time `json:"modified_at" db:"modified_at"`
}

type User struct {
	ID              int64      `json:"id" db:"id"`
	Username        string     `json:"username" db:"username"`
	PasswordHash    string     `json:"-" db:"password_hash"`
	FirstName       *string    `json:"first_name,omitempty" db:"first_name"`
	LastName        *string    `json:"last_name,omitempty" db:"last_name"`
	Email           *string    `json:"email,omitempty" db:"email"`
	IsSuperuser     bool       `json:"is_superuser" db:"is_superuser"`
	IsSystemAuditor bool       `json:"is_system_auditor" db:"is_system_auditor"`
	IsActive        bool       `json:"is_active" db:"is_active"`
	LdapDN          *string    `json:"ldap_dn,omitempty" db:"ldap_dn"`
	LdapSyncedAt    *time.Time `json:"ldap_synced_at,omitempty" db:"ldap_synced_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	ModifiedAt      time.Time  `json:"modified_at" db:"modified_at"`
}

type Team struct {
	ID             int64     `json:"id" db:"id"`
	OrganizationID int64     `json:"organization_id" db:"organization_id"`
	Name           string    `json:"name" db:"name"`
	Description    *string   `json:"description,omitempty" db:"description"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	ModifiedAt     time.Time `json:"modified_at" db:"modified_at"`
}

type Role struct {
	ID          int64           `json:"id" db:"id"`
	Name        string          `json:"name" db:"name"`
	Description *string         `json:"description,omitempty" db:"description"`
	Permissions json.RawMessage `json:"permissions" db:"permissions"`
}

type TeamMember struct {
	ID        int64     `json:"id" db:"id"`
	TeamID    int64     `json:"team_id" db:"team_id"`
	UserID    int64     `json:"user_id" db:"user_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type RoleBinding struct {
	ID             int64     `json:"id" db:"id"`
	RoleID         int64     `json:"role_id" db:"role_id"`
	UserID         *int64    `json:"user_id,omitempty" db:"user_id"`
	TeamID         *int64    `json:"team_id,omitempty" db:"team_id"`
	OrganizationID *int64    `json:"organization_id,omitempty" db:"organization_id"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}
