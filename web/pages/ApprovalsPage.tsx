import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Check, ExternalLink, RefreshCw, ShieldCheck, X } from 'lucide-react';
import { api } from '../services/api';
import { WorkflowApproval } from '../types';
import { PageSpinner } from '../components/ui/PageSpinner';

const elapsed = (iso: string) => {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000));
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
};

const remaining = (iso: string) => {
  const seconds = Math.max(0, Math.ceil((new Date(iso).getTime() - Date.now()) / 1000));
  if (seconds < 60) return `${seconds}s left`;
  if (seconds < 3600) return `${Math.ceil(seconds / 60)}m left`;
  if (seconds < 86400) return `${Math.ceil(seconds / 3600)}h left`;
  return `${Math.ceil(seconds / 86400)}d left`;
};

const ApprovalsPage = () => {
  const navigate = useNavigate();
  const [items, setItems] = useState<WorkflowApproval[]>([]);
  const [loading, setLoading] = useState(true);
  const [acting, setActing] = useState<number | null>(null);
  const [error, setError] = useState('');

  const load = useCallback((silent = false) => {
    if (!silent) setLoading(true);
    return api.getWorkflowApprovals()
      .then(rows => { setItems(rows || []); setError(''); })
      .catch((e: any) => setError(e?.message || 'Could not load approvals.'))
      .finally(() => { if (!silent) setLoading(false); });
  }, []);

  useEffect(() => {
    load();
    const timer = setInterval(() => load(true), 5000);
    return () => clearInterval(timer);
  }, [load]);

  const decide = async (item: WorkflowApproval, approve: boolean) => {
    setActing(item.id);
    setError('');
    try {
      await (approve ? api.approveWorkflowNode(item.id) : api.denyWorkflowNode(item.id));
      setItems(current => current.filter(row => row.id !== item.id));
    } catch (e: any) {
      setError(e?.message || 'This approval could not be updated. It may already have been decided.');
      await load(true);
    } finally {
      setActing(null);
    }
  };

  if (loading && items.length === 0) return <PageSpinner />;

  return (
    <div className="flex min-h-full flex-col bg-bg text-ink">
      <div className="flex items-start gap-4 px-8 pb-5 pt-6">
        <div>
          <h1 className="flex items-center gap-2 text-[19px] font-semibold tracking-tight"><ShieldCheck size={19} className="text-acc" /> Approvals</h1>
          <p className="mt-1 max-w-[68ch] text-[12.5px] text-mut">Workflow gates waiting for your decision. Only workflows where you hold approval access appear here.</p>
        </div>
        <button onClick={() => load()} disabled={loading} className="ml-auto grid h-8 w-8 place-items-center rounded-md text-mut hover:bg-white/5 hover:text-ink disabled:opacity-50" title="Refresh approvals">
          <RefreshCw size={15} className={loading ? 'animate-spin' : ''} />
        </button>
      </div>

      {error && <div role="alert" className="mx-8 mb-4 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-sm text-err">{error}</div>}

      <div className="grid h-8 grid-cols-[minmax(220px,1.3fr)_minmax(180px,1fr)_130px_130px_250px] items-center border-y border-line px-8 font-mono text-[9.5px] uppercase tracking-[0.1em] text-dim max-[920px]:grid-cols-[1fr_130px]">
        <span>Workflow</span><span className="max-[920px]:hidden">Approval gate</span><span className="max-[920px]:hidden">Requested by</span><span>Waiting / deadline</span><span className="text-right max-[920px]:hidden">Decision</span>
      </div>

      <div className="flex-1">
        {items.map(item => (
          <div key={item.id} className="grid min-h-[58px] grid-cols-[minmax(220px,1.3fr)_minmax(180px,1fr)_130px_130px_250px] items-center border-b border-line px-8 hover:bg-white/[0.02] max-[920px]:grid-cols-[1fr_130px]">
            <button onClick={() => navigate(`/workflows/runs/${item.workflow_job_id}`)} className="min-w-0 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-acc/60">
              <span className="block truncate text-[13.5px] font-medium text-ink">{item.workflow_name}</span>
              <span className="mt-0.5 block font-mono text-[10.5px] text-dim">run #{item.workflow_job_id}</span>
            </button>
            <div className="min-w-0 max-[920px]:hidden">
              <span className="block truncate text-[12.5px] text-ink2">{item.node_name}</span>
              <span className="mt-0.5 block truncate font-mono text-[10.5px] text-dim">{item.node_key}</span>
            </div>
            <span className="truncate pr-3 font-mono text-[11px] text-mut max-[920px]:hidden">{item.requested_by || 'automation'}</span>
            <span className="font-mono text-[11px] tabular-nums text-mut" title={`Waiting since ${new Date(item.awaiting_since).toLocaleString()}`}>
              <span className="block">{elapsed(item.awaiting_since)}</span>
              <span className={item.deadline ? 'text-changed' : 'text-dim'}>{item.deadline ? remaining(item.deadline) : 'no timeout'}</span>
            </span>
            <div className="flex justify-end gap-2 max-[920px]:col-span-2 max-[920px]:mt-2 max-[920px]:pb-3">
              <button onClick={() => navigate(`/workflows/runs/${item.workflow_job_id}`)} className="grid h-8 w-8 place-items-center rounded-md border border-line2 text-mut hover:border-white/25 hover:text-ink" title="Open workflow run"><ExternalLink size={13} /></button>
              <button disabled={acting === item.id} onClick={() => decide(item, false)} className="flex h-8 items-center gap-1.5 rounded-md border border-err/40 px-3 text-[12px] font-semibold text-err hover:bg-err/10 disabled:opacity-50"><X size={13} /> Deny</button>
              <button disabled={acting === item.id} onClick={() => decide(item, true)} className="flex h-8 items-center gap-1.5 rounded-md bg-acc px-3 text-[12px] font-semibold text-[#04211d] hover:bg-acc2 disabled:opacity-50"><Check size={13} /> Approve</button>
            </div>
          </div>
        ))}
        {items.length === 0 && !loading && (
          <div className="px-8 py-16 text-center">
            <ShieldCheck size={32} className="mx-auto mb-3 text-dim opacity-50" />
            <p className="text-sm font-medium text-ink2">No approvals are waiting.</p>
            <p className="mt-1 text-[12.5px] text-dim">New workflow gates will appear here automatically.</p>
          </div>
        )}
      </div>
    </div>
  );
};

export default ApprovalsPage;
