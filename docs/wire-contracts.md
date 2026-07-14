# Wire Contracts

Praetor services release independently, so payload compatibility is a product
correctness requirement. The canonical integration fixtures live under
`tests/contracts/v1`. They describe JSON on the wire, not the complete internal
state of any Go struct or database row.

The active fixture level is pinned by `wireContracts` in
`platform-compatibility.yaml`.

## Contract inventory

| Contract | Transport | Producer | Consumer | Durability / retry identity |
| --- | --- | --- | --- | --- |
| Execution request v1 | JetStream `job.requests` | Scheduler outbox relay | Executor, then host runner | `execution_run_id` is the NATS deduplication ID |
| Job event v1 | Host WAL and HTTP batch, then JetStream `job.events` | Host runner, executor, reconciler | Ingestion, consumer | `(execution_run_id, seq)` is the idempotency key |
| Log bytes | HTTP raw body | Host runner or reconciler | Ingestion object store | `(execution_run_id, seq)` identifies a chunk |
| Log chunk index v1 | JetStream `job.logs` | Ingestion | Consumer | `(execution_run_id, seq)` identifies the index row |
| Log cursor v1 | HTTP JSON response | Ingestion | Host runner | `bytes` plus maximum stored `seq` is authoritative |
| Heartbeat v1 | HTTP JSON response | Ingestion | Host runner | Repeated POST is idempotent; `cancel` is level-triggered |
| Runnable gate v1 | HTTP JSON response | Ingestion | Executor | `false` prevents a stale/terminal run from bootstrapping |
| Credential resolution v1 | Internal HTTP JSON | Ingestion | Executor | Run-scoped; response must never be persisted or logged |

Inventory rendering, fact-cache exchange, and inventory-sync payloads are known
HTTP contracts but are not yet frozen in the v1 fixture set. They should be added
before those formats evolve independently.

## Compatibility rules

1. Existing fields are never renamed, retyped, or repurposed within a contract
   version.
2. New fields are optional and readers ignore fields they do not understand.
3. A new reader accepts missing optional fields produced by an older writer.
4. Required-field or semantic changes create a new fixture version and require a
   migration window in which consumers accept both versions.
5. Acknowledgement means durable acceptance, not merely receipt. Ingestion sends
   success only after JetStream or object storage has accepted the payload.
6. Redelivery is normal. Consumers use the documented identity key rather than
   assuming exactly-once transport.
7. Terminal job events are monotonic. Late narration or duplicate events cannot
   move a terminal run back to a non-terminal state.
8. Unknown event types are retained for observability and must not mutate the run
   state unless explicitly registered as lifecycle events.

## Ingestion adoption

The shared `events` module already freezes the complete execution-request shape
with a golden test. The integration fixtures add job-event, log-index, and key
HTTP response boundaries.

The ingestion service now owns an explicit `JobEventRequest` HTTP DTO with
snake_case JSON tags and converts it to the shared event-stream type. It no
longer decodes host-runner payloads through `models.JobEvent`. Its service tests
pin the v1 batch shape, additive-field tolerance, authenticated run-ID override,
and preservation of host/task observability fields.

This adoption was released as `praetordev/ingestion` v0.1.1 and is pinned by the
current development compatibility manifest.

## Adoption requirements

Each repository should copy or fetch the fixture version it consumes and test
its real boundary:

- Scheduler marshals an execution request matching v1.
- Executor and host runner decode execution request v1 plus unknown fields.
- Ingestion decodes job-event batch v1 into an explicit wire DTO. Released in
  ingestion v0.1.1.
- Consumer decodes job-event and log-chunk v1 fixtures.
- Reconciler posts event batches and log chunks matching the ingestion contract.

Until those tests land in the owning repositories, the integration suite guards
the released shared event types and canonical shapes but cannot by itself prove
every service boundary.
