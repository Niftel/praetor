package dto

import (
	"time"

	"github.com/praetordev/models"
)

// Organization is the wire shape of an organization.
type Organization struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ModifiedAt  time.Time `json:"modified_at"`
}

func FromOrganization(m models.Organization) Organization {
	return Organization{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		CreatedAt:   m.CreatedAt,
		ModifiedAt:  m.ModifiedAt,
	}
}

func FromOrganizations(ms []models.Organization) []Organization {
	out := make([]Organization, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromOrganization(m))
	}
	return out
}

func (d Organization) ToModel() models.Organization {
	return models.Organization{
		ID:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		CreatedAt:   d.CreatedAt,
		ModifiedAt:  d.ModifiedAt,
	}
}

// User is the wire shape of a user. The password hash (models.User.PasswordHash,
// json:"-") is deliberately absent — it never belongs on the wire.
type User struct {
	ID              int64      `json:"id"`
	Username        string     `json:"username"`
	FirstName       *string    `json:"first_name,omitempty"`
	LastName        *string    `json:"last_name,omitempty"`
	Email           *string    `json:"email,omitempty"`
	IsSuperuser     bool       `json:"is_superuser"`
	IsSystemAuditor bool       `json:"is_system_auditor"`
	IsActive        bool       `json:"is_active"`
	LdapDN          *string    `json:"ldap_dn,omitempty"`
	LdapSyncedAt    *time.Time `json:"ldap_synced_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	ModifiedAt      time.Time  `json:"modified_at"`
}

func FromUser(m models.User) User {
	return User{
		ID:              m.ID,
		Username:        m.Username,
		FirstName:       m.FirstName,
		LastName:        m.LastName,
		Email:           m.Email,
		IsSuperuser:     m.IsSuperuser,
		IsSystemAuditor: m.IsSystemAuditor,
		IsActive:        m.IsActive,
		LdapDN:          m.LdapDN,
		LdapSyncedAt:    m.LdapSyncedAt,
		CreatedAt:       m.CreatedAt,
		ModifiedAt:      m.ModifiedAt,
	}
}

func FromUsers(ms []models.User) []User {
	out := make([]User, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromUser(m))
	}
	return out
}

// ToModel maps a decoded user request to the persistence struct. PasswordHash is
// not a wire field, so it is left zero here — the handler sets it from the
// separately-decoded password.
func (d User) ToModel() models.User {
	return models.User{
		ID:              d.ID,
		Username:        d.Username,
		FirstName:       d.FirstName,
		LastName:        d.LastName,
		Email:           d.Email,
		IsSuperuser:     d.IsSuperuser,
		IsSystemAuditor: d.IsSystemAuditor,
		IsActive:        d.IsActive,
		LdapDN:          d.LdapDN,
		LdapSyncedAt:    d.LdapSyncedAt,
		CreatedAt:       d.CreatedAt,
		ModifiedAt:      d.ModifiedAt,
	}
}

// Team is the wire shape of a team.
type Team struct {
	ID             int64     `json:"id"`
	OrganizationID int64     `json:"organization_id"`
	Name           string    `json:"name"`
	Description    *string   `json:"description,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ModifiedAt     time.Time `json:"modified_at"`
}

func FromTeam(m models.Team) Team {
	return Team{
		ID:             m.ID,
		OrganizationID: m.OrganizationID,
		Name:           m.Name,
		Description:    m.Description,
		CreatedAt:      m.CreatedAt,
		ModifiedAt:     m.ModifiedAt,
	}
}

func FromTeams(ms []models.Team) []Team {
	out := make([]Team, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromTeam(m))
	}
	return out
}

func (d Team) ToModel() models.Team {
	return models.Team{
		ID:             d.ID,
		OrganizationID: d.OrganizationID,
		Name:           d.Name,
		Description:    d.Description,
		CreatedAt:      d.CreatedAt,
		ModifiedAt:     d.ModifiedAt,
	}
}
