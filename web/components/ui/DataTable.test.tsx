import React from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DataTable, type DataColumn } from './DataTable';

type Row = { id: number; name: string };
const columns: DataColumn<Row>[] = [
  { id: 'name', header: 'Name', sortable: true, cell: row => row.name },
  { id: 'id', header: 'ID', cell: row => row.id },
];

afterEach(cleanup);

describe('DataTable', () => {
  it('renders semantic columns and exposes sort state', () => {
    const onSort = vi.fn();
    render(<DataTable columns={columns} rows={[{ id: 1, name: 'Deploy' }]} rowKey={row => row.id} sort={{ column: 'name', direction: 'asc' }} onSortChange={onSort} />);
    expect(screen.getByRole('columnheader', { name: /Name/ }).getAttribute('aria-sort')).toBe('ascending');
    fireEvent.click(screen.getByRole('button', { name: /Name/ }));
    expect(onSort).toHaveBeenCalledWith({ column: 'name', direction: 'desc' });
  });

  it('activates interactive rows with the keyboard', () => {
    const onActivate = vi.fn();
    render(<DataTable columns={columns} rows={[{ id: 1, name: 'Deploy' }]} rowKey={row => row.id} onRowActivate={onActivate} rowLabel={row => `Open ${row.name}`} />);
    fireEvent.keyDown(screen.getByRole('row', { name: 'Open Deploy' }), { key: 'Enter' });
    expect(onActivate).toHaveBeenCalledWith({ id: 1, name: 'Deploy' });
  });

  it('renders skeleton rows only when no retained data exists', () => {
    const { container, rerender } = render(<DataTable columns={columns} rows={[]} rowKey={row => row.id} loading skeletonRows={3} />);
    expect(container.querySelectorAll('tbody tr')).toHaveLength(3);
    rerender(<DataTable columns={columns} rows={[{ id: 1, name: 'Retained' }]} rowKey={row => row.id} loading />);
    expect(screen.getByText('Retained')).toBeTruthy();
    expect(screen.getByRole('status').textContent).toBe('Refreshing data');
  });

  it('renders a teaching empty state', () => {
    render(<DataTable columns={columns} rows={[]} rowKey={row => row.id} emptyTitle="No activity" emptyDescription="Actions will appear here." />);
    expect(screen.getByRole('heading', { name: 'No activity' })).toBeTruthy();
  });
});
