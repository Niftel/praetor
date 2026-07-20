import React, { useEffect, useMemo, useState } from 'react';
import { api } from '../services/api';
import { RefreshCw, Search } from 'lucide-react';
import { DataTable, type DataColumn, type SortState, Page, PageHeader, PageToolbar, StatusValue, TimestampValue } from '../components/ui';

const actionTone = (a: string): string => {
  const s = (a || '').toLowerCase();
  if (s.includes('delete') || s.includes('remove')) return 'text-err border-err/30';
  if (s.includes('create') || s.includes('launch') || s.includes('add')) return 'text-ok border-ok/30';
  if (s.includes('update') || s.includes('edit') || s.includes('sync')) return 'text-changed border-changed/30';
  return 'text-mut border-line2';
};

const ActivityPage = () => {
  const [entries, setEntries] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState('');
  const [sort, setSort] = useState<SortState>({ column: 'when', direction: 'desc' });

  const load = () => {
    setLoading(true);
    api.getActivityStream(200).then(d => setEntries(d || [])).catch(() => setEntries([])).finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, []);

  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const filtered = !q ? entries : entries.filter(e =>
      [e.username, e.action, e.resource_type, String(e.resource_id), String(e.status_code)]
        .filter(Boolean).some(v => String(v).toLowerCase().includes(q)));
    return [...filtered].sort((a, b) => {
      const values: Record<string, [unknown, unknown]> = {
        when: [new Date(a.created_at).getTime(), new Date(b.created_at).getTime()],
        user: [a.username || '', b.username || ''],
        action: [a.action || '', b.action || ''],
        resource: [`${a.resource_type || ''}/${a.resource_id || ''}`, `${b.resource_type || ''}/${b.resource_id || ''}`],
        code: [a.status_code || 0, b.status_code || 0],
      };
      const [left, right] = values[sort.column] || values.when;
      const result = typeof left === 'number' && typeof right === 'number' ? left - right : String(left).localeCompare(String(right));
      return sort.direction === 'asc' ? result : -result;
    });
  }, [entries, filter, sort]);

  const columns: DataColumn<any>[] = [
    { id: 'when', header: 'When', sortable: true, cell: entry => <TimestampValue value={entry.created_at} className="text-[11px]" />, headerClassName: 'w-[190px]' },
    { id: 'user', header: 'User', sortable: true, cell: entry => <span className="block max-w-[180px] truncate text-ink2">{entry.username || '—'}</span> },
    { id: 'action', header: 'Action', sortable: true, cell: entry => <span className={`inline-block rounded border px-2 py-0.5 text-[10.5px] ${actionTone(entry.action)}`}>{entry.action}</span> },
    { id: 'resource', header: 'Resource', sortable: true, cell: entry => <span className="block max-w-[360px] truncate text-mut">{entry.resource_type}{entry.resource_id ? `/${entry.resource_id}` : ''}</span> },
    { id: 'code', header: 'Code', sortable: true, cell: entry => <StatusValue tone={entry.status_code >= 500 ? 'error' : entry.status_code >= 300 ? 'warning' : entry.status_code ? 'success' : 'neutral'} className="justify-end tabular-nums">{entry.status_code || '—'}</StatusValue>, headerClassName: 'w-[80px] text-right', cellClassName: 'text-right' },
  ];

  return (
    <Page layout="workspace" className="bg-bg text-ink">
      <PageHeader layout="workspace" title="Activity" description="Audit log of who changed or launched what — superuser / auditor only." />
      <PageToolbar className="mb-0 shrink-0 border-b border-line px-4 py-3 sm:px-6" summary={`${shown.length} ${shown.length === 1 ? 'entry' : 'entries'}`}>
        <div className="ml-auto flex w-full items-center gap-2 sm:w-auto">
          <div className="flex items-center gap-2 h-8 px-3 rounded-md border border-line2 w-[220px]">
            <Search size={13} className="text-dim shrink-0" />
            <input aria-label="Filter activity" value={filter} onChange={e => setFilter(e.target.value)} placeholder="Filter" className="min-w-0 flex-1 bg-transparent text-[12px] text-ink outline-none placeholder:text-dim font-mono" />
          </div>
          <button aria-label="Refresh activity" onClick={load} disabled={loading} className="w-8 h-8 grid place-items-center rounded-md text-mut hover:text-ink hover:bg-white/5" title="Refresh"><RefreshCw size={16} className={loading ? 'animate-spin' : ''} /></button>
        </div>
      </PageToolbar>
      <div className="flex-1 min-h-0 overflow-auto scroll-tint">
        <DataTable
          columns={columns}
          rows={shown}
          rowKey={entry => entry.id}
          sort={sort}
          onSortChange={setSort}
          loading={loading}
          emptyTitle={filter ? 'No matching activity' : 'No activity recorded'}
          emptyDescription={filter ? 'Change or clear the filter to see more audit entries.' : 'Audited actions will appear here as they occur.'}
          className="border-t-0"
        />
      </div>
    </Page>
  );
};

export default ActivityPage;
