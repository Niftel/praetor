CREATE TABLE service_principals (
    id                 BIGSERIAL PRIMARY KEY,
    organization_id    BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    enabled            BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at        TIMESTAMPTZ,
    UNIQUE (organization_id, name),
    CHECK ((enabled AND disabled_at IS NULL) OR (NOT enabled AND disabled_at IS NOT NULL))
);

CREATE INDEX idx_service_principals_organization
    ON service_principals (organization_id, id);

CREATE TABLE service_credentials (
    id                   BIGSERIAL PRIMARY KEY,
    service_principal_id BIGINT NOT NULL REFERENCES service_principals(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    token_hash           TEXT NOT NULL UNIQUE,
    expires_at           TIMESTAMPTZ NOT NULL,
    last_used_at         TIMESTAMPTZ,
    created_by_user_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at           TIMESTAMPTZ,
    CHECK (expires_at > created_at)
);

CREATE INDEX idx_service_credentials_principal
    ON service_credentials (service_principal_id, id);

CREATE INDEX idx_service_credentials_active_hash
    ON service_credentials (token_hash)
    WHERE revoked_at IS NULL;
