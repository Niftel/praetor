-- Sensible RBAC role assignments for the "Praetor Industries" demo company.
--
-- Mirrors the org chart onto Praetor's roles. Idempotent and keyed by
-- name/username (not generated IDs), so it is safe to re-run after an LDAP sync
-- or on a fresh environment. Apply with:
--   docker compose exec -T db psql -U postgres -d praetor < deployments/seed/company-role-assignments.sql

-- System Administrators: the platform admin and the CTO.
UPDATE users SET is_superuser = true WHERE username IN ('admin', 'ctran');
INSERT INTO role_members (role_id, user_id)
SELECT r.id, u.id
FROM roles r CROSS JOIN users u
WHERE r.singleton_name = 'system_administrator' AND u.username IN ('admin', 'ctran')
ON CONFLICT (role_id, user_id) DO NOTHING;

-- System Auditors (read-only across everything): CISO and Compliance.
UPDATE users SET is_system_auditor = true WHERE username IN ('scho', 'ocole');
INSERT INTO role_members (role_id, user_id)
SELECT r.id, u.id
FROM roles r CROSS JOIN users u
WHERE r.singleton_name = 'system_auditor' AND u.username IN ('scho', 'ocole')
ON CONFLICT (role_id, user_id) DO NOTHING;

-- Department heads administer their own department (organization admin_role).
INSERT INTO role_members (role_id, user_id)
SELECT r.id, u.id
FROM (VALUES
    ('Engineering',      'vengels'),
    ('Operations',       'opark'),
    ('Security',         'scho'),
    ('Data',             'dreyes'),
    ('QualityAssurance', 'qalvarez')
) AS m(org_name, uid)
JOIN organizations o ON o.name = m.org_name AND o.ldap_dn IS NOT NULL
JOIN roles r ON r.content_type = 'organization' AND r.object_id = o.id AND r.role_field = 'admin_role'
JOIN users u ON u.username = m.uid
ON CONFLICT (role_id, user_id) DO NOTHING;

-- Everyone gets member access to their own department: add each team's members
-- to that team's organization member_role.
INSERT INTO role_members (role_id, user_id)
SELECT org_role.id, tm.user_id
FROM teams t
JOIN organizations o ON o.id = t.organization_id AND o.ldap_dn IS NOT NULL
JOIN roles team_role ON team_role.content_type = 'team' AND team_role.object_id = t.id AND team_role.role_field = 'member_role'
JOIN role_members tm ON tm.role_id = team_role.id
JOIN roles org_role ON org_role.content_type = 'organization' AND org_role.object_id = o.id AND org_role.role_field = 'member_role'
ON CONFLICT (role_id, user_id) DO NOTHING;
