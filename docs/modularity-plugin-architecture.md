# Praetor Modularity & Plugin Architecture Proposal

*Prepared against the actual codebase at `github.com/praetordev/praetor` (Go 1.25). Every claim below is grounded in files read during this review; paths and line references are from the current `main`.*

---

## 0. Framing: what "plugin" should mean for Praetor

Praetor is a small-team, single-module Go product with six microservices sharing one Postgres schema and a NATS JetStream backbone. Go gives you three realistic extensibility patterns, in ascending order of cost:

1. **Data-driven definitions** — behavior described as DB rows / JSON schemas, interpreted by generic engine code. Zero recompile to add one. AWX credential types are the canonical example, and Praetor *already does this well* in `pkg/credentials`.
2. **Compile-time registry** — a package-level `map[string]Interface`, populated by `init()` self-registration or an explicit `Register()` call from one wiring point. Adding a plugin = dropping one file + one blank import. Recompile required, but Praetor ships as container images anyway — a recompile is a non-event.
3. **Out-of-process plugins** — subprocess/gRPC (hashicorp/go-plugin) or webhooks. Only justified when *third parties who can't touch your repo* must ship code. Heavy: versioned wire protocols, process supervision, security surface.

**Blunt position:** Praetor should not build a dynamic plugin system. `plugin.so` is a trap (exact-toolchain/GOPATH coupling, no Windows/macOS cross-grade, panics on version skew), and go-plugin's gRPC handshake machinery is enterprise-vendor tooling that a small maintainer team will spend more time feeding than benefiting from. There is no third-party plugin ecosystem to serve yet. What Praetor needs is **seams**: interfaces + registries so that adding the Nth notification backend or the Nth inventory source is a one-file, zero-switch-statement change — and data-driven definitions where the seam is config-shaped, which is exactly the AWX heritage.

The one place out-of-process is already the right call — and Praetor already made it — is **EDA event intake**: external systems push JSON over HTTP with a shared token (`POST /api/v1/events/{source}`). That *is* the webhook plugin model. Keep it.

---

## A. Seam assessment (as of current `main`)

### A.1 Credential types — **friction today: LOW** (already data-driven, missing one endpoint)

**Mechanism:** Fully AWX-style and data-driven. `credential_types` is a DB table with JSONB `inputs` (field schema: `{"fields":[{"id","label","type","secret"}]}`) and `injectors` (`{"env":{...},"file":{...}}` with `{{ field }}` substitution). Runtime resolution is 100% generic:

- `pkg/credentials/credentials.go:32` — `ResolveInjectors(ctx, q, credID)` loads credential + type, decrypts `secret:true` fields, renders injector templates. No per-type Go code anywhere.
- `services/api/handlers/credentials.go:279` — `maskCredentialSecrets` masks by reading the same schema. Encryption-on-write likewise schema-driven.
- Handler: `services/api/handlers/credential_types.go` — `CredentialTypesResource` with a `CredentialTypeStore` interface, but **only `ListAll`/`Get`** routes.

**Adding a type today:** edit the seed slice in `cmd/migrator/main.go` (~line 283 upsert loop: Machine, AWS, Azure RM, GCE...), rebuild + rerun the migrator. One file, but it's the *wrong* file — a product-config change requires a code release.

**Verdict:** the engine is done; only the management API is missing. This is a **data-driven seam** — do NOT build a Go registry here.

### A.2 Notifications — **friction today: HIGH** (hardcoded switch, no schema, no interface)

**Mechanism:** `notification_templates` table has `notification_type TEXT` (comment in `db/migrations/000022_notifications.up.sql`: `-- webhook | slack`) and a JSONB `config` that in practice only ever holds `{"url": "<encrypted>"}` — the create handler (`services/api/handlers/notifications.go:48`) hardcodes that shape and defaults type to `"webhook"`. Dispatch lives in `services/consumer/core/notifier.go`:

