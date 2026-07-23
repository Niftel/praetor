import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import NotificationDeliveryHistory from './NotificationDeliveryHistory';
import { api } from '../services/api';

vi.mock('../services/api', () => ({
  api: { getNotificationDeliveries: vi.fn() },
}));

const delivery = {
  id: 42,
  organization_id: 5,
  team_id: 8,
  team_name: 'Platform',
  notification_template_id: 3,
  target_name: 'On-call webhook',
  target_type: 'webhook',
  resource_type: 'workflow_template' as const,
  resource_id: 12,
  event: 'approval',
  occurrence_type: 'workflow_node' as const,
  occurrence_id: 'node-7',
  subject_id: 17,
  subject_name: 'Release workflow',
  subject_kind: 'workflow approval',
  status: 'retrying' as const,
  attempt_count: 1,
  max_attempts: 5,
  next_attempt_at: '2026-07-23T10:00:05Z',
  first_attempt_at: '2026-07-23T10:00:00Z',
  last_attempt_at: '2026-07-23T10:00:00Z',
  failure_code: 'endpoint_unavailable',
  failure_reason: 'Destination temporarily unavailable',
  created_at: '2026-07-23T10:00:00Z',
  updated_at: '2026-07-23T10:00:00Z',
  attempts: [{
    attempt_number: 1,
    outcome: 'transient_failure' as const,
    failure_code: 'endpoint_unavailable',
    failure_reason: 'Destination temporarily unavailable',
    started_at: '2026-07-23T10:00:00Z',
    finished_at: '2026-07-23T10:00:01Z',
  }],
};

beforeEach(() => {
  vi.mocked(api.getNotificationDeliveries).mockResolvedValue({ results: [delivery], next_cursor: 42 });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('notification delivery history', () => {
  it('shows retry status, bounded diagnostics, and attempt details', async () => {
    render(<NotificationDeliveryHistory organizationId={5} />);

    expect(await screen.findByText('Release workflow')).toBeTruthy();
    expect(screen.getAllByText('Retrying').length).toBeGreaterThan(0);
    expect(screen.getByText('On-call webhook')).toBeTruthy();
    fireEvent.click(screen.getByText('Release workflow'));
    expect(screen.getAllByText('Destination temporarily unavailable')).toHaveLength(2);
    expect(screen.getByText('Attempt 1')).toBeTruthy();
    expect(screen.queryByText(/config/i)).toBeNull();
  });

  it('filters status and loads older pages with the returned cursor', async () => {
    vi.mocked(api.getNotificationDeliveries)
      .mockResolvedValueOnce({ results: [delivery], next_cursor: 42 })
      .mockResolvedValueOnce({ results: [], next_cursor: undefined })
      .mockResolvedValueOnce({ results: [delivery], next_cursor: 42 })
      .mockResolvedValueOnce({ results: [{ ...delivery, id: 21, status: 'delivered' }], next_cursor: undefined });
    render(<NotificationDeliveryHistory organizationId={5} />);
    await screen.findByText('Release workflow');

    fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'failed' } });
    await waitFor(() => expect(api.getNotificationDeliveries).toHaveBeenCalledWith(5, {
      status: 'failed', cursor: undefined, limit: 25,
    }));

    fireEvent.change(screen.getByLabelText('Status'), { target: { value: '' } });
    await screen.findByRole('button', { name: 'Load older deliveries' });
    fireEvent.click(screen.getByRole('button', { name: 'Load older deliveries' }));
    await waitFor(() => expect(api.getNotificationDeliveries).toHaveBeenCalledWith(5, {
      status: undefined, cursor: 42, limit: 25,
    }));
  });

  it('renders empty and failure states with a retry action', async () => {
    vi.mocked(api.getNotificationDeliveries).mockResolvedValueOnce({ results: [] });
    const { rerender } = render(<NotificationDeliveryHistory organizationId={5} />);
    expect(await screen.findByText('No delivery history')).toBeTruthy();

    vi.mocked(api.getNotificationDeliveries).mockRejectedValueOnce(new Error('history unavailable'));
    rerender(<NotificationDeliveryHistory organizationId={6} />);
    const error = await screen.findByText('Delivery history is unavailable');
    expect(error).toBeTruthy();
    fireEvent.click(within(error.parentElement!).getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(api.getNotificationDeliveries).toHaveBeenCalledWith(6, {
      status: undefined, cursor: undefined, limit: 25,
    }));
  });
});
