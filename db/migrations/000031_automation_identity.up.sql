-- Praetor's automation SSH identity, managed in the database instead of a file
-- mount. A single row holds the keypair: the public key (distributed to hosts'
-- authorized_keys) and the private key, encrypted with the app secret. The
-- migrator seeds this once — importing an existing mounted key for continuity,
-- or generating a fresh keypair — and the API/scheduler read it from here.
CREATE TABLE IF NOT EXISTS automation_identity (
    id          INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    public_key  TEXT NOT NULL,
    private_key TEXT NOT NULL, -- encrypted with the app secret
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
