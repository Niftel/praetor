import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { getJobDiagnostics, streamJobDiagnostics } = vi.hoisted(() => ({
  getJobDiagnostics: vi.fn(),
  streamJobDiagnostics: vi.fn(),
}));

vi.mock('../services/api', () => ({
  api: { getJobDiagnostics },
  streamJobDiagnostics,
}));

import { useRunDiagnostics } from './useRunDiagnostics';

const summary = { unified_job_id: 4, state: 'running', current_phase: 'execute', attempt: 1, last_event_seq: 3, subsequent_job_ids: [] };
const event = (seq: number) => ({ seq, event_type: seq === 3 ? 'JOB_COMPLETED' : 'HOST_OK', host_id: seq < 3 ? seq : undefined, outcome: seq < 3 ? 'ok' : undefined, created_at: '2026-07-20T10:00:00Z' });

function Harness() {
  const diagnostics = useRunDiagnostics('run-1');
  return <div data-testid="result">{diagnostics.events.map(item => item.seq).join(',')}|{diagnostics.connection}|{diagnostics.summary?.state}</div>;
}

describe('useRunDiagnostics', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
  });

  it('drains pages and reconciles a replayed live event without duplicates', async () => {
    getJobDiagnostics
      .mockResolvedValueOnce({ summary, events: [event(1)], next_cursor: 1 })
      .mockResolvedValueOnce({ summary, events: [event(2)] });
    streamJobDiagnostics.mockImplementation(async (_runId, cursor, callbacks) => {
      expect(cursor).toBe(2);
      callbacks.onEvent(event(2));
      callbacks.onEvent(event(3));
      callbacks.onTerminal('successful', 3);
    });

    render(<Harness />);

    await waitFor(() => expect(screen.getByTestId('result').textContent).toBe('1,2,3|closed|successful'));
    expect(getJobDiagnostics).toHaveBeenCalledTimes(2);
    expect(streamJobDiagnostics).toHaveBeenCalledOnce();
  });
});
