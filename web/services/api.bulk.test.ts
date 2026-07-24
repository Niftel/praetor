import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from './api';

const jsonResponse = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status,
  headers: { 'Content-Type': 'application/json' },
});

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe('bulk API contracts', () => {
  it('sends bounded job launches with the caller idempotency key', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse({
      idempotency_key: 'launch-1',
      complete: true,
      results: [{ index: 0, identifier: 'Deploy', status: 'launched', http_status: 201, job_id: 42 }],
    }, 207));

    const response = await api.bulkLaunchJobs([
      { identifier: 'Deploy', unified_job_template_id: 7, name: 'Deploy' },
    ], 'launch-1');

    expect(response.results[0]).toMatchObject({ status: 'launched', job_id: 42 });
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/v1/bulk/jobs/launch');
    expect(new Headers(options?.headers).get('Idempotency-Key')).toBe('launch-1');
    expect(JSON.parse(String(options?.body))).toEqual({
      items: [{ identifier: 'Deploy', unified_job_template_id: 7, name: 'Deploy' }],
    });
  });

  it('keeps preview and confirmation as separate host deletion requests', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(jsonResponse({
        confirmation_token: 'opaque-preview-token',
        expires_at: '2026-07-24T12:30:00Z',
        results: [{
          index: 0,
          identifier: 'web-01',
          status: 'ready',
          http_status: 200,
          host_id: 9,
          blocking_relationships: [],
          affected_relationships: [{ code: 'group_membership', count: 1, effect: 'removed' }],
        }],
      }, 201))
      .mockResolvedValueOnce(jsonResponse({
        idempotency_key: 'delete-1',
        complete: true,
        results: [{ index: 0, identifier: 'web-01', status: 'deleted', http_status: 200, host_id: 9 }],
      }, 200));

    const preview = await api.previewBulkDeleteHosts([{ identifier: 'web-01', host_id: 9 }]);
    await api.bulkDeleteHosts(preview.confirmation_token, 'delete-1');

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/bulk/hosts/delete/preview', expect.objectContaining({ method: 'POST' }));
    const [, confirmationOptions] = fetchMock.mock.calls[1];
    expect(new Headers(confirmationOptions?.headers).get('Idempotency-Key')).toBe('delete-1');
    expect(JSON.parse(String(confirmationOptions?.body))).toEqual({ confirmation_token: 'opaque-preview-token' });
  });

  it('accepts multi-status host creation results without flattening failures', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonResponse({
      idempotency_key: 'create-1',
      complete: true,
      results: [
        { index: 0, identifier: 'web-01', status: 'created', http_status: 201, host_id: 10 },
        { index: 1, identifier: 'web-02', status: 'rejected', http_status: 403, code: 'not_found_or_forbidden', error: 'inventory not found or creation not permitted' },
      ],
    }, 207));

    const response = await api.bulkCreateHosts([
      { identifier: 'web-01', inventory_id: 2, name: 'web-01' },
      { identifier: 'web-02', inventory_id: 2, name: 'web-02' },
    ], 'create-1');

    expect(response.results.map(result => result.status)).toEqual(['created', 'rejected']);
    expect(response.results[1].error).toContain('not permitted');
  });
});
