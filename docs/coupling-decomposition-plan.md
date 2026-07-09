# Praetor: Coupling Hotspots & Domain Decomposition Plan

*Sequel to `docs/modularity-plugin-architecture.md` (the "seams" doc). That doc answered "how should a variant be added" (registry vs data-driven). This one answers the maintainer's actual worry: "when I add the Nth feature, how many files and services do I have to touch, and why?" Every claim below is grounded in current `main`; file:line references verified.*

---

## 0. The headline finding

The worst coupling in Praetor is **not** the switch statements the previous doc catalogued. It is the **launch/dispatch pipeline**: the path from "someone wants a job to run" to "ansible-playbook argv on a target host" is smeared across **6 hand-rolled `INSERT INTO unified_jobs` sites in 2 services, a JSON side-channel (`job_args`) with no shared type, a 25-field manifest struct 3 parties read/write, and a separately-versioned host-runner binary**. This is the pipeline every flagship AAP feature flows through (launch prompts, surveys, tags, verbosity, timeouts, instance selection, provisioning callbacks…), so its tax is paid on almost every release.

The proof it's already hurting — the pipeline has silently dropped two shipped features:

1. **`JobTemplate.Verbosity` and `Forks` never reach Ansible.** They're columns, model fields (`pkg/models/resources.go:89,91`), accepted and stored by the API (`services/api/store/template_store.go:79,109`) — and referenced nowhere in `pkg/events.JobManifest`, the scheduler's manifest build (`scheduler.go:312-324`), or the host-runner's `playArgs` (`cmd/host-runner/runner.go:205-210`). Users set verbosity in the UI; it does nothing.
2. **`Schedule.ExtraVars` is stored and silently ignored.** The schedules handler persists it (`services/api/handlers/schedules.go:138,194`), the model carries it (`resources.go:116`), but `processSchedules` launches via `launchTarget(ctx, tx, sched.Name, sched.WorkflowTemplateID, sched.UnifiedJobTemplateID)` (`scheduler.go:480`) — which takes no overrides and inserts `unified_jobs` with **no `job_args`** (`triggers.go:50-52`). A scheduled run cannot carry extra vars, despite the UI/API accepting them.

These aren't sloppiness; they're what a 6-joint pipeline does to a small team. Each joint was edited by whoever added that launch surface, and nothing forces the joints to agree. **Fix the pipeline's shape and the Nth launch feature becomes a one-unit change; leave it and every one of these features costs 10+ files across 3 services plus a host-runner release.**

---

## 1. Coupling hotspot register (ranked by pain × frequency)

