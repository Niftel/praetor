package handlers

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/hyperjumptech/grule-rule-engine/ast"
	"github.com/hyperjumptech/grule-rule-engine/builder"
	"github.com/hyperjumptech/grule-rule-engine/engine"
	"github.com/hyperjumptech/grule-rule-engine/pkg"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/launch"
	"github.com/praetordev/rbac"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
)

// EventStore is the EDA-domain data access the handler depends on.
type EventStore interface {
	IntakeSource(ctx context.Context, name string) (store.EventIntakeSource, error)
	RulesForIntake(ctx context.Context, orgID, sourceID int64) ([]store.EventRuleMatch, error)
	InsertReceipt(ctx context.Context, sourceID int64, payload []byte, matched int, launched []byte) error
	JobTemplateAllowSimultaneous(ctx context.Context, unifiedTemplateID int64) bool
	ActiveJobCount(ctx context.Context, unifiedTemplateID int64) int
	InsertEventJob(ctx context.Context, name string, unifiedTemplateID int64, opts launch.Options) (int64, error)
	LaunchWorkflowSnapshot(ctx context.Context, workflowTemplateID int64, opts launch.Options) (int64, error)
	ListSources(ctx context.Context) ([]store.EventSource, error)
	CreateSource(ctx context.Context, in store.EventSource) (store.EventSource, error)
	DeleteSource(ctx context.Context, id int64) error
	ListRules(ctx context.Context) ([]store.EventRule, error)
	CreateRule(ctx context.Context, in store.EventRule) (store.EventRule, error)
	DeleteRule(ctx context.Context, id int64) error
}

// EventsResource is Praetor's event-driven automation surface (EDA-style):
// authenticated event SOURCES push events, RULES evaluate a condition against each
// event with the grule rule engine, and a match LAUNCHES a job/workflow template
// with the event injected as context. It's the "source -> rulebook -> action" loop
// adapted to a push model: point Alertmanager/monitoring at /events/{source} and a
// matching rule heals the affected host.
type EventsResource struct {
	*Authorizer
	DB    *sqlx.DB
	store EventStore
}

func NewEventsResource(db *sqlx.DB, authz *Authorizer) *EventsResource {
	return &EventsResource{Authorizer: authz, DB: db, store: store.NewEventStore(db)}
}

// eventSource / eventRule alias the store DTOs so handler code reads unchanged.
type eventSource = store.EventSource
type eventRule = store.EventRule

// SourceRoutes / RuleRoutes are mounted under the authenticated API; Intake is
// public (verified by the source's shared token, like inbound webhooks).
func (rs *EventsResource) SourceRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListSources)
	r.Post("/", rs.CreateSource)
	r.Delete("/{id}", rs.DeleteSource)
	return r
}

func (rs *EventsResource) RuleRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", rs.ListRules)
	r.Post("/", rs.CreateRule)
	r.Delete("/{id}", rs.DeleteRule)
	return r
}

// --- the rule engine (grule) ---

// eventFact wraps the incoming event JSON and exposes typed, path-based accessors
// to GRL conditions, e.g. Event.Str("labels.alertname"). Paths are dotted and may
// index arrays (Event.Str("alerts.0.status")).
type eventFact struct{ data map[string]interface{} }

func (e *eventFact) at(path string) interface{} { return getPath(e.data, path) }
func (e *eventFact) Str(path string) string     { s, _ := e.at(path).(string); return s }
func (e *eventFact) Bool(path string) bool      { b, _ := e.at(path).(bool); return b }
func (e *eventFact) Has(path string) bool       { return e.at(path) != nil }
func (e *eventFact) Num(path string) float64 {
	switch v := e.at(path).(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// getPath walks dotted path segments through nested maps and array indices.
func getPath(data map[string]interface{}, path string) interface{} {
	var cur interface{} = data
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]interface{}:
			cur = node[seg]
		case []interface{}:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil
			}
			cur = node[idx]
		default:
			return nil
		}
		if cur == nil {
			return nil
		}
	}
	return cur
}

