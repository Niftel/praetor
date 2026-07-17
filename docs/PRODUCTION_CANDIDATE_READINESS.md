# Production-candidate readiness report

Praetor's readiness report is generated from a small, typed evidence manifest.
It records exact revisions and the outcome of each required operator journey,
but deliberately does not embed raw logs, API responses, credentials, tokens,
LDAP settings, or infrastructure addresses.

Required journeys are `ldap-operator`, `secrets-service`, `delegated-api`, and
`execution-recovery`. A passing journey must include the SHA-256 digest of its
sanitized machine-readable evidence artifact. A journey may be marked
`not-applicable` only with an allowlisted `justification_code`:
`feature-not-enabled`, `platform-not-applicable`, or
`superseded-by-scoped-test`. Free-form justification text is rejected so it
cannot carry sensitive data into the report.

Generate and validate a report with:

```sh
go run ./cmd/readiness-report \
  -input validation-evidence.json \
  -output production-candidate-readiness.json
```

The command exits `0` only for a `go` decision. It still writes a useful
`no-go` report and exits `2` when a required journey failed, a revision or
evidence digest is missing, or an unresolved release blocker exists. Invalid or
secret-bearing manifest fields are rejected with exit `1`.

When all four live journeys have written their sanitized evidence envelopes,
aggregate them without copying their contents into the report:

```sh
PRAETOR_REVISION="$(git rev-parse HEAD)" \
PRAETOR_SECRETS_REVISION="$(git -C ../praetor-secrets rev-parse HEAD)" \
PRAETOR_READINESS_EVIDENCE_DIR=build/readiness-evidence \
./scripts/generate-readiness-report.sh
```

The aggregator rejects missing/invalid envelopes and sensitive field names,
then records only each journey result and the SHA-256 digest of its sanitized
artifact.

## Evidence manifest

```json
{
  "schema_version": 1,
  "generated_at": "2026-07-17T12:00:00Z",
  "revisions": {
    "praetor": "0123456789abcdef0123456789abcdef01234567",
    "secrets_service": "0123456789abcdef0123456789abcdef01234567",
    "fixture": "0123456789abcdef0123456789abcdef01234567"
  },
  "journeys": [
    {
      "name": "ldap-operator",
      "result": "pass",
      "evidence_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }
  ],
  "findings": [
    {
      "id": "SEC-1",
      "category": "security",
      "classification": "release-blocking",
      "status": "resolved",
      "issue": "https://github.com/Niftel/praetor/issues/134"
    }
  ]
}
```

Findings are classified as `release-blocking`, `non-blocking`, or
`demand-gated`. Open security findings must remain release-blocking unless an
explicit review is recorded with `security_exception_reviewed: true`. Every
finding must link to a scoped issue in `Niftel/praetor`.
