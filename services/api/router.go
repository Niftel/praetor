package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/render"
	"github.com/jmoiron/sqlx"
	promMetrics "github.com/praetordev/praetor/pkg/metrics"
	"github.com/praetordev/praetor/services/api/handlers"
	modelAuth "github.com/praetordev/praetor/services/api/middleware"
	praetorRender "github.com/praetordev/praetor/services/api/render"
)

// NewRouter instantiates the chi Router and wires middleware.
func NewRouter(db *sqlx.DB) *chi.Mux {
	r := chi.NewRouter()

	// Base Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.URLFormat)
	r.Use(modelAuth.Metrics)
	r.Use(render.SetContentType(render.ContentTypeJSON))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Handlers
	content := handlers.NewContentHandler(db)

	// Auth Routes (Public). Login is rate-limited per IP to blunt password
	// brute-forcing (20 attempts/minute).
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.With(modelAuth.RateLimit(20, time.Minute)).Post("/login", content.Login)
	})

	r.Get("/api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		praetorRender.JSON(w, r, map[string]string{"status": "pong"})
	})

	// Prometheus scrape endpoint (unauthenticated, like /ping).
	r.Handle("/metrics", promMetrics.Handler())

	// Public Host Runner Heartbeat (for host-runner agents)
	hosts := handlers.NewHostsResource(db)
	r.Post("/api/v1/hosts/{hostId}/runner-heartbeat", hosts.RunnerHeartbeat)

	// Public inbound webhooks (GitHub/GitLab/generic -> launch). Verified by the
	// template's shared secret, not user auth.
	webhooks := handlers.NewWebhooksResource(db)
	r.Post("/api/v1/webhooks/job-templates/{id}/{service}", webhooks.Handle)
	r.Post("/api/v1/webhooks/workflow-templates/{id}/{service}", webhooks.HandleWorkflow)
	// A git push rebuilds a git-backed Execution Pack.
	r.Post("/api/v1/webhooks/execution-packs/{id}/{service}", webhooks.HandlePack)
	// A waiting webhook_in workflow node is released by its per-run event_token.
	r.Post("/api/v1/webhooks/workflow-job-nodes/{id}/callback", webhooks.HandleNodeCallback)

	// Public event-driven-automation intake (EDA-style): a source pushes an event,
	// verified by the source's shared token; matching rules launch remediation.
	events := handlers.NewEventsResource(db)
	r.Post("/api/v1/events/{source}", events.Intake)

	// Protected Routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(modelAuth.AuthMiddleware(db))
		r.Use(modelAuth.ActivityCapture(db)) // audit log: record successful mutations

		// Execution Packs registry (the self-contained runtimes pushed to hosts).
		r.Mount("/execution-packs", handlers.NewExecutionPacksResource(db).Routes())

		// Event-driven automation (EDA): sources + rules management.
		r.Mount("/event-sources", events.SourceRoutes())
		r.Mount("/event-rules", events.RuleRoutes())

		// Personal access tokens (headless / CI API auth) — each user manages own.
		r.Mount("/tokens", handlers.NewTokensResource(db).Routes())

		// Activity stream (audit log) — superuser/auditor only
		r.Get("/activity-stream", content.ListActivityStream)

		// =======================================================================
		// Organizations (AWX-style with RBAC)
		// =======================================================================
		r.Route("/organizations", func(r chi.Router) {
			r.Get("/", content.ListOrganizations)
			r.Post("/", content.CreateOrganization)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", content.GetOrganization)
				r.Put("/", content.UpdateOrganization)
				r.Delete("/", content.DeleteOrganization)

				// Organization membership
				r.Get("/users", content.ListOrganizationUsers)
				r.Post("/users", content.AddOrganizationUser)
				r.Delete("/users/{userId}", content.RemoveOrganizationUser)

				r.Get("/admins", content.ListOrganizationAdmins)
				r.Post("/admins", content.AddOrganizationAdmin)

				// Organization subresources
				r.Get("/teams", content.ListOrganizationTeams)
				r.Get("/projects", content.ListOrganizationProjects)
				r.Get("/inventories", content.ListOrganizationInventories)
				r.Get("/object_roles", content.ListOrganizationRoles)

				// Galaxy / Automation Hub credentials for the org
				r.Get("/galaxy-credentials", content.ListOrgGalaxyCredentials)
				r.Post("/galaxy-credentials", content.AddOrgGalaxyCredential)
				r.Delete("/galaxy-credentials/{credId}", content.RemoveOrgGalaxyCredential)
			})
		})

		// =======================================================================
		// Users
		// =======================================================================
		r.Route("/users", func(r chi.Router) {
			r.Get("/", content.ListUsers)
			r.Post("/", content.CreateUser)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", content.GetUser)
				r.Put("/", content.UpdateUser)
				r.Delete("/", content.DeleteUser)
				r.Get("/organizations", content.ListUserOrganizations)
				r.Get("/teams", content.ListUserTeams)
				r.Get("/roles", content.ListUserRoles)
				r.Get("/access", content.UserAccess) // roles a user holds, with resource names
			})
		})

		// Per-resource access (who holds which role on an object): AWX-style.
		r.Get("/access", content.ResourceAccess)

		// =======================================================================
		// Teams
		// =======================================================================
		r.Route("/teams", func(r chi.Router) {
			r.Get("/", content.ListTeams)
			r.Post("/", content.CreateTeam)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", content.GetTeam)
				r.Put("/", content.UpdateTeam)
				r.Delete("/", content.DeleteTeam)

				r.Get("/members", content.ListTeamMembers)
				r.Post("/members", content.AddTeamMember)
				r.Delete("/members/{userID}", content.RemoveTeamMember)
			})
		})

		// =======================================================================
		// Roles (AWX-style)
		// =======================================================================
		r.Route("/roles", func(r chi.Router) {
			r.Get("/", content.ListRoles)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", content.GetRole)
				r.Get("/users", content.ListRoleUsers)
				r.Post("/users", content.AddRoleUser)
				r.Delete("/users/{userId}", content.RemoveRoleUser)
				r.Get("/teams", content.ListRoleTeams)
				r.Post("/teams", content.AddRoleTeam)
				r.Delete("/teams/{teamId}", content.RemoveRoleTeam)
			})
		})

		// =======================================================================
		// LDAP Management
		// =======================================================================
		ldapHandler := handlers.NewLDAPHandler(db)
		r.Route("/ldap", func(r chi.Router) {
			r.Get("/config", ldapHandler.GetConfig)
			r.Post("/test-connection", ldapHandler.TestConnection)
			r.Post("/sync", ldapHandler.TriggerSync)
			r.Get("/sync/status", ldapHandler.GetSyncStatus)
			r.Get("/sync/{id}", ldapHandler.GetSyncDetails)
		})

		// =======================================================================
		// Projects
		// =======================================================================
		r.Get("/projects", content.ListProjects)
		r.Post("/projects", content.CreateProject)
		r.Post("/projects/{id}/sync", content.SyncProject)

		// =======================================================================
		// Jobs
		// =======================================================================
		jobs := handlers.NewJobsResource(db)
		r.Mount("/jobs", jobs.Routes())

		// =======================================================================
		// Job Templates
		// =======================================================================
		templates := handlers.NewTemplatesResource(db)
		r.Mount("/job-templates", templates.Routes())

		// Notification templates (org-scoped targets; attachments live under job-templates)
		r.Get("/notification-templates", content.ListNotificationTemplates)
		r.Post("/notification-templates", content.CreateNotificationTemplate)
		r.Delete("/notification-templates/{id}", content.DeleteNotificationTemplate)

		// Workflows (DAG of templates with success/failure/approval edges)
		wf := handlers.NewWorkflowsResource(db)
		r.Get("/workflow-templates", wf.ListWorkflows)
		r.Post("/workflow-templates", wf.CreateWorkflow)
		r.Get("/workflow-templates/{id}", wf.GetWorkflow)
		r.Put("/workflow-templates/{id}", wf.UpdateWorkflow)
		r.Delete("/workflow-templates/{id}", wf.DeleteWorkflow)
		r.Post("/workflow-templates/{id}/launch", wf.LaunchWorkflow)
		r.Get("/workflow-jobs", wf.ListWorkflowJobs)
		r.Get("/workflow-jobs/{id}", wf.GetWorkflowJob)
		r.Post("/workflow-job-nodes/{id}/approve", wf.ApproveNode)
		r.Post("/workflow-job-nodes/{id}/deny", wf.DenyNode)

		// Triggers: event triggers (job outcome -> launch) + webhook trigger surface
		r.Mount("/triggers", handlers.NewTriggersResource(db).Routes())

		// =======================================================================
		// Inventories with nested hosts/groups
		// =======================================================================
		hostsHandler := handlers.NewHostsResource(db)
		groups := handlers.NewGroupsResource(db)

		inventories := handlers.NewInventoriesResource(db)
		r.Route("/inventories", func(r chi.Router) {
			r.Get("/", inventories.ListInventories)
			r.Post("/", inventories.CreateInventory)
			r.Route("/{inventoryId}", func(r chi.Router) {
				r.Get("/", inventories.GetInventoryByParam)
				r.Put("/", inventories.UpdateInventoryByParam)
				r.Delete("/", inventories.DeleteInventoryByParam)
				r.Post("/import", inventories.ImportInventory)
				r.Get("/sources", inventories.ListInventorySources)
				r.Post("/sources", inventories.CreateInventorySource)
				r.Delete("/sources/{sourceId}", inventories.DeleteInventorySource)
				r.Post("/sources/{sourceId}/sync", inventories.SyncInventorySource)
				r.Mount("/hosts", hostsHandler.Routes())
				r.Mount("/groups", groups.Routes())
			})
		})

		// Direct access to hosts and groups by ID
		r.Mount("/hosts", hostsHandler.HostRoutes())
		r.Mount("/groups", groups.GroupRoutes())

		// =======================================================================
		// Credentials
		// =======================================================================
		credTypes := handlers.NewCredentialTypesResource(db)
		r.Mount("/credential-types", credTypes.Routes())

		creds := handlers.NewCredentialsResource(db)
		r.Mount("/credentials", creds.Routes())

		// =======================================================================
		// Schedules
		// =======================================================================
		schedules := handlers.NewSchedulesResource(db)
		r.Mount("/schedules", schedules.Routes())

	})

	return r
}