- `notifyEvent()` (line 31) maps `JOB_STARTED/COMPLETED/FAILED` → `started/success/error`.
- `Notifier.send()` (line 56) does a join query, then a **`switch r.Type` at line 91**: `"slack"` builds `{"text": ...}`, `default` builds a generic JSON body. Both are just an HTTP POST.

**Adding a backend today (e.g., email or PagerDuty):** touch (1) `notifier.go` switch + probably a non-HTTP transport bolted into `send()`, (2) `notifications.go` create-handler body/validation (it only accepts `url`), (3) the migration comment/enum expectations, (4) frontend form. No per-type config schema exists, so email (host/port/from/to) literally cannot be expressed. **This is the worst seam relative to how cheap it is to fix — the worked example in §C.**

### A.3 API resource registration — **friction today: MEDIUM** (boilerplate, but honest boilerplate)

**Mechanism:** `services/api/router.go:31` `NewRouter(db, cfg)` is a single 250-line function. Two idioms coexist:
- The good one: self-contained Resources — `handlers.NewTokensResource(db)` → `r.Mount("/tokens", rs.Routes())` (also jobs, templates, schedules, credentials, credential-types, execution-packs, triggers, hosts, groups).
- The legacy one: the god-object `ContentHandler` (`services/api/handlers/orgs.go:93` — holds 7 store interfaces + `LDAPConfigPath`) with routes spelled inline in `router.go` for orgs/users/teams/roles/projects/notification-templates/workflows.

**Adding a resource today:** 1 new handler file + 1 store file + 1–3 lines in `router.go`. That is genuinely fine. The real cost is the `ContentHandler` legacy pile and the inconsistency, not the mount lines.

### A.4 Event sources / EDA — **friction today: LOW for sources, MEDIUM for actions**

**Mechanism (verified):** Sources are pure DB rows (`event_sources`: org, name, token, enabled — `services/api/store/event_store.go:29`), intake is source-agnostic: `EventsResource.Intake()` (`services/api/handlers/events.go:177`) verifies the shared token (constant-time), parses arbitrary JSON, wraps it in an `eventFact` with dotted-path accessors (`Event.Str("labels.alertname")`, array indices supported), and evaluates each enabled rule's GRL condition via grule (`buildKB()` line 129 wraps the condition in a boilerplate `rule EdaMatch`). Rules are DB rows too (`event_rules`: `condition` GRL text + exactly one of `unified_job_template_id` / `workflow_template_id` + optional `limit_field`).

- **New event source** = an API POST, zero code. Genuinely plugin-ready already (the out-of-process seam done right).
- **New rule *action*** = schema change + `EventRule` struct + `CreateRule` validation + the if/else dispatch in `launch()` (`events.go:245-269`). Today only two actions exist (launch job template / launch workflow). Currently tolerable; becomes a registry candidate the day a third action (notify, webhook-out, set-fact) lands.
- Latent per-source need: vendor signature verification (GitHub-style HMAC vs bare token) and payload normalization would today be if-chains inside `Intake()`.

### A.5 Auth providers — **friction today: MEDIUM (interface exists; dispatch and config are hardcoded)**

**Mechanism (verified):** `Login` (`services/api/handlers/auth.go:54`) branches: local bcrypt if `password_hash != "" && ldap_dn IS NULL`; else LDAP iff `ContentHandler.LDAPConfigPath` is set and the YAML config `UsesLoginMapping()`. The crucial *good* bones: `pkg/auth/mapper.go:28` already defines

```go
type GroupResolver interface {
    AuthenticateAndResolve(username, password string) (*UserIdentity, error)
}
```

and `auth.Authenticate(ctx, db, cfg, resolver, user, pass)` is provider-agnostic on the resolver side: it upserts the user and applies `user_flags_by_group` / `organization_map` / `team_map` in one transaction (`mapper.go:44-82`). `UserIdentity` (DN, names, email, `Groups map[string]struct{}`) is a clean provider-neutral identity.

