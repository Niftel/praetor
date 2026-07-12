package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/pkg/render"
	"github.com/praetordev/praetor/services/api/store"
)

// ProjectStore is the projects-domain data access the handler depends on.
type ProjectStore interface {
	ListAll(ctx context.Context, limit, offset int) ([]models.Project, error)
	CountAll(ctx context.Context) (int64, error)
	ListByIDs(ctx context.Context, ids []int64, limit, offset int) ([]models.Project, error)
	Get(ctx context.Context, id int64) (models.Project, error)
	Create(ctx context.Context, input models.Project) (models.Project, error)
	TouchModified(ctx context.Context, id int64) error
}

// ProjectsResource is the self-contained projects domain (extracted from the
// former ContentHandler god-object — B6/#85). It embeds *Authorizer for the
// shared RBAC helpers.
type ProjectsResource struct {
	DB *sqlx.DB
	*Authorizer
	store ProjectStore
}

func NewProjectsResource(db *sqlx.DB) *ProjectsResource {
	return &ProjectsResource{DB: db, Authorizer: NewAuthorizer(db), store: store.NewProjectStore(db)}
}

// ListProjects GET /api/v1/projects
func (h *ProjectsResource) ListProjects(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)
	uc := currentUser(r)

	var projects []models.Project
	var total int64

	if uc.IsSuperuser || uc.IsSystemAuditor {
		var err error
		if projects, err = h.store.ListAll(r.Context(), pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total, _ = h.store.CountAll(r.Context())
	} else {
		ids, err := h.readableIDs(r, rbac.ContentTypeProject)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if projects, err = h.store.ListByIDs(r.Context(), ids, pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total = int64(len(ids))
	}

	if projects == nil {
		projects = []models.Project{}
	}

	render.JSON(w, r, &render.PaginatedResponse{
		Items:  projects,
		Total:  total,
		Limit:  pg.Limit,
		Offset: pg.Offset,
	})
}

// CreateProject POST /api/v1/projects
func (h *ProjectsResource) CreateProject(w http.ResponseWriter, r *http.Request) {
	var input models.Project
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Basic validation
	if input.Name == "" || input.SCMURL == "" || input.OrganizationID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	// Creating a project requires the org's project_admin_role (org admins and
	// superusers inherit it through the role hierarchy).
	if !h.authorizeOrgRole(w, r, input.OrganizationID, rbac.RoleFieldProjectAdmin) {
		return
	}

	// Default SCM Type
	if input.SCMType == "" {
		input.SCMType = "git"
	}

	created, err := h.store.Create(r.Context(), input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	// The creator becomes admin of the project they just made.
	h.grantCreatorAdmin(r.Context(), rbac.ContentTypeProject, created.ID, currentUser(r))
	render.Created(w, r, created)
}

// SyncProject POST /api/v1/projects/{id}/sync
func (h *ProjectsResource) SyncProject(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Triggering an SCM sync is the AWX update_role action: it may run a project
	// update without full admin. Project admins inherit update_role.
	if !h.authorize(w, r, rbac.ContentTypeProject, id, actUpdate) {
		return
	}

	project, err := h.store.Get(r.Context(), id)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "project_sync_")
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer os.RemoveAll(tmpDir)

	// Clone repo to verify access
	cmd := exec.Command("git", "clone", "--depth", "1", project.SCMURL, tmpDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		render.JSON(w, r, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("git clone failed: %v\nOutput: %s", err, string(output)),
		})
		return
	}

	// Get Commit Hash
	cmdRev := exec.Command("git", "-C", tmpDir, "rev-parse", "--short", "HEAD")
	revOutput, _ := cmdRev.Output()
	revision := string(revOutput)

	// Get Commit Message
	cmdMsg := exec.Command("git", "-C", tmpDir, "log", "-1", "--pretty=%s")
	msgOutput, _ := cmdMsg.Output()
	message := string(msgOutput)

	// Update modified_at to signal sync
	_ = h.store.TouchModified(r.Context(), id)

	render.JSON(w, r, map[string]interface{}{
		"success":    true,
		"message":    "Project synced successfully",
		"revision":   revision,
		"commit_msg": message,
	})
}
