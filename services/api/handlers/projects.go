package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/pkg/rbac"
	"github.com/praetordev/praetor/services/api/render"
)

// ListProjects GET /api/v1/projects
// ListProjects GET /api/v1/projects
func (h *ContentHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)
	uc := currentUser(r)

	var projects []models.Project
	var total int64

	if uc.IsSuperuser || uc.IsSystemAuditor {
		var err error
		if projects, err = h.projects.ListAll(r.Context(), pg.Limit, pg.Offset); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		total, _ = h.projects.CountAll(r.Context())
	} else {
		ids, err := h.readableIDs(r, rbac.ContentTypeProject)
		if err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		if projects, err = h.projects.ListByIDs(r.Context(), ids, pg.Limit, pg.Offset); err != nil {
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
func (h *ContentHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
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

	// Creating a project requires admin on its parent organization.
	if !h.authorize(w, r, rbac.ContentTypeOrganization, input.OrganizationID, actAdmin) {
		return
	}

	// Default SCM Type
	if input.SCMType == "" {
		input.SCMType = "git"
	}

	created, err := h.projects.Create(r.Context(), input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	// The creator becomes admin of the project they just made.
	h.grantCreatorAdmin(r.Context(), rbac.ContentTypeProject, created.ID, currentUser(r))
	render.Created(w, r, created)
}

// SyncProject POST /api/v1/projects/{id}/sync
func (h *ContentHandler) SyncProject(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Triggering an SCM sync mutates the project; require admin on it.
	// (update_role is the finer-grained AWX equivalent — a future refinement.)
	if !h.authorize(w, r, rbac.ContentTypeProject, id, actAdmin) {
		return
	}

	project, err := h.projects.Get(r.Context(), id)
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
	_ = h.projects.TouchModified(r.Context(), id)

	render.JSON(w, r, map[string]interface{}{
		"success":    true,
		"message":    "Project synced successfully",
		"revision":   revision,
		"commit_msg": message,
	})
}
