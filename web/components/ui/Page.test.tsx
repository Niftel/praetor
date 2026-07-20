import React from 'react';
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { EmptyState, ErrorState, LoadingState, Page, PageHeader, PageToolbar } from './index';

afterEach(cleanup);

describe('Praetor page framework', () => {
  it('provides one semantic main and page heading', () => {
    render(<Page><PageHeader title="Jobs" description="Execution history" actions={<button>Launch</button>} /></Page>);
    expect(screen.getByRole('main')).toBeTruthy();
    expect(screen.getByRole('heading', { level: 1, name: 'Jobs' })).toBeTruthy();
    expect(screen.getByText('Execution history')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Launch' })).toBeTruthy();
  });

  it('keeps toolbar controls and result summary discoverable', () => {
    render(<PageToolbar summary="12 results"><input aria-label="Search jobs" /></PageToolbar>);
    expect(screen.getByRole('textbox', { name: 'Search jobs' })).toBeTruthy();
    expect(screen.getByText('12 results')).toBeTruthy();
  });

  it('announces loading and error states', () => {
    const { rerender } = render(<LoadingState label="Loading jobs" />);
    expect(screen.getByRole('status').textContent).toContain('Loading jobs');
    rerender(<ErrorState title="Jobs unavailable" description="The API could not be reached." />);
    expect(screen.getByRole('alert')).toBeTruthy();
  });

  it('uses a meaningful empty-state heading', () => {
    render(<EmptyState title="No templates yet" description="Create a template to launch automation." />);
    expect(screen.getByRole('heading', { level: 2, name: 'No templates yet' })).toBeTruthy();
  });
});
