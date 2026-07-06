package handlers

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hyperjumptech/grule-rule-engine/ast"
	"github.com/hyperjumptech/grule-rule-engine/builder"
	"github.com/hyperjumptech/grule-rule-engine/engine"
	"github.com/hyperjumptech/grule-rule-engine/pkg"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/services/api/render"
)

// EventsResource is Praetor's event-driven automation surface (EDA-style):
// authenticated event SOURCES push events, RULES evaluate a condition against each
// event with the grule rule engine, and a match LAUNCHES a job/workflow template
// with the event injected as context. It's the "source -> rulebook -> action" loop
// adapted to a push model: point Alertmanager/monitoring at /events/{source} and a
// matching rule heals the affected host.
type EventsResource struct{ DB *sqlx.DB }

func NewEventsResource(db *sqlx.DB) *EventsResource { return &EventsResource{DB: db} }

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
	var src struct {
		ID    int64  `db:"id"`
		OrgID int64  `db:"organization_id"`
		Token string `db:"token"`
	}
	// Unknown/disabled source is indistinguishable from not-found (don't reveal
	// which sources exist), matching the inbound-webhook behavior.
	if err := rs.DB.Get(&src,
		`SELECT id, organization_id, token FROM event_sources WHERE name=$1 AND enabled`, name); err != nil {
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

	var rules []struct {
		ID         int64          `db:"id"`
		Name       string         `db:"name"`
		Condition  string         `db:"condition"`
		UJTID      sql.NullInt64  `db:"unified_job_template_id"`
		WfID       sql.NullInt64  `db:"workflow_template_id"`
		LimitField sql.NullString `db:"limit_field"`
	}
	// Rules scoped to this source's org, either bound to this source or global (NULL).
	_ = rs.DB.Select(&rules,
		`SELECT id, name, condition, unified_job_template_id, workflow_template_id, limit_field
		 FROM event_rules
		 WHERE enabled AND organization_id=$1 AND (source_id=$2 OR source_id IS NULL)
		 ORDER BY id`, src.OrgID, src.ID)

	launched := []int64{}
	matched := 0
	for _, rl := range rules {
		ok, err := ruleMatches(rl.Condition, ev)
		if err != nil {
			log.Printf("eda: rule %d (%s) condition error: %v", rl.ID, rl.Name, err)
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
		id, err := rs.launch(rl.Name, rl.UJTID, rl.WfID, payload, limit)
		if err != nil {
			log.Printf("eda: rule %d launch failed: %v", rl.ID, err)
			continue
		}
		if id != 0 {
			launched = append(launched, id)
		}
	}

	launchedJSON, _ := json.Marshal(launched)
	if _, err := rs.DB.Exec(
		`INSERT INTO event_receipts (source_id, payload, matched, launched) VALUES ($1,$2,$3,$4)`,
		src.ID, body, matched, launchedJSON); err != nil {
		log.Printf("eda: receipt insert failed: %v", err)
	}

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"received": true, "matched": matched, "launched": launched})
}