**Adding OIDC today** touches ~7 files: new `pkg/auth/oidc.go`, config loading in `pkg/auth/config.go` (currently `LDAPConfig`-only YAML file, path plumbed env→`cmd/api/main.go`→`api.Config`→`ContentHandler.LDAPConfigPath`), a new branch in `Login()`, a new field on `ContentHandler`, `router.go`, `cmd/api/main.go`. Also, the mapper's tx block is LDAP-flavored (`upsertLDAPUser`, `ldap_dn` column) and `LDAPConfig` is the parameter type, so "provider-agnostic" is only half-true. And OIDC is *shape-breaking*: it's a browser redirect flow, not username/password — the seam is not just "another resolver".

### A.6 SCM / inventory sources — **friction today: LOW–MEDIUM (and mostly the right kind)**

**SCM (verified):** git-only, hardcoded. `SyncProject()` (`services/api/handlers/projects.go:93-151`) does `exec.Command("git", "clone", "--depth", "1", ...)` at line 123. `projects.scm_type` is a TEXT column (schema comment says "git, svn, archive") with no switch anywhere — the column is a discriminator with one implementation. Adding SVN = add a switch in `SyncProject()` plus wherever the host-runner clones. One-file change per new type; no interface.

**Inventory sources (verified):** This seam is quietly Praetor's best design. `inventory_sources.source_kind` is `'inventory' | 'script'`; the executor (`services/executor/core/inventory_sync.go:18-93`) writes the `source` content to disk (mode 0755 if script), injects the credential's resolved env/files (from the data-driven credential system!), and runs **`ansible-inventory -i <source> --list`** — meaning *every Ansible inventory plugin (aws_ec2, azure_rm, gcp_compute...) already works with zero Praetor code*: create a source whose content is the plugin YAML and attach the matching seeded cloud credential. The migrator even pre-seeds AWS/Azure/GCE credential types with exactly the env vars those plugins read. Results POST back to ingestion's generic `UpsertInventory` (`services/ingestion/core/service.go:154-240`).

**Adding a new inventory source today:** for anything Ansible has a plugin for — zero code. Only a genuinely novel discovery mechanism (a Go-native poller, say) would need executor changes. Praetor is delegating the plugin system to Ansible here. Correct call; formalize, don't replace.

### A.7 Execution pack builders / runtime types — **friction today: HIGH by design (and that's correct)**

**Mechanism (verified):** `pkg/packspec.Spec` (packspec.go:24-38) is a strictly validated typed spec: version regexes on `Python`/`Ansible`/`AnsibleCore` (mutually exclusive), a pip-name regex that blocks flags/shell metacharacters, a hardcoded arch whitelist `{amd64, arm64}` (line 50), required `HostRunner` version. `cmd/packbuilder/main.go` drives one monolithic Dockerfile (`build/ansible-runtime/Dockerfile`) via `buildctl`, per-arch, publishing tarballs to Gitea's generic registry. The executor (`bootstrap_runner.go:83` `fetchPackTarball`, `:331` `pushRuntime`) detects target arch via `uname -m`, streams the tarball over SSH, extracts to `/opt/praetor/packs/<pack>/`, promotes the bundled `praetor-host-runner`.

**Adding a runtime variant today:** new arch = 3 files (`allowedArches`, Dockerfile python source, uname case). New engine/builder step = spec field + validation + Dockerfile build-arg + two CLIs. Deliberately rigid: the strict validation is the injection-safety boundary for the "self-contained pushable runtime" thesis. **Verdict: not a plugin seam. Do not pluginize the pack builder.** The extensibility already lives at the right level — the spec's `pip:` list and git-backed specs let users vary pack *contents* without touching the build *pipeline*.

### Seam assessment table

