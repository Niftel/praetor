package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/render"
	"github.com/jmoiron/sqlx"
	promMetrics "github.com/praetordev/metrics"
	"github.com/praetordev/praetor/services/api/handlers"
	modelAuth "github.com/praetordev/praetor/services/api/middleware"
	praetorRender "github.com/praetordev/render"
)

// Config holds the API's externally-supplied configuration, resolved from env in
// cmd/api/main.go and passed in so handlers receive plain values.
type Config struct {
	// CredentialSecrets enables service-backed credential creation. When nil,
	// the legacy local encryption path remains available during migration.
	CredentialSecrets handlers.CredentialSecrets
	// IngestionURL is the base URL the API proxies run-log reads to.
	IngestionURL string
	// InternalToken is the shared cluster secret the API presents to ingestion's
	// authenticated log-read endpoint.
	InternalToken string
	// LDAPConfigPath is the path to the LDAP config file mounted into the API.
	LDAPConfigPath string
	// RBACPolicyPath optionally selects a mounted RBAC v4 policy file. Empty uses
	// the versioned policy embedded in the API binary.
	RBACPolicyPath string
	// RBACPolicySHA256 optionally pins the exact mounted policy bytes.
	RBACPolicySHA256 string
	// RBACPolicyRefreshInterval periodically checks a configured file source.
	RBACPolicyRefreshInterval time.Duration
	// RBACDecisionAudit emits one structured event for every v4 evaluation.
	RBACDecisionAudit bool
}

