import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';

vi.mock('../hooks/useRunDiagnostics', () => ({
  useRunDiagnostics: () => ({
    summary: { unified_job_id: 54, state: 'failed', current_phase: 'execute', attempt: 2, failure_code: 'task_failed', last_event_seq: 3, source_job_id: 51, subsequent_job_ids: [57] },
    events: [
      { seq: 1, event_type: 'JOB_STARTED', created_at: '2026-07-20T10:00:00Z' },
      { seq: 2, event_type: 'HOST_FAILED', host_id: 8, play_name: 'Deploy', task_name: 'Install package', outcome: 'failed', failure_code: 'task_failed', created_at: '2026-07-20T10:00:01Z' },
      { seq: 3, event_type: 'JOB_FAILED', failure_code: 'task_failed', created_at: '2026-07-20T10:00:02Z' },
    ],
    loading: false, error: null, connection: 'closed',
  }),
}));
vi.mock('../lib/useCapabilities', () => ({ useCapabilities: () => ({ capabilities: { execute: false }, loading: false }) }));
vi.mock('../services/api', () => ({
  unwrap: (value: unknown) => value,
  api: { getJobs: vi.fn().mockResolvedValue([]), getTemplates: vi.fn().mockResolvedValue([]), getJobLogsSince: vi.fn() },
}));

import JobDetailPage from './JobDetailPage';
import { buildHostRows, buildTaskRows, failureGuidance } from '../lib/executionDiagnostics';

describe('execution diagnostics job workspace', () => {
  it('provides semantic keyboard tabs and hides unauthorized execution controls', () => {
    render(<MemoryRouter initialEntries={[{ pathname: '/jobs/54', state: { job: { id: 54, name: 'Deploy app', status: 'failed', current_run_id: 'run-54', unified_job_template_id: 3 } } }]}><Routes><Route path="/jobs/:jobId" element={<JobDetailPage />} /></Routes></MemoryRouter>);

    const overview = screen.getByRole('tab', { name: 'Overview' });
    expect(overview.getAttribute('aria-selected')).toBe('true');
    expect(screen.queryByRole('button', { name: /^relaunch$/i })).toBeNull();
    expect(screen.getByText('Relaunch lineage')).toBeTruthy();
    expect(screen.getByText('Source job #51')).toBeTruthy();

    overview.focus();
    fireEvent.keyDown(overview.parentElement!, { key: 'ArrowRight' });
    expect(screen.getByRole('tab', { name: /Tasks/ }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByText('Install package')).toBeTruthy();
  });

  it('aggregates a large structured run without exposing raw payload fields', () => {
    const events = Array.from({ length: 10_000 }, (_, index) => ({
      seq: index + 1, event_type: index % 7 ? 'HOST_OK' : 'HOST_FAILED', host_id: (index % 500) + 1,
      play_name: 'Fleet update', task_name: `Task ${index % 200}`, outcome: index % 7 ? 'ok' : 'failed',
      created_at: '2026-07-20T10:00:00Z',
    }));
    expect(buildTaskRows(events)).toHaveLength(200);
    expect(buildHostRows(events)).toHaveLength(500);
    expect(JSON.stringify(buildHostRows(events))).not.toContain('event_data');
    expect(failureGuidance('unknown_code')).toContain('no more specific safe guidance');
  });
});
