import React, { useEffect, useMemo, useState } from 'react';
import { api } from '../services/api';
import { RefreshCw, Search } from 'lucide-react';
import { PageSpinner } from '../components/ui/PageSpinner';

const statusTone = (code: number) => {
  if (!code) return 'text-dim';
  if (code < 300) return 'text-ok';
  if (code < 400) return 'text-changed';
  if (code < 500) return 'text-changed';
  return 'text-err';
};
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

  const load = () => {
    setLoading(true);
    api.getActivityStream(200).then(d => setEntries(d || [])).catch(() => setEntries([])).finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, []);

  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return entries;
    return entries.filter(e =>
      [e.username, e.action, e.resource_type, String(e.resource_id), String(e.status_code)]
        .filter(Boolean).some(v => String(v).toLowerCase().includes(q)));
  }, [entries, filter]);

  if (loading && entries.length === 0) return <PageSpinner />;

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink">
      <div className="flex items-center gap-4 px-8 pt-6 pb-4 shrink-0">
        <div>
          <h1 className="text-[19px] font-semibold tracking-tight">Activity</h1>
          <p className="text-[12.5px] text-mut mt-0.5">Audit log of who changed or launched what — superuser / auditor only.</p>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <div className="flex items-center gap-2 h-8 px-3 rounded-md border border-line2 w-[220px]">
            <Search size={13} className="text-dim shrink-0" />
            <input value={filter} onChange={e => setFilter(e.target.value)} placeholder="Filter" className="flex-1 bg-transparent outline-none text-[12px] text-ink placeholder:text-dim font-mono" />
          </div>
          <button onClick={load} disabled={loading} className="w-8 h-8 grid place-items-center rounded-md text-mut hover:text-ink hover:bg-white/5" title="Refresh"><RefreshCw size={16} className={loading ? 'animate-spin' : ''} /></button>
        </div>
      </div>

      <div className="grid grid-cols-[180px_140px_150px_1fr_70px] items-center px-8 h-[32px] border-y border-line shrink-0 font-mono text-[9.5px] tracking-[0.1em] uppercase text-dim max-[820px]:grid-cols-[150px_1fr_60px]">
        <span>When</span>
        <span className="max-[820px]:hidden">User</span>
        <span>Action</span>
        <span className="max-[820px]:hidden">Resource</span>
        <span className="text-right">Code</span>
      </div>

      <div className="flex-1 overflow-auto scroll-tint">
        {shown.map(e => (
          <div key={e.id} className="grid grid-cols-[180px_140px_150px_1fr_70px] items-center px-8 h-[42px] border-b border-line hover:bg-white/[0.02] font-mono text-[12px] max-[820px]:grid-cols-[150px_1fr_60px]">
            <span className="text-dim tabular-nums text-[11px]">{new Date(e.created_at).toLocaleString()}</span>
            <span className="text-ink2 truncate pr-3 max-[820px]:hidden">{e.username || '—'}</span>
            <span><span className={`inline-block px-2 py-0.5 rounded border text-[10.5px] ${actionTone(e.action)}`}>{e.action}</span></span>
            <span className="text-mut truncate pr-3 max-[820px]:hidden">{e.resource_type}{e.resource_id ? `/${e.resource_id}` : ''}</span>
            <span className={`text-right tabular-nums ${statusTone(e.status_code)}`}>{e.status_code || '—'}</span>
          </div>
        ))}
        {shown.length === 0 && <p className="px-8 py-10 text-center text-sm text-dim">{filter ? 'No matching activity.' : 'No activity recorded.'}</p>}
      </div>
    </div>
  );
};

export default ActivityPage;