| # | Seam | Current mechanism | Add a new one today | Files touched | Friction |
|---|------|-------------------|---------------------|---------------|----------|
| 1 | Credential types | DB rows (JSONB `inputs`/`injectors`), generic resolver in `pkg/credentials`; seeds upserted by migrator | Edit seed slice in `cmd/migrator/main.go`, redeploy migrator | 1 (wrong one: code release for config) | **Low** (engine) / **Med** (authoring path) |
| 2 | EDA event sources | DB rows + token-verified generic JSON intake + grule GRL conditions | `POST /api/v1/event-sources` — zero code | 0 | **Low** |
| 2b | EDA rule actions | if/else on two nullable FK columns in `launch()` (`events.go:245`) | Migration + `EventRule` struct + validation + dispatch branch | 3 + migration | **Med** |
| 3 | Notification backends | `switch r.Type` in `services/consumer/core/notifier.go:91`; config shape hardcoded to `{url}` in create handler | Edit switch, edit handler validation, hope config fits `{url}` | 3–4 + UI; email impossible without schema work | **High** |
| 4 | Auth providers | Branch in `Login()` (`auth.go:54`); `GroupResolver` interface exists (`pkg/auth/mapper.go:28`) but config/plumbing/mapper are LDAP-shaped | New resolver + config type + `Login()` branch + `ContentHandler` field + router + main | ~7 | **Med–High** |
| 5 | SCM types | Hardcoded `git clone` in `SyncProject()` (`projects.go:123`); `scm_type` column unused as discriminator | Add switch cases in sync path(s) | 1–2 | **Med** (rarely needed) |
| 5b | Inventory sources | `source_kind` string; executor runs `ansible-inventory` against stored plugin config + injected credentials | Zero code for any Ansible inventory plugin | 0 | **Low** |
| 6 | Pack runtimes | Strictly validated `packspec.Spec` + monolithic Dockerfile + buildctl | Spec field + validation + Dockerfile + 2 CLIs | 4–5 | **High — intentionally; keep it** |
| 7 | API resources | Resource structs w/ `Routes() chi.Router` mounted in 250-line `NewRouter`; legacy `ContentHandler` god-object for 7 domains | New handler + store + mount line (good path) or grow `ContentHandler` (bad path) | 3 | **Med** (boilerplate + inconsistency) |

---

## B. Recommended architecture: three patterns, mapped decisively

### The registry primitive (build once, ~40 lines)

One tiny generic package, used by every compile-time seam so they all look alike:

```go
// pkg/registry/registry.go
package registry

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is a named, type-safe plugin table. Registration happens at init
// time from plugin files; lookups happen at request time. It is deliberately
// append-only: no Deregister, no runtime mutation after boot.
type Registry[T any] struct {
	kind string
	mu   sync.RWMutex
	m    map[string]T
}

func New[T any](kind string) *Registry[T] {
	return &Registry[T]{kind: kind, m: make(map[string]T)}
}

// Register panics on duplicates: a duplicate plugin name is a programmer
// error and must fail the build's smoke test, not surface at 3am.
func (r *Registry[T]) Register(name string, v T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.m[name]; dup {
		panic(fmt.Sprintf("%s: duplicate registration %q", r.kind, name))
	}
	r.m[name] = v
}

func (r *Registry[T]) Get(name string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.m[name]
	return v, ok
}

func (r *Registry[T]) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.m))
	for k := range r.m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
```

