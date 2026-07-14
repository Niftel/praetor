# Execution State Machine

Praetor separates durable execution truth from control-plane liveness. The API,
scheduler, consumer, and reconciler may all observe or project state, but only a
terminal executor/host-runner outcome establishes successful, failed, or
canceled execution.

## Run states

| State | Meaning | Final? | Owner |
| --- | --- | --- | --- |
| `pending` | Durable run row exists but execution has not started | No | Scheduler |
| `running` | Executor or host runner emitted `JOB_STARTED` | No | Consumer projection |
| `reconciling` | Control-plane liveness is stale; executor truth is being recovered | No | Scheduler |
| `lost` | Positive evidence says the executor WAL is gone | Provisional | Reconciler/scheduler |
| `successful` | Executor emitted/recovered `JOB_COMPLETED` | Yes | Consumer/reconciler |
| `failed` | Executor emitted/recovered `JOB_FAILED` | Yes | Consumer/reconciler |
| `canceled` | Executor acknowledged cancellation with `JOB_CANCELED` | Yes | Consumer/reconciler |

`lost` is intentionally recoverable. A late WAL or terminal event may replace it
with the real executor outcome. Merely being unable to reach an executor does not
prove loss.

## Unified-job states

| State | Meaning | Final? |
| --- | --- | --- |
| `pending` | Accepted but not scheduled | No |
| `queued` | Run and durable launch request created | No |
| `running` | `JOB_STARTED` accepted | No |
| `error` | Control plane currently considers the run unrecoverable | Provisional |
| `successful` | `JOB_COMPLETED` accepted | Yes |
| `failed` | `JOB_FAILED` accepted, or execution never reached any executor | Yes |
| `canceled` | Cancellation outcome accepted | Yes |

## Lifecycle projections

| Event | Run state | Job state | Terminal |
| --- | --- | --- | --- |
| `JOB_STARTED` | `running` | `running` | No |
| `JOB_COMPLETED` | `successful` | `successful` | Yes |
| `JOB_FAILED` | `failed` | `failed` | Yes |
| `JOB_CANCELED` | `canceled` | `canceled` | Yes |

Task, checkpoint, runner-online, resume, and unknown events are retained for
observability but do not change lifecycle state.

## Projection rules

1. `(execution_run_id, seq)` is the event idempotency key. Exact redelivery has
   no second effect.
2. `last_event_seq` advances for every newly inserted event, including task and
   narration events.
3. A lifecycle event is evaluated while holding a lock on its run row.
4. Repeating the current state is not a transition and produces no lifecycle
   notification or transition metric.
5. `successful`, `failed`, and `canceled` are monotonic. Late starts and
   conflicting terminal events are retained but cannot replace the first
   accepted terminal outcome.
6. `reconciling`, `lost`, and job `error` are provisional. A real terminal event
   may replace them.
7. Lifecycle notifications and terminal-transition metrics fire only when the
   corresponding transition is accepted, not merely because an event arrived.
8. An unreachable control plane or executor never creates a successful or failed
   result by inference. Timeout logic either parks the run for reconciliation or,
   when there is positive evidence no executor ran, records failure explicitly.

## Conflicting terminal events

A run should emit exactly one terminal event. If two different terminal events
arrive, the first transactionally accepted terminal state remains authoritative.
The later event stays in `job_events` for diagnosis and advances the observed
sequence cursor, but has no state or notification side effect. Conflicting
terminal events should be surfaced operationally as protocol corruption in a
future alerting slice.

## Executable enforcement

The consumer owns the pure transition table and table-driven tests. PostgreSQL
helpers `run_is_terminal` and `job_is_terminal` provide the cross-service SQL
guard. Consumer database integration tests cover idempotent redelivery, late
starts, conflicting terminal events, and event-sequence advancement.
