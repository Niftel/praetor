import React from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { AppRoutes } from './App';

vi.mock('./components/Shell', async () => {
  const React = await import('react');
  const { Outlet } = await import('react-router-dom');
  return { default: () => React.createElement(Outlet) };
});

vi.mock('./pages/LoginPage', () => ({
  default: () => <div>Login screen</div>,
}));

vi.mock('./pages/DashboardPage', () => ({
  default: () => <div>Dashboard screen</div>,
}));

vi.mock('./pages/JobsPage', async () => {
  const { useNavigate } = await import('react-router-dom');
  return {
    default: () => {
      const navigate = useNavigate();
      return <button onClick={() => navigate(1)}>Forward to settings</button>;
    },
  };
});

vi.mock('./pages/JobDetailPage', async () => {
  const { useParams } = await import('react-router-dom');
  return {
    default: () => {
      const { jobId } = useParams();
      return <div>Job detail {jobId}</div>;
    },
  };
});

vi.mock('./pages/WorkflowBuilderPage', async () => {
  const { useParams } = await import('react-router-dom');
  return {
    default: () => {
      const { orgId, workflowId } = useParams();
      return <div>Workflow {workflowId} in organization {orgId}</div>;
    },
  };
});

vi.mock('./pages/SettingsPage', async () => {
  const { useNavigate, useParams } = await import('react-router-dom');
  return {
    default: () => {
      const navigate = useNavigate();
      const { section } = useParams();
      return (
        <>
          <div>Settings section {section}</div>
          <button onClick={() => navigate(-1)}>Back to jobs</button>
        </>
      );
    },
  };
});

vi.mock('./pages/AuthProvidersPage', () => ({
  default: () => <div>Authentication providers</div>,
}));

afterEach(cleanup);

const renderRoutes = (path: string, isAuthenticated = true) => render(
  <MemoryRouter initialEntries={[path]}>
    <AppRoutes
      isAuthenticated={isAuthenticated}
      onLogin={vi.fn()}
      onLogout={vi.fn()}
    />
  </MemoryRouter>,
);

describe('application routing', () => {
  it('redirects an unauthenticated deep link to login', async () => {
    renderRoutes('/service-principals', false);

    expect(await screen.findByText('Login screen')).toBeTruthy();
  });

  it('preserves parameters when opening authenticated deep links', async () => {
    renderRoutes('/workflows/org/5/builder/9');

    expect(await screen.findByText('Workflow 9 in organization 5')).toBeTruthy();
  });

  it('renders job detail links with their requested identifier', async () => {
    renderRoutes('/jobs/54');

    expect(await screen.findByText('Job detail 54')).toBeTruthy();
  });

  it('redirects the legacy auth-provider URL to its settings route', async () => {
    renderRoutes('/auth-providers');

    expect(await screen.findByText('Authentication providers')).toBeTruthy();
  });

  it('supports backward and forward history navigation', async () => {
    render(
      <MemoryRouter initialEntries={['/jobs', '/settings/notifications']} initialIndex={1}>
        <AppRoutes isAuthenticated onLogin={vi.fn()} onLogout={vi.fn()} />
      </MemoryRouter>,
    );

    fireEvent.click(await screen.findByRole('button', { name: 'Back to jobs' }));
    const forward = await screen.findByRole('button', { name: 'Forward to settings' });
    fireEvent.click(forward);

    expect(await screen.findByText('Settings section notifications')).toBeTruthy();
  });
});
