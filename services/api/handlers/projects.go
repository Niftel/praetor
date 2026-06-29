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
	"github.com/praetordev/praetor/services/api/render"
)

// ListProjects GET /api/v1/projects
// ListProjects GET /api/v1/projects
func (h *ContentHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	pg := render.ParsePagination(r)

	var projects []models.Project
	query := `SELECT * FROM projects ORDER BY id LIMIT $1 OFFSET $2`
	err := h.DB.Select(&projects, query, pg.Limit, pg.Offset)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	var total int64
	_ = h.DB.Get(&total, "SELECT count(*) FROM projects")

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

	fmt.Printf("DEBUG: CreateProject Input: %+v\n", input)

	// Basic validation
	if input.Name == "" || input.SCMURL == "" || input.OrganizationID == 0 {
		render.ErrInvalidRequest(nil).Render(w, r)
		return
	}

	// Default SCM Type
	if input.SCMType == "" {
		input.SCMType = "git"
	}

	query := `
		INSERT INTO projects (organization_id, name, description, scm_type, scm_url, scm_branch) 
		VALUES (:organization_id, :name, :description, :scm_type, :scm_url, :scm_branch) 
		RETURNING *`

	rows, err := h.DB.NamedQuery(query, input)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	defer rows.Close()

	if rows.Next() {
		var created models.Project
		if err := rows.StructScan(&created); err != nil {
			render.ErrInternal(err).Render(w, r)
			return
		}
		render.Created(w, r, created)
	} else {
		render.ErrInternal(nil).Render(w, r)
	}
}

// SyncProject POST /api/v1/projects/{id}/sync
func (h *ContentHandler) SyncProject(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var project models.Project
	if err := h.DB.Get(&project, "SELECT * FROM projects WHERE id = $1", id); err != nil {
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
	_, _ = h.DB.Exec("UPDATE projects SET modified_at = NOW() WHERE id = $1", id)

	render.JSON(w, r, map[string]interface{}{
		"success":    true,
		"message":    "Project synced successfully",
		"revision":   revision,
		"commit_msg": message,
	})
}
