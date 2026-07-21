import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { AlertTriangle, CheckCircle2, Clock3, Loader } from 'lucide-react';
import { api } from '../services/api';
import { InventorySyncHistory } from '../types';

interface Props {
  inventoryId: number;
  sourceId: number;
}

const statusIcon = (entry: InventorySyncHistory) => {
  if (entry.status === 'successful') return <CheckCircle2 size={14} className="text-ok" />;
  if (entry.status === 'failed') return <AlertTriangle size={14} className="text-err" />;
  if (entry.status === 'running') return <Loader size={14} className="text-acc animate-spin" />;
  return <Clock3 size={14} className="text-dim" />;
};

export default function InventorySourceHistoryList({ inventoryId, sourceId }: Props) {
  const [entries, setEntries] = useState<InventorySyncHistory[]>([]);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setFailed(false);
    api.getInventorySourceHistory(inventoryId, sourceId, { limit: 10 })
      .then(response => { if (active) setEntries(response.results || []); })
      .catch(() => { if (active) setFailed(true); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [inventoryId, sourceId]);

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
          </div>
        </div>
      ))}
    </div>
  );
}
