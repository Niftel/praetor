import React, { useEffect, useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { api } from '../services/api';
import { WorkflowJob } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import WorkflowDag from '../components/WorkflowDag';
import RunLifecycle from '../components/RunLifecycle';
import { ArrowLeft, Check, X, RefreshCw, ChevronDown, ChevronRight } from 'lucide-react';

const TERMINAL = ['successful', 'failed', 'error'];

const statusVariant = (s?: string): 'success' | 'error' | 'info' | 'warning' | 'neutral' => {
  if (s === 'successful') return 'success';
  if (s === 'failed' || s === 'error') return 'error';
  if (s === 'running') return 'info';
  return 'neutral';
};

const WorkflowRunPage = () => {
  const { jobId } = useParams();
  const navigate = useNavigate();
  const id = Number(jobId);
  const [job, setJob] = useState<WorkflowJob | null>(null);
  const [loading, setLoading] = useState(true);
  const [acting, setActing] = useState<number | null>(null);
  const [error, setError] = useState('');
  const [expanded, setExpanded] = useState<number | null>(null);

  const refresh = useCallback(() => {
    if (!id) return;
    return api.getWorkflowJob(id)
      .then(j => { setJob(j); setError(''); })
      .catch(() => setError('Could not load this workflow run.'))
      .finally(() => setLoading(false));
  }, [id]);

  // Poll while the run is active; stop once it reaches a terminal state.
  useEffect(() => {
    let active = true;
    refresh();
    const h = setInterval(() => {
      if (!active) return;
      if (job && TERMINAL.includes(job.status)) return;
      refresh();
    }, 2500);
    return () => { active = false; clearInterval(h); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, job?.status]);

  const decide = async (nodeId: number, approve: boolean) => {
    setActing(nodeId);
    try {
      await (approve ? api.approveWorkflowNode(nodeId) : api.denyWorkflowNode(nodeId));
      await refresh();
    } catch (e: any) {
      setError(e.message || 'Action failed.');
    } finally {
      setActing(null);
    }
  };

  const release = async (nodeId: number, callbackUrl: string, fail: boolean) => {
    setActing(nodeId);
    try {
      await api.releaseWorkflowNode(callbackUrl, fail);
      await refresh();
    } catch (e: any) {
      setError(e.message || 'Callback failed.');
    } finally {
      setActing(null);
    }
  };

  const statusByKey: Record<string, string> = {};
  (job?.nodes || []).forEach(n => { statusByKey[n.node_key] = n.status; });
  const gates = (job?.nodes || []).filter(n => n.status === 'awaiting_approval');
  const waiters = (job?.nodes || []).filter(n => n.status === 'awaiting_event');
  const isTerminal = job ? TERMINAL.includes(job.status) : false;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <button onClick={() => navigate('/workflows')} className="text-gray-500 hover:text-gray-900 p-2 rounded-lg hover:bg-gray-100" title="Back to workflows">
            <ArrowLeft size={20} />
          </button>
          <div>
            <h1 className="text-2xl font-bold text-gray-900">{job?.name || 'Workflow run'}</h1>
            <p className="text-sm text-gray-500 mt-0.5">Run #{id}{job?.created_at ? ` · started ${new Date(job.created_at).toLocaleString()}` : ''}</p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <Badge variant={statusVariant(job?.status)}>{job?.status || (loading ? 'loading…' : 'unknown')}</Badge>
          {!isTerminal && (
            <button onClick={() => refresh()} className="text-gray-600 hover:text-gray-900 p-2 rounded-lg hover:bg-gray-100" title="Refresh">
              <RefreshCw size={18} />
            </button>
          )}
        </div>
      </div>

      {error && <div className="text-sm text-red-600 bg-red-50 border border-red-200 rounded-md px-3 py-2">{error}</div>}

      {/* Approval gates awaiting a decision — always available while the run is open. */}
      {gates.length > 0 && (
        <Card title="Approvals required">
          <div className="space-y-3">
            {gates.map(g => (
              <div key={g.id} className="flex items-center justify-between bg-amber-50 border border-amber-200 rounded-md px-4 py-3">
                <span className="text-sm text-amber-900">⏸ <b>{g.name || g.node_key}</b> is waiting for a decision.</span>
                <div className="flex gap-2">
                  <Button variant="primary" size="sm" icon={<Check size={14} />} disabled={acting === g.id} onClick={() => decide(g.id, true)}>Approve</Button>
                  <Button variant="danger" size="sm" icon={<X size={14} />} disabled={acting === g.id} onClick={() => decide(g.id, false)}>Deny</Button>
                </div>
              </div>
            ))}
          </div>
        </Card>
      )}

      {/* Webhook_in nodes waiting on a remote event. Shows the callback URL to wire
          an external system, or release/fail the node manually. */}
      {waiters.length > 0 && (
        <Card title="Waiting for events">
          <div className="space-y-3">
            {waiters.map(wnode => (
              <div key={wnode.id} className="bg-purple-50 border border-purple-200 rounded-md px-4 py-3 space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-purple-900">📥 <b>{wnode.name || wnode.node_key}</b> is waiting for a remote event.</span>
                  <div className="flex gap-2">
                    <Button variant="primary" size="sm" icon={<Check size={14} />} disabled={acting === wnode.id || !wnode.callback_url} onClick={() => wnode.callback_url && release(wnode.id, wnode.callback_url, false)}>Release</Button>
                    <Button variant="danger" size="sm" icon={<X size={14} />} disabled={acting === wnode.id || !wnode.callback_url} onClick={() => wnode.callback_url && release(wnode.id, wnode.callback_url, true)}>Fail</Button>
                  </div>
                </div>
                {wnode.callback_url && (
                  <div className="flex items-center gap-2">
                    <code className="flex-1 text-[11px] bg-white border border-purple-200 rounded px-2 py-1 text-purple-800 truncate">POST {wnode.callback_url}</code>
                    <button className="text-xs text-purple-700 hover:underline whitespace-nowrap" onClick={() => navigator.clipboard?.writeText(`${window.location.origin}${wnode.callback_url}`)}>Copy</button>
                  </div>
                )}
              </div>
            ))}
          </div>
        </Card>
      )}

      <Card title="Graph">
        {job
          ? <WorkflowDag nodes={(job.nodes || []).map(n => ({ node_key: n.node_key, node_type: n.node_type, name: n.name || '', job_template_id: null }))} edges={job.edges || []} statusByKey={statusByKey} />
          : <div className="text-sm text-gray-400 py-8 text-center">{loading ? 'Loading…' : 'Not found.'}</div>}
      </Card>

      <Card title="Nodes">
        <table className="min-w-full divide-y divide-gray-200">
          <thead><tr>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-500 uppercase">Node</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-500 uppercase">Job</th>
          </tr></thead>
          <tbody className="divide-y divide-gray-100">
            {(job?.nodes || []).map(n => {
              const open = expanded === n.id;
              const canExpand = !!n.run_id;
              return (
                <React.Fragment key={n.id}>
                  <tr className={canExpand ? 'cursor-pointer hover:bg-gray-50' : ''} onClick={() => canExpand && setExpanded(open ? null : n.id)}>
                    <td className="px-3 py-2 text-sm font-medium text-gray-900">
                      <span className="flex items-center gap-1.5">
                        {canExpand
                          ? (open ? <ChevronDown size={14} className="text-gray-400" /> : <ChevronRight size={14} className="text-gray-400" />)
                          : <span className="w-[14px]" />}
                        {n.name || n.node_key}
                      </span>
                    </td>
                    <td className="px-3 py-2 text-sm text-gray-500">{n.node_type}</td>
                    <td className="px-3 py-2 text-sm">
                      <Badge variant={statusVariant(n.status)}>{n.status}</Badge>
                    </td>
                    <td className="px-3 py-2 text-sm text-gray-500">
                      {n.unified_job_id
                        ? <button className="text-brand-600 hover:underline" onClick={e => { e.stopPropagation(); navigate(`/jobs/${n.unified_job_id}`); }}>job #{n.unified_job_id}</button>
                        : '—'}
                    </td>
                  </tr>
                  {open && n.run_id && (
                    <tr>
                      <td colSpan={4} className="px-6 py-3 bg-gray-50 border-l-2 border-brand-200">
                        <RunLifecycle runId={n.run_id} />
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              );
            })}
          </tbody>
        </table>
      </Card>
    </div>
  );
};

export default WorkflowRunPage;
