# LDAP redesign — OU-discovery sync → AAP/AWX login-time group mapping

> **Status: design, not yet implemented.** This is the agreed plan for replacing
> Praetor's LDAP integration wholesale. Source-grounded (Fable-5 consult over
> `pkg/auth/*`, `services/api/handlers/{auth,ldap}.go`, `services/api/store/`,
> `db/migrations/`, the demo LDAP config, and the Helm chart). Line references
> reflect the tree at writing and may drift. Code is the source of truth — this
> doc is not.

## Why

Praetor authenticates users the way AWX/Ansible Automation Platform does — or is
supposed to. The current implementation instead **mirrors directory structure**:
a `Syncer` discovers `organizationalUnit` entries as organizations and
`groupOfNames` as teams, copying them into Praetor tables. That model only fits a
directory shaped like Praetor's demo seed; **real corporate AD/LDAP directories do
not model AWX-style organizations as OUs.** It also has no role dimension and no
superuser mapping.

We are **replacing** it (not adding a second mode) with the AAP/AWX model:
LDAP **groups → roles**, declared by the operator and **evaluated at login**.

### Two facts about the current code that motivate the rewrite

1. **LDAP login does not work today.** `handlers/auth.go Login` only checks a local
   bcrypt hash (`ByUsernameWithHash` → `bcrypt.CompareHashAndPassword`). The sync
   inserts LDAP users with `password_hash = ''` (`sync.go:388`), and bcrypt against
   an empty hash always errors — so every LDAP-sourced user is un-loginable. The
   bind routine `client.go AuthenticateUser` exists but has **zero callers**.
2. **There is no interval sync.** `LDAPSyncConfig.Interval` is parsed and echoed by
   `GetConfig` but no ticker/goroutine runs the Syncer — sync fires only from the
   UI's "Sync Now" (`POST /api/v1/ldap/sync`). So there is no background job to
   unwire, only routes/handlers.

## Authentication model (the key decision)

Two independent account kinds, checked in this order:

1. **Local superuser — the break-glass account, outside all auth providers.**
   A user row with a **non-empty local `password_hash` and `ldap_dn IS NULL`**
   always authenticates **locally**, by bcrypt, regardless of LDAP config or LDAP
   availability. LDAP **never** attaches `ldap_dn` to such a row and never overtakes
   it. This is the AWX/AAP pattern: the seeded `admin`
   (`20240503000000_rbac_tables.up.sql:19`) persists alongside LDAP so a
   misconfigured or unreachable directory can never lock everyone out. The platform
   guarantees at least one such local superuser exists; its password is managed
   out-of-band (seed / API), never by a directory.

2. **LDAP-backed users.** Distinct rows with `ldap_dn` set and an empty local hash.
   They authenticate by **binding to the LDAP server** at login; a bind failure
   returns the same generic 401 — there is **no** fallback to the (empty) local hash.

This closes the old `WHERE username=$1 OR ldap_dn=$2` account-takeover shape in
`sync.go` and means a username that already exists as a local account can never be
shadowed by a same-named directory entry.

### New login flow (`handlers/auth.go Login`)

1. Decode; look up the local user row by username.
2. **If the row is a local account** (non-empty `password_hash`, `ldap_dn IS NULL`)
   → local bcrypt path, exactly as today. (Keeps `user_login_test.go` green.)
3. **Otherwise, if LDAP is configured** → LDAP path:
   a. Connect, service-bind, search the user by
      `users.search_filter && (username_attr = EscapeFilter(username))` across
      **all** `GetSearchBases()`; require exactly one entry.
   b. **Bind as that entry's DN with the supplied password** (authN). Re-bind as the
      service account.
   c. Resolve the user's **group DN set once** per `group_type` (§ Group resolution).
   d. In one DB transaction: upsert the user, apply `user_flags_by_group`,
      `organization_map`, `team_map` (§ Mapper).
4. Reject if `!user.IsActive`. Issue the JWT with the **freshly computed**
   `IsSuperuser` / `IsSystemAuditor`.

