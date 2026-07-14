# Chaos Testing

Praetor's chaos suite verifies the central execution guarantee: control-plane
database availability does not determine whether an active execution can finish,
and recovered projections converge to executor truth.

## Run the suite

Docker and Go are required. From the repository root:

```sh
make chaos-test
```

The harness is destructive only to containers named `praetor-chaos-db` and
`praetor-chaos-nats`. It removes any containers with those names, creates fresh
PostgreSQL 15 and NATS 2.10 instances on ports 55432 and 54222, runs the real
database migrator, and removes both containers on exit. Do not reuse those names
for persistent local services.

The tests deliberately run with `GOWORK=off`. This proves the root repository
works with its released poly-repo dependencies instead of silently using sibling
checkout replacements.

## PostgreSQL outage during active execution

`TestDBOutageDuringActiveExecution` starts a run and consumes events through the
released consumer's database writer. After the first 15 events have committed,
the harness pauses PostgreSQL for five seconds. Execution continues by writing
task events, raw log chunks, log indexes, and the authoritative completion event
to durable JetStream storage. It also republishes the completion event to model
redelivery.

After PostgreSQL returns, the test requires all of the following:

- all 41 event sequence numbers are present exactly once;
- both the execution run and unified job converge to `successful`;
- `last_event_seq` reaches the terminal sequence;
- terminal-event redelivery creates only one database event row;
- both log indexes become queryable and their raw bytes remain unchanged.

Cleanup always unpauses PostgreSQL before deleting fixtures, including when an
assertion fails during the outage.

## NATS restart durability

`TestNATSRestartDurability` publishes events before a consumer exists, restarts
the NATS container, and verifies a new consumer receives every event from
JetStream file storage. The harness recreates its dependencies between scenarios
so durable consumer state cannot leak from one test to another.
