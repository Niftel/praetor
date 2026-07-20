import { DiagnosticEvent } from '../services/api';

export type Outcome = 'ok' | 'changed' | 'failed' | 'unreachable' | 'skipped' | 'running' | 'unknown';
export interface TaskRow { key: string; play: string; name: string; outcome: Outcome; hosts: Set<number>; duration: number; lastSeq: number; }
export interface HostRow { id: number; outcome: Outcome; tasks: Set<string>; failures: number; lastSeq: number; }

const OUTCOME_RANK: Record<Outcome, number> = { unknown: 0, skipped: 1, ok: 2, changed: 3, running: 4, failed: 5, unreachable: 6 };
const worse = (current: Outcome, next: Outcome) => OUTCOME_RANK[next] > OUTCOME_RANK[current] ? next : current;

const eventOutcome = (event: DiagnosticEvent): Outcome => {
  const value = event.outcome?.toLowerCase();
  if (value && value in OUTCOME_RANK) return value as Outcome;
  if (event.event_type === 'HOST_CHANGED') return 'changed';
  if (event.event_type === 'HOST_FAILED') return 'failed';
  if (event.event_type === 'HOST_UNREACHABLE') return 'unreachable';
  if (event.event_type === 'HOST_SKIPPED') return 'skipped';
  if (event.event_type === 'HOST_OK') return 'ok';
  return 'unknown';
};

export const failureGuidance = (code?: string) => {
  switch (code) {
    case 'task_failed': return 'Review the failed task and compare its raw output with the affected host IDs.';
    case 'host_unreachable': return 'Confirm inventory reachability, transport credentials, and the target network path.';
    case 'runner_bootstrap_failed': return 'Check runner bootstrap connectivity and the execution pack selected by the template.';
    case 'approval_rejected': return 'The approval was explicitly rejected. Review the assigned approval team and decision history.';
    case 'control_plane_interrupted': return 'Inspect the interruption window and confirm that the executor reconciled its final state.';
    default: return 'Review the failure event and raw output. Praetor has no more specific safe guidance for this code.';
  }
};

export const buildTaskRows = (events: DiagnosticEvent[]): TaskRow[] => {
  const rows = new Map<string, TaskRow>();
  for (const event of events) {
    if (!event.task_name) continue;
    const key = `${event.play_name || 'Play'}\u0000${event.task_name}`;
    const row = rows.get(key) || { key, play: event.play_name || 'Play', name: event.task_name, outcome: 'unknown' as Outcome, hosts: new Set<number>(), duration: 0, lastSeq: 0 };
    row.outcome = worse(row.outcome, eventOutcome(event));
    if (event.host_id != null) row.hosts.add(event.host_id);
    if (event.duration_ms) row.duration += event.duration_ms;
    row.lastSeq = Math.max(row.lastSeq, event.seq);
    rows.set(key, row);
  }
  return [...rows.values()].sort((a, b) => a.lastSeq - b.lastSeq);
};

export const buildHostRows = (events: DiagnosticEvent[]): HostRow[] => {
  const rows = new Map<number, HostRow>();
  for (const event of events) {
    if (event.host_id == null) continue;
    const row = rows.get(event.host_id) || { id: event.host_id, outcome: 'unknown' as Outcome, tasks: new Set<string>(), failures: 0, lastSeq: 0 };
    const next = eventOutcome(event);
    row.outcome = worse(row.outcome, next);
    if (event.task_name) row.tasks.add(event.task_name);
    if (next === 'failed' || next === 'unreachable') row.failures += 1;
    row.lastSeq = Math.max(row.lastSeq, event.seq);
    rows.set(event.host_id, row);
  }
  return [...rows.values()].sort((a, b) => OUTCOME_RANK[b.outcome] - OUTCOME_RANK[a.outcome] || a.id - b.id);
};