Do all LDAP I/O **before** opening the DB transaction (keep the tx short).

## RBAC mapping — no new tables

Praetor already has the full AWX role machinery (`000011_awx_style_rbac.up.sql`):
polymorphic `roles` (`content_type`,`object_id`,`role_field`), a `role_members`
grant table, org-insert/team-insert triggers that materialize
`admin_role`/`member_role`/`auditor_role`/`read_role`, delete-triggers that GC them
(`000029`), and `users.is_superuser` / `users.is_system_auditor` flags enforced in
the JWT middleware. Grant/revoke primitives exist (`pkg/rbac/access.go`).

| AAP concept | Praetor target |
|---|---|
| `organization_map.admins`  | org `admin_role` grant in `role_members` |
| `organization_map.users`   | org `member_role` |
| `organization_map.auditors`| org `auditor_role` |
| `team_map.users`           | team `member_role` |
| `user_flags_by_group.is_superuser`     | `users.is_superuser` |
| `user_flags_by_group.is_system_auditor`| `users.is_system_auditor` |

**No schema change for roles.** One migration (`000047_ldap_login_model`) only drops
dead artifacts (§ Migration).

## New config surface

Replaces `LDAPOrgConfig` / `LDAPTeamConfig` / `LDAPSyncConfig`. `server`, `users`,
and `users.attributes` (the `user_attr_map`) are unchanged.

```go
type LDAPConfig struct {
    Server          LDAPServerConfig            `yaml:"server"`              // unchanged
    Users           LDAPUserConfig              `yaml:"users"`               // unchanged
    GroupType       LDAPGroupTypeConfig         `yaml:"group_type"`
    UserFlags       LDAPUserFlagsConfig         `yaml:"user_flags_by_group"`
    OrganizationMap map[string]LDAPOrgMapEntry  `yaml:"organization_map"`    // key = Praetor org NAME
    TeamMap         map[string]LDAPTeamMapEntry `yaml:"team_map"`            // key = Praetor team NAME
}

type LDAPGroupTypeConfig struct {
    Type              string `yaml:"type"`                // member_dn | member_of | posix | nested
    SearchBase        string `yaml:"search_base"`         // groups base (member_dn/posix/nested)
    SearchFilter      string `yaml:"search_filter"`       // default (objectClass=groupOfNames)
    MemberAttribute   string `yaml:"member_attribute"`    // default "member" (posix: memberUid)
    MemberOfAttribute string `yaml:"member_of_attribute"` // default "memberOf"
    MaxDepth          int    `yaml:"max_depth"`           // nested only, default 5
}

type LDAPUserFlagsConfig struct {
    IsSuperuser     GroupDNList `yaml:"is_superuser"`
    IsSystemAuditor GroupDNList `yaml:"is_system_auditor"`
}

type LDAPOrgMapEntry struct {
    Admins         GroupMatch `yaml:"admins"`
    Users          GroupMatch `yaml:"users"`
    Auditors       GroupMatch `yaml:"auditors"`
    RemoveAdmins   bool       `yaml:"remove_admins"`
    RemoveUsers    bool       `yaml:"remove_users"`
    RemoveAuditors bool       `yaml:"remove_auditors"`
}

type LDAPTeamMapEntry struct {
    Organization string     `yaml:"organization"` // required; created if absent
    Users        GroupMatch `yaml:"users"`
    Remove       bool       `yaml:"remove"`
}

// GroupMatch mirrors django-auth-ldap's DN-string / list / bool trichotomy.
type GroupMatch struct {
    All *bool    // yaml `true`/`false`
    DNs []string // yaml string or list of strings
    // Matches(set): All==true → true; All==false → false; else any DN in set.
    // "configured" = All != nil || len(DNs) > 0  (needed for remove_* semantics)
}
```

`remove_*` semantics (django-auth-ldap parity): when the flag is true **and** the
match is configured **and** the user does not match → revoke at this login; when
false → grant-only, never revoke.