// matchResult is the fact a matching rule writes to.
type matchResult struct{ Matched bool }

func (m *matchResult) Hit() { m.Matched = true }

// buildKB compiles a single GRL condition into a knowledge base. Isolating each
// rule means one rule's bad syntax can't break evaluation of the others.
func buildKB(condition string) (*ast.KnowledgeBase, error) {
	grl := fmt.Sprintf(`rule EdaMatch "eda" salience 10 {
    when
        %s
    then
        Result.Hit();
        Retract("EdaMatch");
}`, condition)
	kl := ast.NewKnowledgeLibrary()
	rb := builder.NewRuleBuilder(kl)
	if err := rb.BuildRuleFromResource("eda", "0.0.1", pkg.NewBytesResource([]byte(grl))); err != nil {
		return nil, err
	}
	return kl.NewKnowledgeBaseInstance("eda", "0.0.1")
}

// compileCondition validates a GRL condition without evaluating it (used at rule
// create time so a bad condition is rejected up front).
func compileCondition(condition string) error {
	_, err := buildKB(condition)
	return err
}

// ruleMatches evaluates a compiled condition against an event.
func ruleMatches(condition string, ev *eventFact) (bool, error) {
	kb, err := buildKB(condition)
	if err != nil {
		return false, err
	}
	res := &matchResult{}
	dctx := ast.NewDataContext()
	if err := dctx.Add("Event", ev); err != nil {
		return false, err
	}
	if err := dctx.Add("Result", res); err != nil {
		return false, err
	}
	if err := engine.NewGruleEngine().Execute(dctx, kb); err != nil {
		return false, err
	}
	return res.Matched, nil
}

// --- intake (public) ---

// Intake POST /api/v1/events/{source} — accepts a JSON event from an authenticated
// source, evaluates every enabled rule for that source, and launches the target of
// each match with the event as context. Public: authorized by the source token.
func (rs *EventsResource) Intake(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "source")
	// Unknown/disabled source is indistinguishable from not-found (don't reveal
	// which sources exist), matching the inbound-webhook behavior.
	src, err := rs.store.IntakeSource(r.Context(), name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tok := r.Header.Get("X-Praetor-Token")
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	if subtle.ConstantTimeCompare([]byte(tok), []byte(src.Token)) != 1 {
		http.Error(w, "token verification failed", http.StatusUnauthorized)
		return
	}

	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		render.ErrInvalidRequest(fmt.Errorf("event body must be a JSON object")).Render(w, r)
		return
	}
	ev := &eventFact{data: payload}

	// Rules scoped to this source's org, either bound to this source or global (NULL).
	rules, _ := rs.store.RulesForIntake(r.Context(), src.OrgID, src.ID)

	launched := []int64{}
	matched := 0
	for _, rl := range rules {
		ok, err := ruleMatches(rl.Condition, ev)
		if err != nil {
			logger.Error("eda rule condition error", "rule_id", rl.ID, "name", rl.Name, "err", err)
			continue
		}
		if !ok {
			continue
		}
		matched++
		limit := ""
		if rl.LimitField.Valid && rl.LimitField.String != "" {
			limit = ev.Str(rl.LimitField.String)
		}
		id, err := rs.launch(r.Context(), rl.Name, rl.UJTID, rl.WfID, payload, limit)
		if err != nil {
			logger.Error("eda rule launch failed", "rule_id", rl.ID, "err", err)
			continue
		}
		if id != 0 {
			launched = append(launched, id)
		}
	}

	launchedJSON, _ := json.Marshal(launched)
	if err := rs.store.InsertReceipt(r.Context(), src.ID, body, matched, launchedJSON); err != nil {
		logger.Error("eda receipt insert failed", "err", err)
	}

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"received": true, "matched": matched, "launched": launched})
}