// NewRouter instantiates the chi Router and wires middleware.
func NewRouter(db *sqlx.DB, cfg Config) *chi.Mux {
	r := chi.NewRouter()

	// The authorization enforcement helper (PEP) is built once and injected into
	// every resource that enforces access. It wraps the capability store (PDP)
	// with the legacy is_superuser decorator — the single place that bypass
	// lives, so removing it later is one edit here, not a sweep of handlers.
	authz := handlers.NewAuthorizerWithPolicy(db, cfg.RBACPolicyPath, cfg.RBACPolicySHA256, cfg.RBACPolicyRefreshInterval, cfg.RBACDecisionAudit)

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

	// Identity / access domains (formerly the ContentHandler god-object — B6/#85).
	auth := handlers.NewAuthResource(db, authz)
	auth.LDAPConfigPath = cfg.LDAPConfigPath // enables LDAP login when set
	orgs := handlers.NewOrgsResource(db, authz)
	users := handlers.NewUsersResource(db, authz)
	teams := handlers.NewTeamsResource(db, authz)
	access := handlers.NewAccessResource(db, authz)

	// Auth Routes (Public). Login is rate-limited per IP to blunt password
	// brute-forcing (20 attempts/minute).
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.With(modelAuth.RateLimit(20, time.Minute)).Post("/login", auth.Login)
	})

	r.Get("/api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		praetorRender.JSON(w, r, map[string]string{"status": "pong"})
	})
	r.Get("/api/v1/ready", func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		praetorRender.JSON(w, r, map[string]string{"status": "ready"})
	})

	// Prometheus scrape endpoint (unauthenticated, like /ping).
	r.Handle("/metrics", promMetrics.Handler())

	// Public Host Runner Heartbeat (for host-runner agents)
	hosts := handlers.NewHostsResource(db, authz)
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
	events := handlers.NewEventsResource(db, authz)
	r.Post("/api/v1/events/{source}", events.Intake)

	// Protected Routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(modelAuth.AuthMiddleware(db))
		r.Use(modelAuth.ActivityCapture(db)) // audit log: record successful mutations

		// Active RBAC v4 policy provenance and an operator-triggered refresh.
		r.Get("/rbac/policy", authz.PolicyStatus)
		r.Post("/rbac/policy/refresh", authz.RefreshPolicy)

		// Execution Packs registry (the self-contained runtimes pushed to hosts).
		r.Mount("/execution-packs", handlers.NewExecutionPacksResource(db, authz).Routes())

		// Event-driven automation (EDA): sources + rules management.
		r.Mount("/event-sources", events.SourceRoutes())
		r.Mount("/event-rules", events.RuleRoutes())

		// Personal access tokens (headless / CI API auth) — each user manages own.
		r.Mount("/tokens", handlers.NewTokensResource(db, authz).Routes())

		// Activity stream (audit log) — superuser/auditor only
		r.Get("/activity-stream", access.ListActivityStream)

		// =======================================================================
		// Organizations (AWX-style with RBAC)
		// =======================================================================
		r.Route("/organizations", func(r chi.Router) {
			r.Get("/", orgs.ListOrganizations)
			r.Post("/", orgs.CreateOrganization)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", orgs.GetOrganization)
				r.Put("/", orgs.UpdateOrganization)
				r.Delete("/", orgs.DeleteOrganization)

				// Organization membership
				r.Get("/users", orgs.ListOrganizationUsers)
				r.Post("/users", orgs.AddOrganizationUser)
				r.Delete("/users/{userId}", orgs.RemoveOrganizationUser)

				r.Get("/admins", orgs.ListOrganizationAdmins)
				r.Post("/admins", orgs.AddOrganizationAdmin)

				// Organization subresources
				r.Get("/teams", orgs.ListOrganizationTeams)
				r.Get("/projects", orgs.ListOrganizationProjects)
				r.Get("/inventories", orgs.ListOrganizationInventories)
				r.Get("/object_roles", orgs.ListOrganizationRoles)

				// Galaxy / Automation Hub credentials for the org
				r.Get("/galaxy-credentials", orgs.ListOrgGalaxyCredentials)
				r.Post("/galaxy-credentials", orgs.AddOrgGalaxyCredential)
				r.Delete("/galaxy-credentials/{credId}", orgs.RemoveOrgGalaxyCredential)
			})
		})

		// =======================================================================
		// Users
		// =======================================================================
		r.Route("/users", func(r chi.Router) {
			r.Get("/", users.ListUsers)
			r.Post("/", users.CreateUser)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", users.GetUser)
				r.Put("/", users.UpdateUser)
				r.Delete("/", users.DeleteUser)
				r.Get("/organizations", users.ListUserOrganizations)
				r.Get("/teams", users.ListUserTeams)
				r.Get("/access", access.UserAccess) // capability roles a user holds
			})
		})

		// Per-resource access (who holds which RoleDefinition on an object), the
		// RoleDefinitions assignable on a type, and grant/revoke.
		r.Get("/access", access.ResourceAccess)
		r.Post("/access", access.GrantAccess)
		r.Delete("/access", access.RevokeAccess)
		r.Get("/role-definitions", access.AssignableRoles)

		// =======================================================================
		// Teams
		// =======================================================================
		r.Route("/teams", func(r chi.Router) {
			r.Get("/", teams.ListTeams)
			r.Post("/", teams.CreateTeam)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", teams.GetTeam)
				r.Put("/", teams.UpdateTeam)
				r.Delete("/", teams.DeleteTeam)

				r.Get("/members", teams.ListTeamMembers)
				r.Post("/members", teams.AddTeamMember)
				r.Delete("/members/{userID}", teams.RemoveTeamMember)
			})
		})

		// =======================================================================
		// LDAP Management
		// =======================================================================
		ldapHandler := handlers.NewLDAPHandler(db, cfg.LDAPConfigPath)
		r.Route("/ldap", func(r chi.Router) {
			r.Get("/config", ldapHandler.GetConfig)
			r.Post("/test-connection", ldapHandler.TestConnection)
		})

		// =======================================================================
		// Projects
		// =======================================================================
		projects := handlers.NewProjectsResource(db, authz)
		r.Get("/projects", projects.ListProjects)
		r.Post("/projects", projects.CreateProject)
		r.Post("/projects/{id}/sync", projects.SyncProject)

		// =======================================================================
		// Jobs
		// =======================================================================
		jobs := handlers.NewJobsResource(db, cfg.IngestionURL, cfg.InternalToken, authz)
		r.Mount("/jobs", jobs.Routes())

		// =======================================================================
		// Job Templates
		// =======================================================================
		templates := handlers.NewTemplatesResource(db, authz)
		r.Mount("/job-templates", templates.Routes())

		// Notification templates (org-scoped targets; attachments live under job-templates)
		notifications := handlers.NewNotificationsResource(db, authz)
		r.Get("/notification-types", notifications.ListNotificationTypes) // registered backends + their config schema
		r.Get("/notification-templates", notifications.ListNotificationTemplates)
		r.Post("/notification-templates", notifications.CreateNotificationTemplate)
		r.Delete("/notification-templates/{id}", notifications.DeleteNotificationTemplate)

		// Workflows (DAG of templates with success/failure/approval edges)
		wf := handlers.NewWorkflowsResource(db, authz)
		r.Get("/workflow-templates", wf.ListWorkflows)
		r.Post("/workflow-templates", wf.CreateWorkflow)
		r.Get("/workflow-templates/{id}", wf.GetWorkflow)
		r.Put("/workflow-templates/{id}", wf.UpdateWorkflow)
		r.Delete("/workflow-templates/{id}", wf.DeleteWorkflow)
		r.Post("/workflow-templates/{id}/launch", wf.LaunchWorkflow)
		r.Get("/workflow-jobs", wf.ListWorkflowJobs)
		r.Get("/workflow-jobs/{id}", wf.GetWorkflowJob)
		r.Get("/workflow-approvals", wf.ListWorkflowApprovals)
		r.Post("/workflow-job-nodes/{id}/approve", wf.ApproveNode)
		r.Post("/workflow-job-nodes/{id}/deny", wf.DenyNode)
		// Workflow-level notification attachments (success | error | approval).
		r.Get("/workflow-templates/{id}/notifications", wf.ListWorkflowNotifications)
		r.Post("/workflow-templates/{id}/notifications", wf.AttachWorkflowNotification)
		r.Delete("/workflow-templates/{id}/notifications/{ntId}/{event}", wf.DetachWorkflowNotification)

		// Triggers: event triggers (job outcome -> launch) + webhook trigger surface
		r.Mount("/triggers", handlers.NewTriggersResource(db, authz).Routes())

		// =======================================================================
		// Inventories with nested hosts/groups
		// =======================================================================
		hostsHandler := handlers.NewHostsResource(db, authz)
		groups := handlers.NewGroupsResource(db, authz)

		inventories := handlers.NewInventoriesResource(db, authz)
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
		credTypes := handlers.NewCredentialTypesResource(db, authz)
		r.Mount("/credential-types", credTypes.Routes())

		creds := handlers.NewCredentialsResourceWithSecrets(db, authz, cfg.CredentialSecrets)
		r.Mount("/credentials", creds.Routes())

		// =======================================================================
		// Schedules
		// =======================================================================
		schedules := handlers.NewSchedulesResource(db, authz)
		r.Mount("/schedules", schedules.Routes())

	})

	return r
}