| # | Hotspot | What fuses to what | Concrete feature that pays the tax | Files touched today | Pain | Frequency |
|---|---------|--------------------|------------------------------------|--------------------|------|-----------|
| **H1** | **Launch pipeline** — 6 `INSERT INTO unified_jobs` sites: `job_store.go:147`, `webhook_store.go:69`, `event_store.go:107`, `inventory_store.go:149` (api) + `triggers.go:51`, `workflow.go:247` (scheduler); overrides ride an untyped `job_args` JSON blob parsed only by `scheduler/core/launch_args.go` | Every launch surface (manual, webhook, EDA, schedule, event trigger, workflow node) re-implements "create a job", each with different fields; override gating (`ask_*`) lives only in `handlers/jobs.go:173-190`, so 4 of 6 surfaces can't carry overrides at all | **"Add `ask_tags_on_launch` (job tags at launch)"**: migration + `models/resources.go` + `store/columns.go` + `templates.go` + `jobs.go` (LaunchRequest+gate) + `job_store.go` + `webhook_store.go` + `event_store.go` + `launch_args.go` + `scheduler.go` manifest + `pkg/events/schemas.go` + `cmd/host-runner/runner.go` + host-runner version bump/deploy | **12–13 files, 3 services + host-runner release train** | **High** | **High** — this is the product's main feature axis |
| **H2** | **`pkg/events.JobManifest`** (`schemas.go:49-116`) — 25+ fields, 3-party contract: scheduler writes it (2 sites: normal + inventory-sync), executor *mutates* it in flight (`bootstrap_runner.go:157-208`: fills CredentialEnv, Inventory, CachedFacts, IngestToken), host-runner reads it **on targets, on its own release train** (imports `pkg/events` — the only shared pkg it uses) | One struct fuses: inventory resolution, credential injection, galaxy config, pack selection, runner-host routing, ingest auth, AND a second job kind (inventory sync) smuggled in via `InventorySync bool` | **"Add a per-job SSH port / connection timeout"**: JobManifest field + scheduler build + executor passthrough + host-runner consume + backward-compat reasoning for old runners already deployed on hosts (memory: WAL/versioning rules) | 4–5 files, but the real cost is **cross-version compatibility reasoning with deployed host-runners** each time | **High** | Med–High |
| **H3** | **`pkg/models` + `SELECT *` in the scheduler** — scheduler does `SELECT * FROM job_templates/projects/inventories/hosts` (`scheduler.go:215,226,246,268,278`) into shared structs; API mitigated this with `store/columns.go` + reflection test, scheduler didn't | Any new column on those 4 tables couples migration→every-binary rebuild; skew fails at **runtime** ("missing destination name X") in the *dispatch path*, i.e. jobs stop launching | **"Add `hosts.ssh_port` column"**: migration + models + columns.go + hosts handler — and the scheduler breaks at runtime unless rebuilt/redeployed in lockstep (the maintainer already carries this as a memorized rule) | 4 files + all-images rebuild ritual | **Med-High** | High — every schema change |
| **H4** | **`ContentHandler` god-object** (`handlers/orgs.go:93-107`) — 7 store interfaces (orgs, projects, roles, teams, access, users, notifications) + `Authorizer` + `LDAPConfigPath`; ~40 handler methods across 7 domains; routes spelled inline in `router.go:105-236` while 12 other resources use the self-contained Resource pattern | Identity (users/teams/orgs/roles), projects/SCM, notifications, and auth (Login) all share one struct — a change in any forces navigating all; every new resource author must choose between two idioms | **"Add OIDC login"** (~7 files, per prior doc §A.5) or **"add org-level settings field"**: touches orgs.go in a file that also contains Login and notification handlers | 3–7 files, all funneling through one 546-line file | **Med** | Med — hit whenever identity/projects/notifications change |
| **H5** | **RBAC new-resource ritual** — `pkg/rbac/types.go` ContentType consts (10 today) + **three** switches in `access.go` (`OrgForContent:203`, `FilterAccessibleIDs:256,282`) + a ~140-line copy-adapted trigger migration per resource (pattern: `000011`, `000048`, `000049_workflow_template_rbac`) + handler authz calls | RBAC knowledge is split between Go switches and SQL triggers that must agree on role_field names | **"Make execution packs (or schedules) RBAC-scoped"**: new ContentType + 3 switch edits + 140-line migration cloned from 000049 + handler gating | ~5 files + a large hand-adapted migration | **Med** | Med — every new first-class resource |
| **H6** | **Workflow node dispatch** — node behavior is an if/else ladder in `advanceWorkflow` (`workflow.go:192-257`: approval / webhook_in / webhook_out / job) + `wfTerminal`/`wfEdgeFires` state lists (`workflow.go:84-104`) + the snapshot INSERT's hand-listed column set (`triggers.go:39-42`) + node validation in `handlers/workflows.go` | Adding a node type means synchronized edits to dispatch, terminal-state predicate, snapshot columns, and API validation; workflow-node jobs also launch with **no `job_args`** (`workflow.go:247`) so nodes can't carry per-node overrides | **"Add a 'pause/timer' node"**: `workflow.go` (3 spots) + `triggers.go` snapshot + `workflows.go` validation + migration for the config column | 4 files + migration, all must agree | **Med** | Low-Med |
| **H7** | **Notification backends** — `switch r.Type` in `consumer/core/notifier.go:91`, config shape hardcoded in `handlers/notifications.go` | (Fully analyzed in prior doc §A.2/§C — unchanged) | "Add email/PagerDuty": impossible without schema work | 3–4 files + UI | **High per incident** | Low until asked, then blocking |
| **H8** | **Job lifecycle event consts** — `pkg/events` consts consumed by switch ladders in `consumer/core/db_writer.go:127-160` and `notifier.go:32-37`, emitted by executor + host-runner, relayed by ingestion | New lifecycle/narration event = emit site + two consumer switches + (if host-runner emits it) runner release | "Add JOB_RETRYING / notify-on-canceled": 3–4 files across 3 services | 3–4 files | **Low-Med** | Low |

