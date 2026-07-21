import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { AlertTriangle, Ban, CheckCircle2, Clock3, Loader, Square } from 'lucide-react';
import { api } from '../services/api';
import { InventorySyncHistory } from '../types';

interface Props {
  inventoryId: number;
  sourceId: number;
  onTerminal?: () => void;
  canCancel?: boolean;
}

const statusIcon = (entry: InventorySyncHistory) => {
  if (entry.status === 'successful') return <CheckCircle2 size={14} className="text-ok" />;
  if (entry.status === 'failed') return <AlertTriangle size={14} className="text-err" />;
  if (entry.status === 'canceled') return <Ban size={14} className="text-dim" />;
  if (entry.status === 'running') return <Loader size={14} className="text-acc animate-spin" />;
  return <Clock3 size={14} className="text-dim" />;
};

export default function InventorySourceHistoryList({ inventoryId, sourceId, onTerminal, canCancel = false }: Props) {
  const [entries, setEntries] = useState<InventorySyncHistory[]>([]);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let active = true;
    let timer: number | undefined;
    let hadActive = false;
    const load = async () => {
      try {
        const response = await api.getInventorySourceHistory(inventoryId, sourceId, { limit: 10 });
        if (!active) return;
        const next = response.results || [];
        const hasActive = next.some((entry: InventorySyncHistory) => entry.status === 'pending' || entry.status === 'running');
        setEntries(next); setFailed(false); setLoading(false);
        if (hadActive && !hasActive) onTerminal?.();
        hadActive = hasActive;
        if (hasActive) timer = window.setTimeout(load, 2000);
      } catch {
        if (active) { setFailed(true); setLoading(false); }
      }
    };
    setLoading(true); setFailed(false); load();
    return () => { active = false; if (timer) window.clearTimeout(timer); };
    // onTerminal is an event hook, not part of the identity of this source feed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inventoryId, sourceId]);

  const cancel = async (jobId: number) => {
    try {
      await api.cancelJob(jobId);
      setEntries(current => current.map(entry => entry.unified_job_id === jobId ? { ...entry, status: 'canceled', phase: 'completed' } : entry));
      onTerminal?.();
    } catch { setFailed(true); }
  };

  if (loading) return <div className="flex items-center gap-2 py-3 text-xs text-dim"><Loader size={13} className="animate-spin" /> Loading sync history…</div>;
  if (failed) return <p className="py-3 text-xs text-err">Sync history could not be loaded.</p>;
  if (entries.length === 0) return <p className="py-3 font-mono text-[11px] text-faint">No synchronization attempts yet.</p>;

  return (
    <div className="py-2 space-y-1" data-testid={`source-${sourceId}-history`}>
      {entries.map(entry => (
        <div key={entry.id} className="grid grid-cols-[18px_minmax(0,1fr)_auto] gap-x-2 gap-y-1 px-2 py-2 rounded-md bg-white/[0.02] text-[11px]">
          <span className="pt-0.5">{statusIcon(entry)}</span>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="font-medium text-ink capitalize">{entry.status}</span>
              <span className="font-mono text-dim">{entry.phase}</span>
              <span className="font-mono text-faint">{entry.reconciliation_policy.replace('_', ' ')}</span>
            </div>
            <p className="font-mono text-dim mt-1">
              +{entry.hosts_added} added · {entry.hosts_updated} updated · {entry.hosts_disabled} disabled · {entry.hosts_unchanged} unchanged
            </p>
            {entry.diagnostic_message && <p className="mt-1 text-err break-words">{entry.diagnostic_message}</p>}
          </div>
          <div className="text-right whitespace-nowrap text-dim">
            <div>{new Date(entry.created_at).toLocaleString()}</div>
            {entry.unified_job_id && <Link to={`/jobs/${entry.unified_job_id}`} className="font-mono text-acc hover:underline">job {entry.unified_job_id}</Link>}
            {canCancel && entry.unified_job_id && (entry.status === 'pending' || entry.status === 'running') && <button onClick={() => cancel(entry.unified_job_id!)} className="ml-2 inline-flex items-center gap-1 font-mono text-err hover:underline"><Square size={10} /> cancel</button>}
          </div>
        </div>
      ))}
    </div>
  );
}
