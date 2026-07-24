import React from 'react';
import { act, cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { BulkActionBar, BulkResultPanel, useBulkSelection } from './BulkSelection';

afterEach(cleanup);

describe('useBulkSelection', () => {
  it('selects visible resources without losing filtered selections', () => {
    const { result, rerender } = renderHook(
      ({ available, visible }) => useBulkSelection(available, visible, 3),
      { initialProps: { available: [1, 2, 3], visible: [1, 2] } },
    );
    act(() => result.current.toggleAllVisible());
    expect([...result.current.selected]).toEqual([1, 2]);
    rerender({ available: [1, 2, 3], visible: [3] });
    act(() => result.current.toggleAllVisible());
    expect([...result.current.selected]).toEqual([1, 2, 3]);
  });

  it('prunes resources that disappear after refresh and enforces the bound', () => {
    const { result, rerender } = renderHook(
      ({ available }) => useBulkSelection(available, available, 2),
      { initialProps: { available: [1, 2, 3] } },
    );
    act(() => result.current.replace([1, 2, 3]));
    expect([...result.current.selected]).toEqual([1, 2]);
    rerender({ available: [2, 3] });
    expect([...result.current.selected]).toEqual([2]);
  });
});

describe('bulk operation surfaces', () => {
  it('exposes selected count, actions, and clear behavior', () => {
    const clear = vi.fn();
    render(<BulkActionBar selectedCount={2} limit={25} onClear={clear}><button>Launch selected</button></BulkActionBar>);
    expect(screen.getByText('2 selected · 25 maximum')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }));
    expect(clear).toHaveBeenCalledOnce();
  });

  it('announces partial results and retries only failed items', () => {
    const retry = vi.fn();
    render(
      <BulkResultPanel
        title="Bulk launch finished"
        results={[
          { index: 0, identifier: 'Deploy web', status: 'launched', http_status: 201 },
          { index: 1, identifier: 'Deploy db', status: 'rejected', http_status: 403, error: 'Launch not permitted' },
        ]}
        onRetryFailed={retry}
      />,
    );
    expect(screen.getByText('1 succeeded · 1 failed')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry failed' }));
    expect(retry).toHaveBeenCalledWith([
      expect.objectContaining({ identifier: 'Deploy db', status: 'rejected' }),
    ]);
  });
});
