import React from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import RouteErrorBoundary from './RouteErrorBoundary';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const ThrowingRoute = ({ message = 'secret-token=do-not-render' }: { message?: string }) => {
  throw new Error(message);
};

describe('RouteErrorBoundary', () => {
  it('contains a route render failure without exposing sensitive error details', () => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => undefined);

    render(
      <RouteErrorBoundary pathname="/credentials/org/customer-secret-host?token=hidden" onDashboard={() => undefined}>
        <ThrowingRoute />
      </RouteErrorBoundary>,
    );

    expect(screen.getByTestId('route-recovery')).toBeTruthy();
    expect(screen.getByTestId('failed-route').textContent).toBe('/credentials/org/:id');
    expect(document.body.textContent).not.toContain('secret-token');
    expect(document.body.textContent).not.toContain('customer-secret-host');
    const praetorDiagnostics = log.mock.calls.filter(([message]) => message === 'Praetor route rendering failed');
    expect(praetorDiagnostics).toEqual([
      ['Praetor route rendering failed', { route: '/credentials/org/:id' }],
    ]);
    expect(JSON.stringify(praetorDiagnostics)).not.toContain('secret-token');
  });

  it('retries explicitly and returns to recovery when the route still fails', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    render(
      <RouteErrorBoundary pathname="/jobs/54" onDashboard={() => undefined}>
        <ThrowingRoute />
      </RouteErrorBoundary>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Retry page' }));
    expect(screen.getByTestId('route-recovery')).toBeTruthy();
  });

  it('offers a working Dashboard recovery action', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const onDashboard = vi.fn();
    render(
      <RouteErrorBoundary pathname="/service-principals" onDashboard={onDashboard}>
        <ThrowingRoute />
      </RouteErrorBoundary>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Go to Dashboard' }));
    expect(onDashboard).toHaveBeenCalledOnce();
  });

  it('restores normal content after navigation to another route', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const { rerender } = render(
      <RouteErrorBoundary pathname="/jobs/54" onDashboard={() => undefined}>
        <ThrowingRoute />
      </RouteErrorBoundary>,
    );

    rerender(
      <RouteErrorBoundary pathname="/jobs" onDashboard={() => undefined}>
        <p>Job history restored</p>
      </RouteErrorBoundary>,
    );

    expect(screen.getByText('Job history restored')).toBeTruthy();
    expect(screen.queryByTestId('route-recovery')).toBeNull();
  });
});
