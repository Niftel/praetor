-- Recreate the automation identity table (keypair is not restored; the migrator
-- would re-seed one on next run if this subsystem were reintroduced).
CREATE TABLE IF NOT EXISTS automation_identity (
    id          INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    public_key  TEXT NOT NULL,
    private_key TEXT NOT NULL, -- encrypted with the app secret
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
