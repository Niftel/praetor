import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import TemplatesPage from './TemplatesPage';

const mocks = vi.hoisted(() => ({
  bulkLaunchJobs: vi.fn(),
}));

vi.mock('../services/api', () => ({
  newIdempotencyKey: () => 'ui-bulk-launch-test',
  unwrap: (value: any) => Array.isArray(value) ? value : value?.items ?? [],
  api: {
    getTemplates: vi.fn().mockResolvedValue([
      { id: 11, organization_id: 5, name: 'Deploy web', playbook: 'web.yml', unified_job_template_id: 101 },
      { id: 12, organization_id: 5, name: 'Deploy db', playbook: 'db.yml', unified_job_template_id: 102 },
    ]),
    getWorkflows: vi.fn().mockResolvedValue([]),
    getJobs: vi.fn().mockResolvedValue([]),
    getWorkflowJobs: vi.fn().mockResolvedValue([]),
    getProjects: vi.fn().mockResolvedValue([]),
    getInventories: vi.fn().mockResolvedValue([]),
    getCredentials: vi.fn().mockResolvedValue([]),
    getExecutionPacks: vi.fn().mockResolvedValue([]),
    getOrganizations: vi.fn().mockResolvedValue([{ id: 5, name: 'Engineering' }]),
    bulkLaunchJobs: mocks.bulkLaunchJobs,
  },
}));

afterEach(() => {
  cleanup();
  mocks.bulkLaunchJobs.mockReset();
});

describe('fleet-scale browser journey', () => {
  it('selects templates, reports mixed results, and retries only the failed item', async () => {
    mocks.bulkLaunchJobs
      .mockResolvedValueOnce({
        idempotency_key: 'launch-first',
        complete: true,
        results: [
          { index: 0, identifier: 'Deploy web', status: 'accepted', http_status: 201, job_id: 91 },
          { index: 1, identifier: 'Deploy db', status: 'rejected', http_status: 403, error: 'Launch not permitted' },
        ],
      })
      .mockResolvedValueOnce({
        idempotency_key: 'launch-retry',
        complete: true,
        results: [
          { index: 0, identifier: 'Deploy db', status: 'accepted', http_status: 201, job_id: 92 },
        ],
      });

    render(
      <MemoryRouter initialEntries={['/templates/org/5']}>
        <Routes>
          <Route path="/templates/org/:orgId" element={<TemplatesPage />} />
        </Routes>
      </MemoryRouter>,
    );

    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select all visible job templates' }));
    fireEvent.click(screen.getByRole('button', { name: 'Launch selected' }));

    expect(await screen.findByText('1 succeeded · 1 failed')).toBeTruthy();
    expect(mocks.bulkLaunchJobs).toHaveBeenNthCalledWith(1, [
      expect.objectContaining({ identifier: 'Deploy web', unified_job_template_id: 101 }),
      expect.objectContaining({ identifier: 'Deploy db', unified_job_template_id: 102 }),
    ], 'ui-bulk-launch-test');

    fireEvent.click(screen.getByRole('button', { name: 'Retry failed' }));
    await waitFor(() => expect(mocks.bulkLaunchJobs).toHaveBeenCalledTimes(2));
    expect(mocks.bulkLaunchJobs).toHaveBeenNthCalledWith(2, [
      expect.objectContaining({ identifier: 'Deploy db', unified_job_template_id: 102 }),
    ], 'ui-bulk-launch-test');
    expect(await screen.findByText('1 succeeded · 0 failed')).toBeTruthy();
  });
});
