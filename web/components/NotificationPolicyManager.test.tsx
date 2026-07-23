import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import NotificationPolicyManager, { NotificationPolicyEvent } from './NotificationPolicyManager';
import { api } from '../services/api';

vi.mock('../services/api', () => ({
  unwrap: (value: any) => Array.isArray(value) ? value : value?.items ?? [],
  api: {
    getNotificationTemplates: vi.fn(), getNotificationPolicies: vi.fn(),
    getOrganizationTeams: vi.fn(), createNotificationPolicy: vi.fn(),
    deleteNotificationPolicy: vi.fn(),
  },
}));

const workflowEvents: NotificationPolicyEvent[] = [
  { id: 'success', label: 'Successful', description: 'Terminal success.' },
  { id: 'approval', label: 'Approval requested', description: 'Assigned team only.', requiresTeam: true },
];

beforeEach(() => {
  vi.mocked(api.getNotificationTemplates).mockResolvedValue([{ id: 8, name: 'On-call webhook', notification_type: 'webhook' }]);
  vi.mocked(api.getNotificationPolicies).mockResolvedValue([]);
  vi.mocked(api.getOrganizationTeams).mockResolvedValue([{ id: 4, organization_id: 5, name: 'Platform', created_at: '' }]);
  vi.mocked(api.createNotificationPolicy).mockResolvedValue({ id: 12 });
  vi.mocked(api.deleteNotificationPolicy).mockResolvedValue(new Response(null, { status: 204 }));
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('notification policy manager', () => {
  it('requires an explicit team for approval routes', async () => {
    render(<MemoryRouter><NotificationPolicyManager organizationId={5} resourceType="workflow_template" resourceId={9} events={workflowEvents} canManage /></MemoryRouter>);

    await screen.findByText('No notification routes attached.');
    fireEvent.change(screen.getByLabelText('Event'), { target: { value: 'approval' } });
    const attach = screen.getByRole('button', { name: 'Attach' }) as HTMLButtonElement;
    expect(attach.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText('Approval team'), { target: { value: '4' } });
    expect(attach.disabled).toBe(false);
    fireEvent.click(attach);

    await waitFor(() => expect(api.createNotificationPolicy).toHaveBeenCalledWith({
      notification_template_id: 8,
      resource_type: 'workflow_template',
      resource_id: 9,
      event: 'approval',
      team_id: 4,
    }));
  });

  it('shows attached routes without mutation controls to read-only users', async () => {
    vi.mocked(api.getNotificationPolicies).mockResolvedValue([{
      id: 12, notification_template_id: 8, notification_name: 'On-call webhook',
      notification_type: 'webhook', event: 'approval', team_id: 4, team_name: 'Platform',
    }]);
    render(<MemoryRouter><NotificationPolicyManager organizationId={5} resourceType="workflow_template" resourceId={9} events={workflowEvents} canManage={false} /></MemoryRouter>);

    expect(await screen.findByText('On-call webhook')).toBeTruthy();
    expect(screen.getByText('Platform')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Attach' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Detach/ })).toBeNull();
  });
});
