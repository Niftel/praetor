import React from 'react';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import InventorySourceHistoryList from './InventorySourceHistory';
import { api } from '../services/api';

vi.mock('../services/api', () => ({
  api: { getInventorySourceHistory: vi.fn() },
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('InventorySourceHistoryList', () => {
  it('shows deterministic deltas, diagnostics, policy, and linked job', async () => {
    vi.mocked(api.getInventorySourceHistory).mockResolvedValue({
      total: 1,
      results: [{
        id: 3, correlation_id: 'corr', inventory_id: 2, inventory_source_id: 7,
        unified_job_id: 44, reconciliation_policy: 'disable_missing',
        phase: 'reconciliation', status: 'failed', hosts_added: 2,
        hosts_updated: 1, hosts_disabled: 3, hosts_unchanged: 4,
        groups_added: 1, groups_updated: 0, groups_unchanged: 0,
        diagnostic_code: 'reconciliation_failed', diagnostic_message: 'Group update failed safely.',
        diagnostic_details: {}, created_at: '2026-07-21T12:00:00Z',
      }],
    });

    render(<MemoryRouter><InventorySourceHistoryList inventoryId={2} sourceId={7} /></MemoryRouter>);

    await waitFor(() => expect(screen.getByText('failed')).toBeTruthy());
    expect(screen.getByText(/\+2 added · 1 updated · 3 disabled · 4 unchanged/)).toBeTruthy();
    expect(screen.getByText('disable missing')).toBeTruthy();
    expect(screen.getByText('Group update failed safely.')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'job 44' }).getAttribute('href')).toBe('/jobs/44');
    expect(api.getInventorySourceHistory).toHaveBeenCalledWith(2, 7, { limit: 10 });
  });

  it('renders a bounded empty state', async () => {
    vi.mocked(api.getInventorySourceHistory).mockResolvedValue({ total: 0, results: [] });
    render(<MemoryRouter><InventorySourceHistoryList inventoryId={2} sourceId={7} /></MemoryRouter>);
    await waitFor(() => expect(screen.getByText('No synchronization attempts yet.')).toBeTruthy());
  });
});
