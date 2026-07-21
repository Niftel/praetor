import fs from 'node:fs';
import { performance } from 'node:perf_hooks';
import { buildHostRows, buildTaskRows } from '../lib/executionDiagnostics';
import type { DiagnosticEvent } from '../services/api';

const input = process.argv[2];
if (!input) throw new Error('usage: vite-node scripts/measure-diagnostics.ts <events.json>');
const events = JSON.parse(fs.readFileSync(input, 'utf8')) as DiagnosticEvent[];
if (!Array.isArray(events) || events.length < 100) throw new Error('at least 100 diagnostic events are required');

const samples: number[] = [];
let taskRows = 0;
let hostRows = 0;
for (let run = 0; run < 25; run += 1) {
  const started = performance.now();
  taskRows = buildTaskRows(events).length;
  hostRows = buildHostRows(events).length;
  samples.push(performance.now() - started);
}
samples.sort((a, b) => a - b);
const p95 = samples[Math.ceil(samples.length * 0.95) - 1];
process.stdout.write(JSON.stringify({
  render_p95_ms: Math.round(p95 * 1000) / 1000,
  samples: samples.length,
  event_count: events.length,
  task_rows: taskRows,
  host_rows: hostRows,
}));
