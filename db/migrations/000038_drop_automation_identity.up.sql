-- Remove Praetor's shared automation SSH identity. The platform no longer ships
-- a shared key: hosts are reached via per-job Machine credentials (username +
-- SSH key + privilege escalation), and the operator owns the login user and its
-- authorized_keys on each host. See cmd/migrator (seedCredentialTypes: Machine).
DROP TABLE IF EXISTS automation_identity;
