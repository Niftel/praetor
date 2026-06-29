-- Galaxy/Automation Hub credentials attached to an organization (ordered),
-- AWX-style. When a job in the org runs, the scheduler resolves these into the
-- job manifest so the host-runner installs requirements from them.
CREATE TABLE IF NOT EXISTS organization_galaxy_credentials (
    id              BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    credential_id   BIGINT NOT NULL REFERENCES credentials(id) ON DELETE CASCADE,
    position        INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id, credential_id)
);

CREATE INDEX IF NOT EXISTS idx_org_galaxy_creds_org
    ON organization_galaxy_credentials (organization_id, position);