**Deliberately absent:** credential types, EDA sources/rules, inventory sources, pack pipeline — the prior doc established these are healthy (data-driven or intentionally rigid); nothing found this pass contradicts that.

---

## 2. Target domain decomposition

Praetor's tables and services already imply clean domains; the code just doesn't respect two boundaries (launch and identity). Target state — 8 bounded domains:

| Domain | Owns (tables) | Public seam (what others may call) | Current violations |
|---|---|---|---|
| **Identity & Access** | organizations, users, teams, roles, role_members, team_roles, team_members, tokens | `rbac.AccessChecker` (exists, good) + an `auth.Authenticate` provider seam (prior doc §A.5) | Fused into `ContentHandler` with projects+notifications; mapper is LDAP-flavored; `access.go` needs per-ContentType edits from other domains (H5) |
| **Projects / SCM** | projects | `ProjectsResource` + "give me URL/ref for template X" | Lives inside `ContentHandler`; scheduler reads `projects` directly via `SELECT *` (`scheduler.go:226`) |
| **Inventory** | inventories, hosts, groups, host_groups, inventory_sources, facts | Resources (exist) + ingestion's `ResolveInventory`/`ResolveFacts` (exists, good — by-reference is the right seam) | Scheduler reads hosts/inventories via `SELECT *`; inventory-sync jobs smuggled through JobManifest as a bool flag |
| **Credentials** | credential_types, credentials, org galaxy credential links | `pkg/credentials.ResolveInjectors` + ingestion's run-scoped resolve (both exist, both good) | None material. Praetor's best domain. |
| **Templates & Launch** ← *the missing domain* | job_templates, surveys, unified_jobs (creation), job_args semantics | **`launch.Launch(ctx, tx, target, Options)`** — one function, one typed `Options{ExtraVars, Limit, …}` struct, one place that gates by `ask_*` flags. All 6 current insert sites become callers. | Today this domain *does not exist*: its logic is split across `handlers/jobs.go:173-190`, 4 store files, `launch_args.go`, and 2 scheduler files (H1) |
| **Execution** | execution_runs, execution_outbox, job_events, chunks; JobManifest; packs (execution_packs + packspec) | `manifest.Build(tx, job) (JobManifest, error)` + the outbox; NATS subjects; ingestion's run-token'd intake (exists) | Manifest assembly is inlined in the scheduler's claim transaction (`scheduler.go:206-360`) with cross-domain `SELECT *`s into templates/projects/inventories/hosts |
| **Workflows** | workflow_templates, workflow_nodes/edges, workflow_jobs/nodes/edges | `WorkflowsResource` + "launch workflow template X" (should itself call the Launch seam) | Node-type ladder (H6); node launch bypasses overrides; snapshot column list duplicated |
| **Automation triggers** (schedules, event_triggers, webhooks, EDA sources/rules) | schedules, event_triggers, event_trigger_fires, event_sources, event_rules, webhook config on templates | They are pure *callers* of Launch (and Workflows); own no execution semantics | Each re-implements job insertion (H1); schedules drop their own ExtraVars |
| **Notifications** | notification_templates, jt-notification links | `notify.Backend` registry (prior doc §C) | Switch in consumer; handler hardcodes `{url}` |

**The load-bearing insight:** five of the eight domains (Templates&Launch, Workflows, Triggers, Execution, Notifications) meet at exactly one choke point — *starting a job*. Give that choke point a real API and most of the register's High rows collapse.

---

## 3. Decomposition backlog (ordered; each item one-PR-sized; `main` shippable throughout)

Reconciliation with the prior doc's §D phases: items B3/B6/B7 below **are** its Phases 1–3 unchanged in content; this backlog re-sequences them behind the launch-pipeline work, which the seam analysis missed because it's vertical (a pipeline), not horizontal (a variant point). Prior Phase 4/5 triggers still stand.