// launch starts a rule's target with the event as context: extra_vars.eda_event is
// the full event, and limit (a value pulled from the event) becomes the run's
// --limit so remediation targets only the affected host. Honors the target
// template's allow_simultaneous concurrency guard (skip overlapping heals).
func (rs *EventsResource) launch(ruleName string, ujt, wf sql.NullInt64, payload map[string]interface{}, limit string) (int64, error) {
	switch {
	case ujt.Valid:
		var allowSim bool
		_ = rs.DB.Get(&allowSim, `SELECT allow_simultaneous FROM job_templates WHERE unified_job_template_id=$1`, ujt.Int64)
		if !allowSim {
			var active int
			_ = rs.DB.Get(&active,
				`SELECT count(*) FROM unified_jobs WHERE unified_job_template_id=$1 AND status NOT IN ('successful','failed','canceled','error')`,
				ujt.Int64)
			if active > 0 {
				return 0, nil // a heal is already in flight; skip this event
			}
		}
		args := map[string]interface{}{"extra_vars": map[string]interface{}{"eda_event": payload}}
		if limit != "" {
			args["limit"] = limit
		}
		jobArgs, _ := json.Marshal(args)
		var id int64
		err := rs.DB.QueryRowx(
			`INSERT INTO unified_jobs (name, unified_job_template_id, status, created_at, job_args)
			 VALUES ($1,$2,'pending',now(),$3) RETURNING id`,
			ruleName+" (event)", ujt.Int64, jobArgs).Scan(&id)
		if isActiveRunConflict(err) {
			return 0, nil // a heal is already in flight; skip this event
		}
		return id, err
	case wf.Valid:
		// Snapshot the workflow into a run (same as an inbound workflow webhook).
		tx, err := rs.DB.Beginx()
		if err != nil {
			return 0, err
		}
		defer tx.Rollback()
		var wjID int64
		if err := tx.QueryRowx(
			`INSERT INTO workflow_jobs (workflow_template_id, status) VALUES ($1,'running') RETURNING id`, wf.Int64).Scan(&wjID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(
			`INSERT INTO workflow_job_nodes (workflow_job_id, node_key, node_type, job_template_id, name, webhook_url, webhook_body, status)
			 SELECT $1, node_key, node_type, job_template_id, name, webhook_url, webhook_body, 'pending' FROM workflow_nodes WHERE workflow_template_id=$2`,
			wjID, wf.Int64); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(
			`INSERT INTO workflow_job_edges (workflow_job_id, parent_key, child_key, edge_type)
			 SELECT $1, parent_key, child_key, edge_type FROM workflow_node_edges WHERE workflow_template_id=$2`,
			wjID, wf.Int64); err != nil {
			return 0, err
		}
		return wjID, tx.Commit()
	default:
		return 0, fmt.Errorf("rule %q has no target", ruleName)
	}
}

// --- sources CRUD (superuser) ---

type eventSource struct {
	ID             int64     `json:"id" db:"id"`
	OrganizationID int64     `json:"organization_id" db:"organization_id"`
	Name           string    `json:"name" db:"name"`
	Token          string    `json:"token,omitempty" db:"token"`
	Enabled        *bool     `json:"enabled" db:"enabled"` // pointer: omitted -> default true
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

func (rs *EventsResource) ListSources(w http.ResponseWriter, r *http.Request) {
	out := []eventSource{}
	// Token is a secret — never returned by list.
	if err := rs.DB.SelectContext(r.Context(), &out,
		`SELECT id, organization_id, name, '' AS token, enabled, created_at FROM event_sources ORDER BY name`); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, out)
}

func (rs *EventsResource) CreateSource(w http.ResponseWriter, r *http.Request) {
	if !requireSuperuser(w, r) {
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
	var created eventSource
	if err := rs.DB.QueryRowxContext(r.Context(),
		`INSERT INTO event_sources (organization_id, name, token, enabled)
		 VALUES ($1,$2,$3, COALESCE($4,true))
		 RETURNING id, organization_id, name, token, enabled, created_at`,
		in.OrganizationID, in.Name, in.Token, in.Enabled).StructScan(&created); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	render.Created(w, r, created) // token returned once, here
}

func (rs *EventsResource) DeleteSource(w http.ResponseWriter, r *http.Request) {
	if !requireSuperuser(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if _, err := rs.DB.ExecContext(r.Context(), `DELETE FROM event_sources WHERE id=$1`, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- rules CRUD (superuser) ---

type eventRule struct {
	ID                   int64     `json:"id" db:"id"`
	OrganizationID       int64     `json:"organization_id" db:"organization_id"`
	Name                 string    `json:"name" db:"name"`
	Enabled              *bool     `json:"enabled" db:"enabled"` // pointer: omitted -> default true
	SourceID             *int64    `json:"source_id,omitempty" db:"source_id"`
	Condition            string    `json:"condition" db:"condition"`
	UnifiedJobTemplateID *int64    `json:"unified_job_template_id,omitempty" db:"unified_job_template_id"`
	WorkflowTemplateID   *int64    `json:"workflow_template_id,omitempty" db:"workflow_template_id"`
	LimitField           *string   `json:"limit_field,omitempty" db:"limit_field"`
	CreatedAt            time.Time `json:"created_at" db:"created_at"`
}

func (rs *EventsResource) ListRules(w http.ResponseWriter, r *http.Request) {
	out := []eventRule{}
	if err := rs.DB.SelectContext(r.Context(), &out,
		`SELECT id, organization_id, name, enabled, source_id, condition, unified_job_template_id, workflow_template_id, limit_field, created_at
		 FROM event_rules ORDER BY id`); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	render.JSON(w, r, out)
}

func (rs *EventsResource) CreateRule(w http.ResponseWriter, r *http.Request) {
	if !requireSuperuser(w, r) {
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
	var created eventRule
	if err := rs.DB.QueryRowxContext(r.Context(),
		`INSERT INTO event_rules (organization_id, name, enabled, source_id, condition, unified_job_template_id, workflow_template_id, limit_field)
		 VALUES ($1,$2, COALESCE($3,true), $4,$5,$6,$7,$8)
		 RETURNING id, organization_id, name, enabled, source_id, condition, unified_job_template_id, workflow_template_id, limit_field, created_at`,
		in.OrganizationID, in.Name, in.Enabled, in.SourceID, in.Condition, in.UnifiedJobTemplateID, in.WorkflowTemplateID, in.LimitField,
	).StructScan(&created); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	render.Created(w, r, created)
}

func (rs *EventsResource) DeleteRule(w http.ResponseWriter, r *http.Request) {
	if !requireSuperuser(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}
	if _, err := rs.DB.ExecContext(r.Context(), `DELETE FROM event_rules WHERE id=$1`, id); err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