// launch starts a rule's target with the event as context: extra_vars.eda_event is
// the full event, and limit (a value pulled from the event) becomes the run's
// --limit so remediation targets only the affected host. Honors the target
// template's allow_simultaneous concurrency guard (skip overlapping heals).
func (rs *EventsResource) launch(ctx context.Context, ruleName string, ujt, wf sql.NullInt64, payload map[string]interface{}, limit string) (int64, error) {
	// The event is context for whatever we launch: the full event under
	// extra_vars.eda_event, and (when the rule extracts one) the affected host as
	// --limit so remediation targets only it. Both the job and workflow targets
	// carry these; for a workflow they overlay every node job.
	opts := launch.Options{ExtraVars: map[string]interface{}{"eda_event": payload}}
	if limit != "" {
		opts.Limit = &limit
	}
	switch {
	case ujt.Valid:
		if !rs.store.JobTemplateAllowSimultaneous(ctx, ujt.Int64) {
			if rs.store.ActiveJobCount(ctx, ujt.Int64) > 0 {
				return 0, nil // a heal is already in flight; skip this event
			}
		}
		id, err := rs.store.InsertEventJob(ctx, ruleName+" (event)", ujt.Int64, opts)
		if isActiveRunConflict(err) {
			return 0, nil // a heal is already in flight; skip this event
		}
		return id, err
	case wf.Valid:
		// Snapshot the workflow into a run (same as an inbound workflow webhook),
		// carrying the event context + limit into the run's node jobs.
		return rs.store.LaunchWorkflowSnapshot(ctx, wf.Int64, opts)
	default:
		return 0, fmt.Errorf("rule %q has no target", ruleName)
	}
}

// --- sources CRUD (superuser) ---

func (rs *EventsResource) ListSources(w http.ResponseWriter, r *http.Request) {
	out, err := rs.store.ListSources(r.Context())
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, out)
}

func (rs *EventsResource) CreateSource(w http.ResponseWriter, r *http.Request) {
	if !rs.requireGlobal(w, r, rbac.CapManageEventSource) {
		return
	}
	var in eventSource
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || in.OrganizationID == 0 {
		render.ErrInvalidRequest(fmt.Errorf("name and organization_id are required")).Render(w, r)
		return
	}
	if strings.TrimSpace(in.Token) == "" {
		in.Token = genWebhookKey() // reuse the webhook shared-secret generator
	}
	created, err := rs.store.CreateSource(r.Context(), in)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	render.Created(w, r, created) // token returned once, here
}

func (rs *EventsResource) DeleteSource(w http.ResponseWriter, r *http.Request) {
	if !rs.requireGlobal(w, r, rbac.CapManageEventSource) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if err := rs.store.DeleteSource(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- rules CRUD (superuser) ---

func (rs *EventsResource) ListRules(w http.ResponseWriter, r *http.Request) {
	out, err := rs.store.ListRules(r.Context())
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, out)
}

func (rs *EventsResource) CreateRule(w http.ResponseWriter, r *http.Request) {
	if !rs.requireGlobal(w, r, rbac.CapManageEventSource) {
		return
	}
	var in eventRule
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || in.OrganizationID == 0 {
		render.ErrInvalidRequest(fmt.Errorf("name and organization_id are required")).Render(w, r)
		return
	}
	// Exactly one target (job or workflow template).
	if (in.UnifiedJobTemplateID == nil) == (in.WorkflowTemplateID == nil) {
		render.ErrInvalidRequest(fmt.Errorf("set exactly one of unified_job_template_id or workflow_template_id")).Render(w, r)
		return
	}
	if strings.TrimSpace(in.Condition) == "" {
		render.ErrInvalidRequest(fmt.Errorf("condition is required")).Render(w, r)
		return
	}
	// Reject an invalid GRL condition up front.
	if err := compileCondition(in.Condition); err != nil {
		render.ErrInvalidRequest(fmt.Errorf("invalid condition: %w", err)).Render(w, r)
		return
	}
	created, err := rs.store.CreateRule(r.Context(), in)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	render.Created(w, r, created)
}

func (rs *EventsResource) DeleteRule(w http.ResponseWriter, r *http.Request) {
	if !rs.requireGlobal(w, r, rbac.CapManageEventSource) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if err := rs.store.DeleteRule(r.Context(), id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