**B1. Guard rails first.** *(S, near-zero risk; rebuild: none — test-only)*
- chi route-table golden test: walk `chi.Routes()`, golden-file method+pattern (this was prior-doc Phase 3's mitigation; write it now, it guards B6 too).
- Launch contract test: table-driven test asserting, for each of the 6 launch surfaces, exactly what lands in `unified_jobs` (status, template id, `job_args` keys). It will *document today's divergence* (schedules/triggers/workflow nodes carry no args) and freeze behavior before B2 moves it.
- Manifest golden test: marshal a fully-populated `events.ExecutionRequest`, golden-file the JSON. Any accidental wire change to the host-runner contract now fails CI.

**B2. Extract the Launch domain: `pkg/launch`.** *(M; risk: behavior drift on the 6 surfaces — de-risked by B1's contract test; rebuild: api + scheduler)*
- `launch.Options{ExtraVars map[string]any, Limit *string, InventorySourceID int64}` — the typed replacement for ad-hoc `job_args` JSON; `Options.Gate(tpl)` applies `ask_*`/survey rules (moved from `handlers/jobs.go:173-190`); `launch.Job(ctx, ex, name, ujtID, opts)` and `launch.Workflow(ctx, ex, wfID, opts)` (absorbing `launchTarget` — it already takes the `sqlExec` interface, so tx/db both work).
- Convert all 6 insert sites to callers. Delete `scheduler/core/launch_args.go` (its parse/merge helpers move into `pkg/launch`, tested once).
- **Ship the two bug fixes as the proof of value:** schedules pass their stored `ExtraVars`; workflow nodes gain an (initially empty) options path. Before→after blast radius for the next launch-time feature: 6 files/2 services → **1 package + the manifest leg**.

**B3. Notifications registry (prior doc Phase 1, unchanged).** *(M; rebuild: api + consumer)* Independent of B2; can run in parallel. Kept at #3 because it's blocking-when-asked rather than taxing-every-release.

**B4. Extract manifest assembly: `manifest.Build`.** *(M; risk: dispatch regression — de-risked by B1's manifest golden + existing chaos tests; rebuild: scheduler)*
- Move `scheduler.go:206-360`'s template/project/inventory/host/pack resolution into a `services/scheduler/core/manifest.go` (or `pkg/manifest` if the executor ever needs it) with **explicit column lists** replacing the five `SELECT *`s — reusing `store.Prefixed`/columns constants (export them from a shared spot; they already exist and are reflection-tested).
- Split the inventory-sync path into its own builder so `InventorySync bool` stops overloading the job manifest ("a second job kind smuggled through" — make it explicit even if the wire shape stays identical for old runners).
- Wire `Forks`/`Verbosity` template→manifest→host-runner `playArgs` here (one manifest field + `runner.go:205` sibling lines + runner minor release), closing the dropped-feature gap and proving the new one-place-to-edit shape.
- After this, adding a schema column no longer risks runtime scan failures in the dispatch path; the memorized "rebuild everything" rule relaxes to "rebuild what the columns test says".

**B5. JobManifest compatibility discipline.** *(S; rebuild: none required)*
- Add `ManifestVersion int` to `ExecutionRequest`, a one-page compat rule in `pkg/events` doc comments (additive-only fields; host-runner must tolerate unknown fields — it already does via JSON), and extend B1's golden test to pin the *minimum* fields each supported host-runner version needs. This converts "reason about deployed runners from memory" into a checked contract. (Mirrors the walFormat rules already established for the reverse direction.)

**B6. Dissolve `ContentHandler` (prior doc Phase 3, unchanged), one domain per PR.** *(M total, mechanical; risk: route drift — covered by B1's golden test; rebuild: api only)* Order: Projects first (it unblocks nothing but is smallest), then Notifications (aligns with B3's handler rework), then Users/Teams/Orgs/Roles (identity cluster), leaving `auth.go` Login as its own `AuthResource` holding `LDAPConfigPath` — which is the prep for prior-doc Phase 4 (OIDC) without designing the provider interface early.
- End state: `router.go`'s protected block is a declarative mount table; adding a resource = 1 handler + 1 store + 1 table row.

**B7. Credential-types management API (prior doc Phase 2, unchanged).** *(S-M; rebuild: api + migrator)* Unchanged in scope; sequenced after the launch work because it's a capability gap, not a coupling tax.

**B8. RBAC resource kit.** *(S-M; rebuild: api + any service importing rbac)*
- Collapse `access.go`'s three ContentType switches into one declarative `map[ContentType]resourceMeta{orgFKQuery, table}` consulted by `OrgForContent`/`FilterAccessibleIDs` — new resource = one map entry.
- Turn the 000049 trigger pattern into a documented, parameterized SQL template (a `db/migrations/TEMPLATE_rbac_resource.sql` with `:resource`/`:role_edges` placeholders to copy) — *not* runtime machinery. Guard: the existing rbac cascade tests (`rbac_cascade_test.go` et al.) already cover the semantics; add one for the new map.

**B9. Workflow node table-ification.** *(S; rebuild: scheduler + api)* Replace the if/else ladder with a small in-package `map[string]nodeBehavior{onEligible func, terminalStates []string}` + derive the snapshot column list from one shared const. Explicitly **not** a plugin registry (prior doc's non-goal stands) — just one-place-per-node-type inside the scheduler. Do when the next node type is actually scheduled.

Sequencing logic: B1 makes everything else safe. B2+B4 kill the top two register rows and pay for themselves immediately (three latent bugs fixed). B3/B6/B7 are the prior doc's plan, unblocked and guarded. B5/B8/B9 are cheap hardening that ride along when their area is next touched.

---

## 4. Explicit non-targets (coupling that is fine)

- **One shared Postgres across six services.** It looks like the textbook sin; it is the small team's superpower (transactional outbox, SKIP LOCKED claiming, advisory-locked workflow advancement all depend on it). Splitting databases or putting a service in front of every table would multiply every feature's cost. Keep it; the discipline needed is column-list hygiene (B4), not service boundaries.
- **`pkg/models` as one package.** Splitting into per-domain model packages would touch every import in the repo for zero behavior change; the actual pain is the `SELECT *` runtime skew, fixed by B4. Revisit only if two domains ever need *conflicting* versions of a struct (unlikely in one module).
- **The scheduler's tick-task monolith** (`scheduler.go` 761 lines). It's long but linear, and the `tickTask` decomposition already gives per-pass isolation. After B2/B4 remove launch+manifest logic it shrinks to its real job (claiming, relaying, reaping). No further split.
- **Router mount lines / chi wiring.** 250 explicit lines you can read top-to-bottom is a feature (prior doc §B stands). B6 shortens it as a side effect; do not registry-fy routes.
- **Pack pipeline rigidity, EDA intake, credential engine, inventory-source delegation to Ansible** — all reaffirmed healthy; see prior doc §A.1/§A.4/§A.6/§A.7.
- **NATS subject/versioning framework.** Two subjects, one producer/consumer pair each. B5's golden test + additive-only rule is the entire versioning story Praetor needs; a schema registry would be churn.
- **The consumer's lifecycle switches** (H8) at current size. Two small switches over 4 event types don't justify machinery; they only matter if narration-event count grows — the no-op default already makes additions non-breaking.
- **Generalizing `sqlExec` / store interfaces into a repository framework.** The store-interface-per-handler pattern is verbose but greppable and fake-able; the width problem was `ContentHandler` holding seven of them, not the pattern itself.

---

## 5. Closing judgment

The prior doc's registry/data-driven prescriptions all stand. What it missed — and what this pass found by tracing features instead of variants — is that Praetor's feature-velocity ceiling is a *vertical* pipeline, not horizontal seams: **six hand-rolled ways to start a job, an inline manifest build over `SELECT *`, and a three-party manifest contract with no pinned shape.** The evidence is not theoretical: verbosity, forks, and scheduled extra-vars are all shipped UI features that the pipeline silently discards today. B1+B2+B4 (roughly two weeks of guarded, mechanical work, api+scheduler images only) turn the platform's single most-trafficked feature axis into a one-package change and fix three user-visible bugs on the way through.