Conventions that keep this from becoming magic:
- **`init()` self-registration only within the owning package** (e.g., `pkg/notify/slack.go` registers into `pkg/notify`'s registry). Importing `pkg/notify` gets all built-ins; there is exactly one blank-import site per binary, none hidden.
- **Panic on duplicate, at init.** Praetor ships as images; a bad registration dies in CI, never in prod.
- **Routes stay explicit.** Do *not* use `init()` registries for chi routes — middleware ordering, auth scoping, and public-vs-protected placement are things you want to read top-to-bottom in `router.go`.

### Pattern per seam

| Seam | Pattern | Why |
|------|---------|-----|
| **1. Credential types** | **Data-driven (DB rows), full stop.** Add the missing `POST/PUT/DELETE /credential-types` (superuser, with a `managed` flag protecting migrator-seeded rows). | The interpreter (`ResolveInjectors`, masking, encryption) is already 100% generic. A Go registry here would be a *regression* — AWX proved this seam is config, not code. The only future code-shaped need is external secret lookup (Vault/CyberArk); if that lands, it becomes a *second*, registry-backed concept ("credential lookup plugin"), not a rework of types. |
| **2. EDA event sources** | **Already done: data-driven rows + out-of-process intake (HTTP+token).** Optionally later: a tiny registry of `PayloadVerifier` funcs keyed by an added `source_type` column, for vendor HMAC signatures (GitHub-style). | External systems pushing JSON *is* the out-of-process plugin model, with grule as the user-programmable matching layer. Don't wrap it in anything. |
| **2b. EDA rule actions** | **Compile-time registry — but only when the third action ships.** Today's two-way if/else in `launch()` is fine. When "notify"/"outbound webhook"/"set fact" arrives, introduce `eda.Action{Type() string; Validate(json.RawMessage) error; Execute(ctx, payload) error}` and replace the nullable-FK-columns pattern with a `(action_type, action_config JSONB)` pair. | Two hardcoded cases don't justify a framework; three or more do, because each new action currently means schema + struct + validation + dispatch edits in lockstep. |
| **3. Notification backends** | **Compile-time registry + data-driven config schema. Highest payoff. Do first.** (§C) | The switch is already wrong (email is inexpressible), the blast radius is small (one consumer file + one handler), and the schema-driven config machinery can be *copied* from what credentials already do — same `Field{id,label,secret}` shape, same encrypt/mask code path. |
| **4. Auth providers** | **Compile-time registry over a widened provider interface — build it when OIDC is actually scheduled, not before.** Keep `GroupResolver` + provider-neutral `UserIdentity`; generalize `Authenticate()`'s LDAP-flavored upsert (`upsertLDAPUser`, `ldap_dn`) into `upsertExternalUser(provider, externalID, identity)`. | The bones are right, but OIDC is *flow-breaking* (browser redirect, not username/password), so a password-shaped `Provider` interface designed today would be designed wrong. When it's real: `auth.Provider{Name(); LoginRoutes() chi.Router; Resolve(...) (*UserIdentity, error)}` registered per-provider, each mounting its own public routes under `/api/v1/auth/{provider}`. Out-of-process (SAML broker etc.) is not warranted — nobody third-party ships auth code into a small-team AAP. |
| **5. SCM types** | **Nothing now; a 20-line `map[string]func` when SCM #2 is demanded.** | One implementation behind a TEXT discriminator is not friction, it's YAGNI discipline. An `scm.Cloner` interface with a two-entry map is an afternoon *when a user asks for SVN*, which may be never. |
| **5b. Inventory sources** | **Data-driven, delegated to Ansible's plugin ecosystem (status quo, formalized).** Add a curated `source_kind` catalog table (name, description, example plugin YAML, suggested credential type) purely as UX metadata — zero engine change. | `ansible-inventory --list` + injected credentials means AWS/Azure/GCP/VMware inventory plugins already work. Praetor should ship *recipes*, not Go code, here. |
| **6. Pack runtimes** | **Neither. Keep the strict typed spec.** | The rigidity is the security boundary of the core product thesis (pushable self-contained runtime). Variability belongs in spec *data* (`pip:`, versions, git-backed specs) inside hard validation walls. A "pack builder plugin" is an injection vector with extra steps. |
| **7. API resources** | **Convention + a declarative mount table — not a registry.** Finish converting `ContentHandler` domains to the Resource pattern; collapse `router.go`'s protected section to a `[]mount{{path, handler.Routes()}}` loop. | Route wiring must stay greppable and ordering-explicit. The problem is the god-object and the inconsistency, not the mount lines. `init()`-registered routes are how codebases become haunted. |

**Where a plugin system is NOT worth it (explicit non-goals):** `plugin.so` anywhere; hashicorp/go-plugin anywhere; pluginizing packbuilder; workflow node types as plugins (the DAG engine's node semantics are core, not periphery); out-of-process credential resolution; a generic "extensions" DB table. Praetor's differentiator is the pack engine and the AAP domain model — plugin machinery is only valuable where it removes *recurring* one-file-should-have-been-enough friction.

---

## C. Worked example: notification backends

### Before — adding PagerDuty today touches 4+ places

1. `services/consumer/core/notifier.go` — extend the `switch r.Type` at line 91; PagerDuty needs a routing key + severity, which don't fit the `{url}` config, so also rework the config unmarshal at line 78.
2. `services/api/handlers/notifications.go` — `CreateNotificationTemplate` hardcodes `{OrganizationID, Name, NotificationType, URL}` and `crypto.EncryptSecret(body.URL)`; needs a new body shape and per-type validation.
3. `db/migrations/000022_notifications.up.sql` — the `-- webhook | slack` contract comment (and any UI enum) drifts.
4. Frontend form — no schema endpoint exists to drive it, so it's hardcoded too.

And there is still no way for the API to *tell* anyone which types exist: `notification_type` is validated nowhere.

### After — the seam

**New package `pkg/notify`** (shared, like `pkg/credentials` — note the standing rule: editing a shared pkg means rebuilding every service that imports it; here that's `api` and `consumer` only).

```go
// pkg/notify/notify.go
package notify

import (
	"context"

	"github.com/praetordev/praetor/pkg/registry"
)

// Message is the backend-agnostic notification content built by the consumer
// when a job reaches a lifecycle event.
type Message struct {
	JobID   int64  `json:"job_id"`
	JobName string `json:"job_name"`
	Event   string `json:"event"`  // started | success | error
	Status  string `json:"status"` // human verb: started | succeeded | failed
}

// Field describes one config input, mirroring the credential_types.inputs
// field shape ({id,label,type,secret}) so the frontend and the encrypt/mask
// helpers treat both identically.
type Field struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Type    string `json:"type"` // text | password | textarea
	Secret  bool   `json:"secret,omitempty"`
	Default string `json:"default,omitempty"`
}

// Backend delivers notifications for one notification_type.
type Backend interface {
	// Type is the notification_templates.notification_type discriminator.
	Type() string
	// ConfigFields is the config schema: drives the create-form, validation,
	// and which config keys are encrypted at rest / masked on read.
	ConfigFields() []Field
	// Send delivers msg using cfg. Secret fields arrive already decrypted.
	// Implementations must respect ctx (the consumer sends with a timeout).
	Send(ctx context.Context, cfg map[string]string, msg Message) error
}

// Backends is the process-wide backend registry. Backend files in this
// package self-register in init(); importing pkg/notify is sufficient.
var Backends = registry.New[Backend]("notify backend")
```

```go
// pkg/notify/webhook.go
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func init() { Backends.Register("webhook", &webhookBackend{}) }

type webhookBackend struct{}

func (webhookBackend) Type() string { return "webhook" }

func (webhookBackend) ConfigFields() []Field {
	return []Field{{ID: "url", Label: "Target URL", Type: "text", Secret: true}}
}

func (webhookBackend) Send(ctx context.Context, cfg map[string]string, msg Message) error {
	body, _ := json.Marshal(msg)
	return postJSON(ctx, cfg["url"], body)
}

// postJSON is shared by the HTTP-shaped backends.
func postJSON(ctx context.Context, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("notification endpoint returned %d", resp.StatusCode)
	}
	return nil
}
```

```go
// pkg/notify/slack.go
package notify

import (
	"context"
	"encoding/json"
	"fmt"
)

func init() { Backends.Register("slack", &slackBackend{}) }

type slackBackend struct{}

func (slackBackend) Type() string { return "slack" }

func (slackBackend) ConfigFields() []Field {
	return []Field{{ID: "url", Label: "Webhook URL", Type: "text", Secret: true}}
}

func (slackBackend) Send(ctx context.Context, cfg map[string]string, msg Message) error {
	body, _ := json.Marshal(map[string]string{
		"text": fmt.Sprintf("Praetor job %q %s", msg.JobName, msg.Status),
	})
	return postJSON(ctx, cfg["url"], body)
}
```

**The consumer's switch dies.** `services/consumer/core/notifier.go:77-113` becomes:

```go
for _, r := range rows {
	backend, ok := notify.Backends.Get(r.Type)
	if !ok {
		logger.Error("notifier unknown backend", "type", r.Type, "job_id", jobID)
		continue
	}
	cfg := decryptConfig(backend.ConfigFields(), r.Config) // generic: decrypt Secret fields
	if err := backend.Send(ctx, cfg, notify.Message{
		JobID: jobID, JobName: r.JobName, Event: ev, Status: verb,
	}); err != nil {
		logger.Error("notifier send failed", "type", r.Type, "job_id", jobID, "err", err)
		continue
	}
	logger.Info("notifier sent notification", "type", r.Type, "job_id", jobID, "event", ev)
}
```

**The API grows one discovery endpoint and loses its hardcoded body.** In `services/api/handlers/notifications.go`, following the existing Resource idiom (cf. `tokens.go`):

```go
// GET /api/v1/notification-types — drives the frontend form, replaces the
// migration-comment "enum".
func (h *ContentHandler) ListNotificationTypes(w http.ResponseWriter, r *http.Request) {
	out := make([]map[string]interface{}, 0)
	for _, name := range notify.Backends.Names() {
		b, _ := notify.Backends.Get(name)
		out = append(out, map[string]interface{}{"type": name, "fields": b.ConfigFields()})
	}
	render.JSON(w, r, out)
}
```

and `CreateNotificationTemplate` swaps its `{url}`-only body for `Config map[string]string`, validated against `backend.ConfigFields()` (required fields present, no unknown keys) with `Secret: true` fields passed through `crypto.EncryptSecret` — the exact logic `credentials.go` already implements for credential inputs, extracted into a small shared helper.

### After — adding PagerDuty is one file

```go
// pkg/notify/pagerduty.go
package notify

import (
	"context"
	"encoding/json"
	"fmt"
)

func init() { Backends.Register("pagerduty", &pagerdutyBackend{}) }

type pagerdutyBackend struct{}

func (pagerdutyBackend) Type() string { return "pagerduty" }

func (pagerdutyBackend) ConfigFields() []Field {
	return []Field{
		{ID: "routing_key", Label: "Integration Routing Key", Type: "password", Secret: true},
		{ID: "severity", Label: "Severity", Type: "text", Default: "error"},
	}
}

func (pagerdutyBackend) Send(ctx context.Context, cfg map[string]string, msg Message) error {
	if msg.Event == "started" {
		return nil // only page on terminal states
	}
	body, _ := json.Marshal(map[string]interface{}{
		"routing_key":  cfg["routing_key"],
		"event_action": "trigger",
		"payload": map[string]string{
			"summary":  fmt.Sprintf("Praetor job %q %s", msg.JobName, msg.Status),
			"source":   "praetor",
			"severity": cfg["severity"],
		},
	})
	return postJSON(ctx, "https://events.pagerduty.com/v2/enqueue", body)
}
```

Drop the file in `pkg/notify/`, rebuild. The consumer dispatches it, the API validates and encrypts its config against its own schema, and the frontend form renders itself from `/notification-types`. Zero edits to `notifier.go`, `notifications.go`, migrations, or the router. Tests are table-driven against the `Backend` interface with a fake `Message` — the same seam style as the existing store-interface fakes.

---

## D. Phased migration plan (incremental; each phase ships alone)

**Phase 1 — the primitive + notifications (proof).** ~2–4 days.
Build `pkg/registry` (with a `Names()`-ordering test and a duplicate-panic test). Build `pkg/notify` with `webhook` + `slack` ports (byte-compatible payloads with today's `notifier.go` output — verify against a captured request). Rewire `Notifier.send`, add `/notification-types`, generalize the create handler. *Risks:* config stored by old handler is `{"url": "<encrypted>"}` — the new decryptConfig must read existing rows unchanged (it does: `url` remains the field ID, still secret). Remember the shared-pkg rule: rebuild **api + consumer** images together. No schema migration needed.

**Phase 2 — credential types become user-manageable.** ~2–3 days.
Add `managed BOOLEAN NOT NULL DEFAULT false` to `credential_types`; migrator seeds set `managed=true`. Add `Create/Update/Delete` to `CredentialTypeStore` + routes on the existing `CredentialTypesResource` (superuser-gated; refuse mutation of `managed` rows; validate `inputs`/`injectors` JSON shape server-side — reuse the schema structs from `pkg/credentials`). *Risk:* injector templates are executed into job env — validate field IDs referenced in injectors exist in inputs, and forbid injectors targeting reserved env (e.g. `ANSIBLE_*_FILE` collisions are fine, but document it). This closes the "config change needs a code release" hole with zero engine changes.

**Phase 3 — dissolve `ContentHandler`, normalize the Resource pattern.** ~1 week, mechanical, can be done one domain per PR.
Extract `OrgsResource`, `UsersResource`, `TeamsResource`, `RolesResource`, `ProjectsResource`, `NotificationTemplatesResource`, `WorkflowsResource` (already half-split) each with their existing store interface. `router.go`'s protected block becomes a declarative table:

```go
for _, m := range []struct {
	path string
	h    interface{ Routes() chi.Router }
}{
	{"/organizations", handlers.NewOrgsResource(db)},
	{"/tokens", handlers.NewTokensResource(db)},
	// ...
} {
	r.Mount(m.path, m.h.Routes())
}
```

*Risk:* pure refactor churn; mitigate with route-table snapshot test (walk `chi.Routes()` and golden-file the method+pattern list) written *before* the first extraction. This is the highest-LOC phase but lowest conceptual risk, and it's what makes "add a resource = add a file + one table row" true.

**Phase 4 — auth provider seam hardening (gated on OIDC actually being scheduled).**
Pre-work that's safe now: rename the mapper's LDAP-specific upsert to a provider-neutral `upsertExternalUser` and split `LDAPConfig`'s mapping section (`user_flags_by_group`/`organization_map`/`team_map` + `GroupMatch`) into a provider-agnostic `MappingConfig` — LDAP keeps embedding it, OIDC will too. Defer the `Provider` registry + per-provider route mounting until the OIDC flow's real shape (redirect + callback) is on the roadmap; designing it against password-auth today guarantees a redesign. *Risk if rushed:* wrong interface shape; *risk if deferred:* none — the seam is used ~once a year.

**Phase 5 — on-demand seams (do none of these until the trigger fires).**
- Third EDA rule action requested → `eda.Action` registry + `(action_type, action_config)` migration replacing the nullable-FK pair.
- Vendor-signed event sources (GitHub HMAC etc.) → `source_type` column + `PayloadVerifier` registry in the intake path.
- Second SCM type requested → `scm.Cloner` map in the sync path.
- Inventory-source catalog table (UX metadata only) → whenever the UI wants a picker.

**Sequencing rationale:** Phase 1 proves the registry idiom on the seam with the worst friction-to-effort ratio and establishes the schema-driven-config helper that Phase 2 reuses. Phase 3 is independent and interleavable. Nothing is big-bang; every phase leaves `main` shippable, and the k3d Helm deploy needs only the images whose services changed (api+consumer in P1, api+migrator in P2, api in P3).

---

## Closing judgment

Praetor's instincts are already right in the two places that matter most: credentials (data-driven) and inventory (delegate to Ansible's plugin ecosystem). The pack pipeline's rigidity is a feature. The actionable debt is narrow: one hardcoded switch (notifications), one god-object (`ContentHandler`), one missing management API (credential types), and one half-finished interface (auth). A 40-line generic registry plus the discipline of "data where it's config-shaped, registry where it's code-shaped, explicit routes always" covers everything on the horizon — without importing a plugin framework the team would spend the next year maintaining.

