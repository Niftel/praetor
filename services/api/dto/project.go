// Package dto holds the API's HTTP wire types — the request/response shapes the
// API serves — kept separate from the persistence/domain structs in
// github.com/praetordev/models. Handlers map models to these DTOs on the way out
// and map DTOs to models on the way in, so the JSON contract the frontend depends
// on is decoupled from the database schema: a db column can be renamed, added or
// reordered without silently changing the wire, and (once models sheds its json
// tags) the two evolve independently.
//
// Each DTO carries the exact json tags AND field types its model has, and
// dto_test.go asserts — field by field, without a database — that the wire shape
// is byte-identical to the model's. That reflection guard is the
// byte-compatibility contract; keep a case in it for every DTO added here.
package dto

import (
	"time"

	"github.com/praetordev/models"
)

// Project is the wire shape of a project.
type Project struct {
	ID             int64     `json:"id"`
	OrganizationID int64     `json:"organization_id"`
	Name           string    `json:"name"`
	Description    *string   `json:"description,omitempty"`
	SCMType        string    `json:"scm_type"`
	SCMURL         string    `json:"scm_url"`
	SCMBranch      *string   `json:"scm_branch,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	ModifiedAt     time.Time `json:"modified_at"`
}

// FromProject maps a persisted project to its wire shape.
func FromProject(m models.Project) Project {
	return Project{
		ID:             m.ID,
		OrganizationID: m.OrganizationID,
		Name:           m.Name,
		Description:    m.Description,
		SCMType:        m.SCMType,
		SCMURL:         m.SCMURL,
		SCMBranch:      m.SCMBranch,
		CreatedAt:      m.CreatedAt,
		ModifiedAt:     m.ModifiedAt,
	}
}

// FromProjects maps a slice of projects, never returning nil (so an empty result
// serializes as [] rather than null).
func FromProjects(ms []models.Project) []Project {
	out := make([]Project, 0, len(ms))
	for _, m := range ms {
		out = append(out, FromProject(m))
	}
	return out
}

// ToModel maps a decoded request DTO to the persistence struct.
func (d Project) ToModel() models.Project {
	return models.Project{
		ID:             d.ID,
		OrganizationID: d.OrganizationID,
		Name:           d.Name,
		Description:    d.Description,
		SCMType:        d.SCMType,
		SCMURL:         d.SCMURL,
		SCMBranch:      d.SCMBranch,
		CreatedAt:      d.CreatedAt,
		ModifiedAt:     d.ModifiedAt,
	}
}