### Group resolution (new `LDAPClient` methods, per `group_type`)

- **member_of** — read `memberOf` off the user entry already fetched at authN (add
  it to requested attrs); zero extra queries. *(Not available on the demo directory
  — osixia has no memberOf overlay.)*
- **member_dn** — one search `(&<filter>(member=<userDN>))` under `search_base`.
  *(Correct setting for the demo's `groupOfNames`.)*
- **posix** — `(|(memberUid=<uid>)(gidNumber=<gid>))`.
- **nested** — AD fast path `(member:1.2.840.113556.1.4.1941:=<userDN>)`, else
  bounded iterative expansion to `max_depth`.

**Normalize every DN** (parse + canonical lower-case) before set-membership tests —
config DNs and directory DNs differ in case, and un-normalized matching silently
never fires.

## Mapper (`pkg/auth/mapper.go`, replaces `sync.go`)

`func Authenticate(ctx, cfg, db, username, password) (models.User, error)`

1. LDAP phase (no tx): bind-authN, fetch attrs incl. custom + `memberOf` if needed,
   compute `groups` set once.
2. Open tx. Upsert user (attrs from `users.attributes`; custom → `users.ldap_metadata`,
   the column from `000015` currently written by nothing); set `ldap_dn`,
   `ldap_synced_at=NOW()`, `password_hash=''`.
3. **Flags:** only **assign** `is_superuser` / `is_system_auditor` when that flag
   mapping is **configured** (unset ≠ false — otherwise every login strips
   manually-granted flags). Never touch non-LDAP users.
4. **organization_map:** per `(orgName, entry)` — select-or-insert org (the
   `trg_create_org_roles` trigger materializes roles in the same tx); for
   admins/users/auditors, `INSERT INTO role_members … ON CONFLICT DO NOTHING` on
   match, `DELETE` on no-match when `remove_*` and configured.
5. **team_map:** resolve org (create if absent), select team by
   **`(organization_id, name)`** (not name-only — avoid `sync.go:548`'s cross-org
   bug), create if absent, grant/revoke team `member_role` per match/`remove`.
6. Commit; return user for JWT issuance.

Idempotent: grants are `ON CONFLICT DO NOTHING`, revokes are plain deletes; re-login
converges. Handle first-login create races via the unique constraints (catch → re-select).

## What gets deleted

- `pkg/auth/sync.go` — whole file.
- `pkg/auth/ldap.go` — `LDAPOrgConfig`, `LDAPTeamConfig`, `LDAPOrgAttributes`,
  `LDAPTeamAttributes`, `LDAPNestedGroupConfig`, `LDAPSyncConfig`, `LDAPSyncResult`,
  `LDAPSyncItem`, the org/team `GetSearchBases` variants. Keep `LDAPServerConfig`,
  `LDAPUserConfig`, `LDAPUserAttributes`, `LDAPEntry`, `LDAPSearchScope`.
- `pkg/auth/client.go` — `SearchOrganizations`, `SearchTeams`. Repurpose
  `AuthenticateUser` (fix to use `GetSearchBases()`), keep `Connect`/`Bind`/`search`.
- `services/api/handlers/ldap.go` — `TriggerSync`, `TriggerSyncSpecific`,
  `GetSyncStatus`, `GetSyncDetails`, `SyncRequest`, the `LdapStore` iface. Keep
  `TestConnection` + `GetConfig` (rewrite to render the new shape).
- `services/api/store/ldap_store.go` — whole file.
- `services/api/router.go` — the three `/ldap/sync*` routes (keep `/config`,
  `/test-connection`).
- Web UI (`web/pages/AuthProvidersPage.tsx`, `web/services/api.ts`) — **user-facing
  removal**: "Sync Now" button, sync-history table, `SyncLogEntry`/`SyncItem`/
  `SyncDetails`, `triggerLdapSync`/`getLdapSyncStatus`/`getLdapSyncDetails`. The page
  keeps: configured-or-not, server + user-search summary, a read-only maps summary,
  and Test Connection.

## Migration `000047_ldap_login_model`

- **up:** `DROP TABLE IF EXISTS ldap_sync_items, ldap_sync_log;` `ALTER TABLE
  organizations DROP COLUMN IF EXISTS ldap_dn, ldap_synced_at;` same for `teams`
  (and the partial indexes from `000012`). **Keep** `users.ldap_dn`,
  `users.ldap_synced_at`, `users.ldap_metadata`.
- **down:** recreate the two tables + columns verbatim from `000012`–`000014`.
- Leave `000012`–`000015` files immutable.

## Demo + Helm reshape

- **`deployments/ldap/bootstrap.ldif`:** keep `ou=users` (their `userPassword`s now
  matter — login binds); keep the `groupOfNames` under `ou=teams` as the group
  source; the `ou=organizations` subtree becomes inert (fix the header comment that
  calls it "=> Praetor organizations"). Reuse `cn=admins` as the superuser group.
- **`deployments/ldap/ldap-config.yaml`:** drop `organizations`/`teams`/`sync`; add
  `group_type: {type: member_dn, search_base: ou=teams,…}`, `user_flags_by_group`,
  and `organization_map` / `team_map` binding the demo groups to demo orgs/teams.
- **Helm (`deployments/helm/praetor-v2`):** the render mechanism is unchanged —
  `ldap.config` is mounted verbatim; only the example shape, `values.schema.json`
  ldap block, and the README Authentication section need updating.
- **Existing demo DB rows:** the mapper keys orgs/teams by name, so reusing the same
  names reconciles onto existing rows (no data loss). Unmapped leftover orgs are
  **not** auto-deleted (AAP doesn't delete orgs either). Simplest for the demo:
  re-seed (`docker compose down -v`) in the quickstart.

## Risks / gotchas (codebase-specific)

1. **JWT staleness.** Flags are baked into the 24h JWT and trusted by middleware
   without a per-request DB re-check on the JWT path; login-time revocation lags to
   token expiry (same as AWX). Document it, or re-read flags in middleware if
   unacceptable.
2. **`user_flags_by_group` unset ≠ false** — only assign when configured, else every
   login strips manually-granted superuser.
3. **Break-glass integrity** — never attach `ldap_dn` to a row with a non-empty local
   hash; the local superuser must remain local-only.
4. **Empty-hash lockout is intentional** — LDAP users keep `password_hash=''`;
   ensure the password-reset path (`user_store.go`) refuses LDAP users (no shadow
   local password).
5. **Per-login LDAP latency** — bind + group query adds up to hundreds of ms; do it
   outside the tx. `client.go:39` mutates a package-global `ldap.DefaultTimeout` — a
   pre-existing smell to fix while here.
6. **Timing/enumeration** — floor the LDAP path (dummy bcrypt / constant floor) so
   "user not in LDAP" is indistinguishable from "bad password".
7. **DN normalization** (see Group resolution) and **team `(org_id, name)`
   uniqueness** (don't inherit the name-only lookup bug).
8. **Trigger dependence** — create orgs/teams via plain INSERTs in the tx, then look
   up the roles the triggers just made (same tx, they exist immediately).

## Implementation slices (verifiable order)

1. **Config + mapper + login (Go core).** New `ldap.go` types, `GroupMatch`
   unmarshal, group resolution on `client.go`, `mapper.go`, `Login` branch. Unit
   tests with a fake group-resolver (no live LDAP). `user_login_test.go` stays green.
2. **Migration + deletes.** `000047`, remove `sync.go`/`ldap_store.go`/sync
   handlers+routes; rewrite `config_test.go` and `GetConfig`.
3. **Demo + Helm.** Reshape `bootstrap.ldif` + `ldap-config.yaml`, Helm example +
   schema + README; live-verify against the demo directory (bind login → superuser
   via group → org/team role grants).
4. **UI.** Trim `AuthProvidersPage` to config + test-connection; drop the sync APIs.
