-- This migration has been superseded by 000011_awx_style_rbac.up.sql for RBAC tables
-- The AWX-style RBAC migration creates a complete role hierarchy system

-- team_members table is handled by 000011_awx_style_rbac.up.sql
-- role_bindings -> replaced by role_members and team_roles tables
-- Old seeded roles -> replaced by system singleton roles and implicit object roles

-- Keep team_members table creation for compatibility
CREATE TABLE IF NOT EXISTS team_members (
    id BIGSERIAL PRIMARY KEY,
    team_id BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (team_id, user_id)
);

-- Seed Default Admin User (Password: "password")
-- bcrypt hash of "password": $2a$10$H2LfVov8ODCuR9Csh.E43uZhW89E0sRU4mlEo0BQVZARGk/lKLFre
INSERT INTO users (username, password_hash, email, is_superuser, is_active)
VALUES ('admin', '$2a$10$H2LfVov8ODCuR9Csh.E43uZhW89E0sRU4mlEo0BQVZARGk/lKLFre', 'admin@example.com', TRUE, TRUE)
ON CONFLICT (username) DO NOTHING;
