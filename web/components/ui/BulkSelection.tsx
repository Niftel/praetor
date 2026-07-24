import React, { useEffect, useId, useMemo, useRef, useState } from 'react';
import { AlertTriangle, CheckCircle2, Loader, RotateCcw, XCircle } from 'lucide-react';
import Button from './Button';

export type SelectionKey = string | number;

export interface BulkResult {
  index: number;
  identifier?: string;
  status: string;
  http_status: number;
  code?: string;
  error?: string;
  job_id?: number;
  host_id?: number;
}

export interface BulkSelectionState<K extends SelectionKey> {
  selected: ReadonlySet<K>;
  selectedCount: number;
  allVisibleSelected: boolean;
  someVisibleSelected: boolean;
  toggle: (key: K) => void;
  toggleAllVisible: () => void;
  clear: () => void;
  replace: (keys: Iterable<K>) => void;
}

/**
 * Shared selection state for bounded resource operations.
 *
 * Filtering does not discard hidden selections. A resource disappearing from
 * `availableKeys` does, which prevents refreshes from retaining stale targets.
 */
export function useBulkSelection<K extends SelectionKey>(
  availableKeys: readonly K[],
  visibleKeys: readonly K[] = availableKeys,
  limit = Number.POSITIVE_INFINITY,
): BulkSelectionState<K> {
  const [selected, setSelected] = useState<Set<K>>(new Set());
  const available = useMemo(() => new Set(availableKeys), [availableKeys]);

  useEffect(() => {
    setSelected(current => {
      const next = new Set([...current].filter(key => available.has(key)));
      return next.size === current.size ? current : next;
    });
  }, [available]);

  const selectableVisible = visibleKeys.filter(key => available.has(key));
  const visibleSelectedCount = selectableVisible.filter(key => selected.has(key)).length;
  const allVisibleSelected = selectableVisible.length > 0 && visibleSelectedCount === selectableVisible.length;

  const toggle = (key: K) => {
    if (!available.has(key)) return;
    setSelected(current => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else if (next.size < limit) next.add(key);
      return next;
    });
  };

  const toggleAllVisible = () => {
    setSelected(current => {
      const next = new Set(current);
      if (allVisibleSelected) {
        selectableVisible.forEach(key => next.delete(key));
      } else {
        for (const key of selectableVisible) {
          if (next.size >= limit) break;
          next.add(key);
        }
      }
      return next;
    });
  };

  return {
    selected,
    selectedCount: selected.size,
    allVisibleSelected,
    someVisibleSelected: visibleSelectedCount > 0 && !allVisibleSelected,
    toggle,
    toggleAllVisible,
    clear: () => setSelected(new Set()),
    replace: keys => setSelected(new Set([...keys].filter(key => available.has(key)).slice(0, limit))),
  };
}

export function SelectionCheckbox({
  checked,
  indeterminate = false,
  label,
  disabled = false,
  onChange,
}: {
  checked: boolean;
  indeterminate?: boolean;
  label: string;
  disabled?: boolean;
  onChange: () => void;
}) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate;
  }, [indeterminate]);

  return (
    <input
      ref={ref}
      type="checkbox"
      checked={checked}
      disabled={disabled}
      aria-label={label}
      onChange={onChange}
      className="h-3.5 w-3.5 cursor-pointer rounded border-line2 bg-panel accent-[var(--acc)] disabled:cursor-not-allowed disabled:opacity-40"
    />
  );
}

export function BulkActionBar({
  selectedCount,
  limit,
  busy = false,
  busyLabel = 'Working',
  onClear,
  children,
}: {
  selectedCount: number;
  limit?: number;
  busy?: boolean;
  busyLabel?: string;
  onClear: () => void;
  children: React.ReactNode;
}) {
  if (selectedCount === 0) return null;
  return (
    <div
      role="region"
      aria-label="Bulk actions"
      className="flex min-h-11 items-center gap-3 border-b border-line2 bg-panel2 px-4 py-2 sm:px-6 max-[600px]:flex-wrap"
    >
      <span className="font-mono text-[13px] tabular-nums text-ink2">
        {selectedCount} selected{limit ? ` · ${limit} maximum` : ''}
      </span>
      <div className="ml-auto flex items-center gap-2 max-[600px]:ml-0">
        {busy && <span role="status" className="inline-flex items-center gap-1.5 text-[13px] text-mut"><Loader size={12} className="animate-spin" />{busyLabel}</span>}
        {children}
        <Button size="sm" variant="ghost" onClick={onClear} disabled={busy}>Clear</Button>
      </div>
    </div>
  );
}

const resultTone = (result: BulkResult) => {
  if (['accepted', 'launched', 'created', 'deleted', 'ready', 'successful'].includes(result.status)) return 'success';
  if (result.status === 'pending' || result.status === 'running') return 'pending';
  return 'error';
};

export function BulkResultPanel({
  title,
  results,
  running = false,
  onRetryFailed,
  onDismiss,
}: {
  title: string;
  results: BulkResult[];
  running?: boolean;
  onRetryFailed?: (failed: BulkResult[]) => void;
  onDismiss?: () => void;
}) {
  const headingId = useId();
  const failed = results.filter(result => resultTone(result) === 'error');
  const succeeded = results.filter(result => resultTone(result) === 'success');

  if (!running && results.length === 0) return null;
  return (
    <section aria-labelledby={headingId} aria-live="polite" className="border-b border-line bg-panel2">
      <div className="flex items-start gap-3 px-4 py-3 sm:px-6">
        {running ? <Loader size={15} className="mt-0.5 shrink-0 animate-spin text-acc" /> :
          failed.length ? <AlertTriangle size={15} className="mt-0.5 shrink-0 text-changed" /> :
            <CheckCircle2 size={15} className="mt-0.5 shrink-0 text-ok" />}
        <div className="min-w-0 flex-1">
          <h2 id={headingId} className="text-[12.5px] font-medium text-ink">{title}</h2>
          <p className="mt-0.5 font-mono text-[10.5px] tabular-nums text-mut">
            {running ? 'The server is processing the bounded request.' : `${succeeded.length} succeeded · ${failed.length} failed`}
          </p>
        </div>
        {!running && failed.length > 0 && onRetryFailed && (
          <Button size="sm" variant="secondary" icon={<RotateCcw size={12} />} onClick={() => onRetryFailed(failed)}>Retry failed</Button>
        )}
        {!running && onDismiss && <Button size="sm" variant="ghost" onClick={onDismiss}>Dismiss</Button>}
      </div>
      {results.length > 0 && (
        <ul className="max-h-44 overflow-auto border-t border-line px-4 py-1 scroll-tint sm:px-6">
          {results.map(result => {
            const tone = resultTone(result);
            return (
              <li key={`${result.index}-${result.identifier ?? result.host_id ?? result.job_id ?? 'item'}`} className="flex min-h-8 items-center gap-2 border-b border-line py-1.5 last:border-b-0">
                {tone === 'success' ? <CheckCircle2 size={12} className="shrink-0 text-ok" /> :
                  tone === 'pending' ? <Loader size={12} className="shrink-0 animate-spin text-run" /> :
                    <XCircle size={12} className="shrink-0 text-err" />}
                <span className="min-w-0 flex-1 truncate font-mono text-[13px] text-ink2">{result.identifier || `Item ${result.index + 1}`}</span>
                <span className={`shrink-0 text-[10.5px] ${tone === 'success' ? 'text-ok' : tone === 'pending' ? 'text-run' : 'text-err'}`}>{result.status}</span>
                {result.error && <span className="max-w-[42ch] truncate text-[10.5px] text-mut" title={result.error}>{result.error}</span>}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
