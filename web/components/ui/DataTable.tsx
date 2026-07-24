import React from 'react';
import { ArrowDown, ArrowUp, ChevronsUpDown } from 'lucide-react';
import { EmptyState } from './StatePanel';
import { SelectionCheckbox } from './BulkSelection';

export type SortDirection = 'asc' | 'desc';
export interface SortState { column: string; direction: SortDirection }

export interface DataColumn<T> {
  id: string;
  header: React.ReactNode;
  cell: (row: T) => React.ReactNode;
  /** Enables the shared sort control. The page remains responsible for sorting data. */
  sortable?: boolean;
  headerClassName?: string;
  cellClassName?: string;
}

interface DataTableProps<T> {
  columns: DataColumn<T>[];
  rows: T[];
  rowKey: (row: T) => React.Key;
  sort?: SortState;
  onSortChange?: (sort: SortState) => void;
  onRowActivate?: (row: T) => void;
  rowLabel?: (row: T) => string;
  loading?: boolean;
  skeletonRows?: number;
  emptyTitle?: string;
  emptyDescription?: React.ReactNode;
  className?: string;
  selection?: {
    selectedKeys: ReadonlySet<React.Key>;
    allVisibleSelected: boolean;
    someVisibleSelected?: boolean;
    onToggle: (row: T) => void;
    onToggleAllVisible: () => void;
    isRowSelectable?: (row: T) => boolean;
    rowSelectionLabel: (row: T) => string;
    selectAllLabel?: string;
  };
}

/**
 * Semantic, horizontally scrollable data table for Praetor resource pages.
 * Filtering, pagination, and server interaction deliberately stay page-owned.
 */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  sort,
  onSortChange,
  onRowActivate,
  rowLabel,
  loading = false,
  skeletonRows = 6,
  emptyTitle = 'No results',
  emptyDescription,
  className = '',
  selection,
}: DataTableProps<T>) {
  const changeSort = (column: DataColumn<T>) => {
    if (!column.sortable || !onSortChange) return;
    onSortChange({
      column: column.id,
      direction: sort?.column === column.id && sort.direction === 'asc' ? 'desc' : 'asc',
    });
  };

  if (!loading && rows.length === 0) {
    return <EmptyState title={emptyTitle} description={emptyDescription} />;
  }

  return (
    <div className={`min-w-0 overflow-x-auto border-y border-line scroll-tint ${className}`}>
      <table className="w-full min-w-[720px] border-collapse text-left text-[12px]">
        <thead className="bg-bg">
          <tr>
            {selection && (
              <th scope="col" className="h-8 w-11 border-b border-line px-3 text-center first:pl-4">
                <SelectionCheckbox
                  checked={selection.allVisibleSelected}
                  indeterminate={selection.someVisibleSelected}
                  label={selection.selectAllLabel ?? 'Select all visible rows'}
                  onChange={selection.onToggleAllVisible}
                />
              </th>
            )}
            {columns.map(column => {
              const active = sort?.column === column.id;
              const ariaSort = active ? (sort?.direction === 'asc' ? 'ascending' : 'descending') : 'none';
              return (
                <th key={column.id} scope="col" aria-sort={column.sortable ? ariaSort : undefined} className={`h-8 border-b border-line px-4 font-mono text-[9.5px] font-medium uppercase tracking-[0.1em] text-dim first:pl-6 last:pr-6 ${column.headerClassName ?? ''}`}>
                  {column.sortable ? (
                    <button type="button" onClick={() => changeSort(column)} className="inline-flex items-center gap-1.5 rounded-sm hover:text-ink focus-visible:text-ink">
                      <span>{column.header}</span>
                      {active ? (sort?.direction === 'asc' ? <ArrowUp size={11} /> : <ArrowDown size={11} />) : <ChevronsUpDown size={11} className="text-faint" />}
                    </button>
                  ) : column.header}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {loading && rows.length === 0
              ? Array.from({ length: skeletonRows }, (_, index) => (
                <tr key={`skeleton-${index}`} aria-hidden="true" className="border-b border-line last:border-b-0">
                  {selection && <td className="h-[43px] w-11 px-3 first:pl-4"><span className="mx-auto block h-3.5 w-3.5 animate-pulse rounded bg-white/[0.06]" /></td>}
                  {columns.map((column, columnIndex) => (
                    <td key={column.id} className={`h-[43px] px-4 first:pl-6 last:pr-6 ${column.cellClassName ?? ''}`}>
                      <span className={`block h-2.5 animate-pulse rounded bg-white/[0.06] ${columnIndex === 0 ? 'w-28' : 'w-20'}`} />
                    </td>
                  ))}
                </tr>
              ))
            : rows.map(row => {
                const interactive = Boolean(onRowActivate);
                const activate = () => onRowActivate?.(row);
                return (
                  <tr
                    key={rowKey(row)}
                    tabIndex={interactive ? 0 : undefined}
                    aria-label={interactive ? rowLabel?.(row) : undefined}
                    onClick={interactive ? activate : undefined}
                    onKeyDown={interactive ? event => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault();
                        activate();
                      }
                    } : undefined}
                    className={`border-b border-line last:border-b-0 hover:bg-white/[0.025] ${interactive ? 'cursor-pointer focus-visible:bg-white/[0.035]' : ''}`}
                  >
                    {selection && (() => {
                      const key = rowKey(row);
                      const selectable = selection.isRowSelectable?.(row) ?? true;
                      return (
                        <td className="h-[43px] w-11 px-3 text-center first:pl-4" onClick={event => event.stopPropagation()}>
                          <SelectionCheckbox
                            checked={selection.selectedKeys.has(key)}
                            disabled={!selectable}
                            label={selection.rowSelectionLabel(row)}
                            onChange={() => selection.onToggle(row)}
                          />
                        </td>
                      );
                    })()}
                    {columns.map(column => <td key={column.id} className={`h-[43px] px-4 first:pl-6 last:pr-6 ${column.cellClassName ?? ''}`}>{column.cell(row)}</td>)}
                  </tr>
                );
              })}
        </tbody>
      </table>
      {loading && rows.length > 0 && <div role="status" className="sr-only">Refreshing data</div>}
    </div>
  );
}
