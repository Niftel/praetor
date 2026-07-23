import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import NotificationSettings from './NotificationSettings';
import { api } from '../services/api';

vi.mock('../services/api', () => ({
  unwrap: (value: any) => Array.isArray(value) ? value : value?.items ?? [],
  api: {
    getOrganizations: vi.fn(), getNotificationTypes: vi.fn(), getNotificationTemplates: vi.fn(),
    getCapabilities: vi.fn(), createNotificationTemplate: vi.fn(), deleteNotificationTemplate: vi.fn(),
    testNotificationTemplate: vi.fn(), getNotificationDeliveries: vi.fn(),
  },
}));

const organizations = [{ id: 5, name: 'Platform', created_at: '2026-07-22T00:00:00Z' }];
const types = [{ type: 'webhook', fields: [{ id: 'url', label: 'Target URL', type: 'text', secret: true }] }];

beforeEach(() => {
  vi.mocked(api.getOrganizations).mockResolvedValue(organizations);
  vi.mocked(api.getNotificationTypes).mockResolvedValue(types);
  vi.mocked(api.getCapabilities).mockResolvedValue({ view: true, manage: true, use: false, execute: false, update: false, approve: false });
  vi.mocked(api.getNotificationTemplates).mockResolvedValue([]);
  vi.mocked(api.getNotificationDeliveries).mockResolvedValue({ results: [] });
  vi.mocked(api.createNotificationTemplate).mockResolvedValue({ id: 8 });
  vi.mocked(api.testNotificationTemplate).mockResolvedValue({ status: 'delivered', notification_template_id: 8, tested_at: '2026-07-22T00:00:00Z' });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('notification settings', () => {
  it('creates a target through a write-only backend-driven form', async () => {
    render(<NotificationSettings />);
    fireEvent.click(await screen.findByRole('button', { name: 'Create target' }));

    fireEvent.change(screen.getByLabelText(/Name/), { target: { value: 'Platform alerts' } });
    const destination = screen.getByLabelText(/Target URL/) as HTMLInputElement;
    expect(destination.type).toBe('password');
    expect(screen.getByText(/Praetor will not display this value again/)).toBeTruthy();
    fireEvent.change(destination, { target: { value: 'https://hooks.example.test/secret' } });
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create target' }));

    await waitFor(() => expect(api.createNotificationTemplate).toHaveBeenCalledWith({
      organization_id: 5,
      name: 'Platform alerts',
      notification_type: 'webhook',
      config: { url: 'https://hooks.example.test/secret' },
    }));
  });

  it('tests an existing target without requesting its secret configuration', async () => {
    vi.mocked(api.getNotificationTemplates).mockResolvedValue([{ id: 8, organization_id: 5, name: 'On-call webhook', notification_type: 'webhook' }]);
    render(<NotificationSettings />);

    expect(await screen.findByText(/secrets hidden/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Send test' }));
    await waitFor(() => expect(api.testNotificationTemplate).toHaveBeenCalledWith(8));
    expect(api.getNotificationTemplates).toHaveBeenCalledWith(5);
  });

  it('keeps target mutation controls hidden from read-only users', async () => {
    vi.mocked(api.getCapabilities).mockResolvedValue({ view: true, manage: false, use: false, execute: false, update: false, approve: false });
    vi.mocked(api.getNotificationTemplates).mockResolvedValue([{ id: 8, organization_id: 5, name: 'On-call webhook', notification_type: 'webhook' }]);
    render(<NotificationSettings />);

    await screen.findByText('On-call webhook');
    expect(screen.queryByRole('button', { name: 'Create target' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Send test' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Delete/ })).toBeNull();
  });
});
