# Authenticator mappings

Praetor evaluates external identity data through ordered, provider-neutral
authenticator maps. LDAP is the first provider, but rules consume only normalized
groups and user attributes; they never contain LDAP searches, filters, regular
expressions, or executable templates.

This replaces the legacy controller-specific `organization_map`, `team_map`, and
`user_flags_by_group` model for new deployments. Those fields remain readable
during migration. When both models target the same platform role, ordered
`authenticator_maps` are applied last.

## Security model

- Each predicate node has exactly one explicit operator: `all`, `any`, `not`,
  `group`, `attribute`, or `always`.
- Group and attribute comparisons are exact. Missing attributes do not match.
- Predicates are limited to 8 levels and 64 conditions; values are limited to
  1024 bytes.
- Invalid configuration prevents the API from starting.
- Login is allowed by default. An authoritative (`revoke: true`) non-matching
  `allow` rule denies login; a later matching allow rule can deliberately restore
  it.
- Rules run by ascending `order`. A later rule replaces an earlier decision only
  when both address the same target.
- Authentication maps target platform organizations, teams, and global roles.
  They cannot assign roles directly to inventories, credentials, templates, or
  workflows. Grant those resource roles to the mapped Praetor team through RBAC.

## Example

```yaml
authenticator_maps:
  - name: deny login by default
    order: 0
    revoke: true
    when:
      always: false
    map:
      type: allow

  - name: allow active automation operators
    order: 10
    when:
      all:
        - group: cn=automation-operators,ou=groups,dc=example,dc=com
        - not:
            group: cn=suspended-users,ou=groups,dc=example,dc=com
    map:
      type: allow

  - name: map production operators to the platform team
    order: 20
    revoke: true
    when:
      any:
        - all:
            - group: cn=automation-operators,ou=groups,dc=example,dc=com
            - group: cn=production,ou=groups,dc=example,dc=com
        - attribute:
            name: department
            equals: platform
    map:
      type: team
      organization: Engineering
      team: Production Operators
      role: Team Member
```

Supported targets are:

| Type | Required target | Supported role |
|---|---|---|
| `allow` | none | none |
| `organization` | `organization` | Organization Admin, Member, or Auditor |
| `team` | `organization`, `team` | Team Admin or Team Member |
| `role` | none | System Auditor |
| `is_superuser` | none | none |

For LDAP, group values are normalized distinguished names. Attribute names come
from built-in identity fields (`username`, `email`, `first_name`, `last_name`) or
the configured `users.attributes.custom` map.

