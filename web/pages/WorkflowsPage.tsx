import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams, Link } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Workflow, WorkflowRunSummary } from '../types';
import { Plus, Trash2, Rocket, GitFork, Pencil, ChevronDown, ChevronRight, ArrowLeft } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import WorkflowLaunchModal, { WorkflowLaunchOptions } from '../components/WorkflowLaunchModal';
import { useCapabilities } from '../lib/useCapabilities';

const runTone = (s: string): { text: string; dot: string } => {
  if (s === 'successful') return { text: 'text-ok', dot: 'bg-ok' };
  if (s === 'failed' || s === 'error') return { text: 'text-err', dot: 'bg-err' };
  if (s === 'running' || s === 'pending') return { text: 'text-run', dot: 'bg-run' };
  return { text: 'text-mut', dot: 'bg-dim' };
};

const WorkflowsPage = () => {
  const navigate = useNavigate();
  const { orgId: orgIdStr } = useParams();
  const orgId = Number(orgIdStr);
  const [orgName, setOrgName] = useState('');
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [runs, setRuns] = useState<WorkflowRunSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [collapsed, setCollapsed] = useState<Record<number, boolean>>({});
  const [launching, setLaunching] = useState<Workflow | null>(null);
  const { capabilities: orgCapabilities, loading: orgCapabilitiesLoading } = useCapabilities('organization', orgId);
  const canCreate = !!orgCapabilities.add_workflow_template;

  // silent=true for background polls so the list stays live without flashing the
  // full-page spinner or disturbing scroll.
  const load = async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const [workflowResult, runResult, organizationResult] = await Promise.allSettled([
        api.getWorkflows(),
        api.getWorkflowJobs(),
        api.getOrganizations(),
      ]);

      // A background refresh must never replace confirmed data with an empty
      // state just because one request failed. Update each slice independently
      // only when its endpoint returned successfully.
      if (workflowResult.status === 'fulfilled') {
        setWorkflows(unwrap<Workflow>(workflowResult.value).filter(w => (w as any).organization_id === orgId));
      }
      if (runResult.status === 'fulfilled') {
        setRuns(unwrap<WorkflowRunSummary>(runResult.value));
      }
      if (organizationResult.status === 'fulfilled') {
        setOrgName(unwrap<{ id: number; name: string }>(organizationResult.value).find(x => x.id === orgId)?.name ?? `Org ${orgId}`);
      }
    } finally {
      if (!silent) setLoading(false);
    }
  };
  useEffect(() => {
    load();
    const h = setInterval(() => load(true), 5000);
    return () => clearInterval(h);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orgId]);

  const runGroups = useMemo(() => {
    const m = new Map<number, { id: number; name: string; runs: WorkflowRunSummary[] }>();
    for (const r of runs) {
      if (!m.has(r.workflow_template_id)) m.set(r.workflow_template_id, { id: r.workflow_template_id, name: r.template_name, runs: [] });
      m.get(r.workflow_template_id)!.runs.push(r);
    }
    return [...m.values()];
  }, [runs]);
  const toggleGroup = (id: number) => setCollapsed(c => ({ ...c, [id]: !c[id] }));

  const launch = async (options: WorkflowLaunchOptions, signal?: AbortSignal) => {
    if (!launching) return;
    const res = await api.launchWorkflow(launching.id, options, signal);
    navigate(`/workflows/runs/${res.workflow_job_id}`);
  };
  const remove = async (wf: Workflow) => {
    if (!(await confirmDialog(`Delete workflow "${wf.name}"?`, { destructive: true, confirmText: 'Delete' }))) return;
    try { await api.deleteWorkflow(wf.id); setWorkflows(ws => ws.filter(w => w.id !== wf.id)); } catch { toast.error('Failed to delete'); }
  };
  const edit = (wf: Workflow) => navigate(`/workflows/org/${orgId}/builder/${wf.id}`);

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink overflow-auto scroll-tint">
      <div className="max-w-[1060px] w-full mx-auto px-8 pt-6 pb-16">
        <div className="flex flex-wrap items-start gap-4 mb-6">
          <div className="min-w-0 flex-1">
            <Link to="/workflows" className="inline-flex items-center gap-1.5 text-[12px] text-mut hover:text-acc"><ArrowLeft size={14} /> Organizations</Link>
            <h1 className="text-[21px] font-semibold tracking-tight mt-1.5 flex items-center gap-2"><GitFork size={20} className="text-acc2" /> {orgName} · Workflows</h1>
            <p className="mt-2 text-[12.5px] text-mut max-w-[560px] leading-relaxed">Chain job templates into a DAG with success / failure / always edges, approval gates, and webhook steps.</p>
          </div>
          {!orgCapabilitiesLoading && canCreate && (
            <button
              onClick={() => navigate(`/workflows/org/${orgId}/builder`)}
              className="h-9 px-4 rounded-lg text-[12.5px] font-semibold inline-flex items-center gap-1.5 bg-acc text-[#04211d] hover:bg-acc2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-acc/60 focus-visible:ring-offset-2 focus-visible:ring-offset-bg"
            >
              <Plus size={15} /> New workflow
            </button>
          )}
        </div>

        {/* Catalog */}
        <div className="rounded-2xl border border-line overflow-hidden mb-8">
          {workflows.map(wf => (
            <WorkflowCatalogRow key={wf.id} workflow={wf} edit={edit} launch={setLaunching} remove={remove} />
          ))}
          {workflows.length === 0 && !loading && (
            <div className="px-6 py-12 text-center">
              <GitFork size={34} className="mx-auto mb-3 text-dim opacity-40" />
              <p className="text-sm text-dim mb-4">No workflows in this organization yet.</p>
              {!orgCapabilitiesLoading && canCreate && <button onClick={() => navigate(`/workflows/org/${orgId}/builder`)} className="h-9 px-4 rounded-lg text-[12.5px] font-semibold inline-flex items-center gap-1.5 bg-acc text-[#04211d] hover:bg-acc2"><Plus size={15} /> New workflow</button>}
            </div>
          )}
        </div>

        {/* Recent runs */}
        <div className="font-mono text-[10px] tracking-[0.16em] uppercase text-mut mb-3">Recent runs</div>
        <div className="rounded-2xl border border-line overflow-hidden">
          {runGroups.length === 0 && !loading && <p className="px-5 py-8 text-center text-sm text-dim">No runs yet. Launch a workflow to see it here.</p>}
          {runGroups.map(g => {
            const open = !collapsed[g.id];
            const active = g.runs.filter(r => r.status === 'running').length;
            return (
              <div key={g.id} className="border-b border-line last:border-0">
                <button onClick={() => toggleGroup(g.id)} className="w-full flex items-center gap-2.5 px-5 py-3 hover:bg-white/[0.02] text-left">
                  {open ? <ChevronDown size={15} className="text-dim" /> : <ChevronRight size={15} className="text-dim" />}
                  <span className="text-[13.5px] font-medium truncate">{g.name}</span>
                  <span className="font-mono text-[11px] text-dim">{g.runs.length} run{g.runs.length === 1 ? '' : 's'}</span>
                  {active > 0 && <span className="font-mono text-[10px] text-run">{active} running</span>}
                  <span className="ml-auto font-mono text-[10.5px] text-dim">latest {new Date(g.runs[0].created_at).toLocaleString()}</span>
                </button>
                {open && g.runs.map(r => {
                  const t = runTone(r.status);
                  return (
                    <div key={r.id} onClick={() => navigate(`/workflows/runs/${r.id}`)} className="flex items-center gap-3 pl-12 pr-5 py-2 border-t border-line hover:bg-white/[0.02] cursor-pointer">
                      <span className="font-mono text-[12px] text-acc2 w-14">#{r.id}</span>
                      <span className={`inline-flex items-center gap-1.5 text-[12px] ${t.text}`}><span className={`w-[6px] h-[6px] rounded-full ${t.dot} ${r.status === 'running' ? 'animate-pulse' : ''}`} />{r.status}</span>
                      <span className="ml-auto font-mono text-[11px] text-dim">{r.created_at ? new Date(r.created_at).toLocaleString() : '—'}</span>
                    </div>
                  );
                })}
              </div>
            );
          })}
        </div>
        <WorkflowLaunchModal isOpen={!!launching} workflowName={launching?.name || 'Workflow'} organizationId={orgId} onClose={() => setLaunching(null)} onLaunch={launch} />
      </div>
    </div>
  );
};

const WorkflowCatalogRow: React.FC<{
  workflow: Workflow;
  edit: (workflow: Workflow) => void;
  launch: (workflow: Workflow) => void;
  remove: (workflow: Workflow) => void;
}> = ({ workflow, edit, launch, remove }) => {
  const { capabilities, loading } = useCapabilities('workflow_template', workflow.id);
  const nodeCount = (workflow as any).nodes?.length;
  const canManage = !loading && capabilities.manage;
  const canLaunch = !loading && capabilities.execute;
  return (
    <div onClick={canManage ? () => edit(workflow) : undefined}
      className={`group flex items-center gap-3 px-5 py-4 border-b border-line last:border-0 hover:bg-white/[0.02] ${canManage ? 'cursor-pointer' : ''}`}>
      <span className="w-9 h-9 rounded-lg border border-line2 grid place-items-center text-acc2 shrink-0"><GitFork size={17} /></span>
      <div className="min-w-0">
        <div className="text-[14px] font-semibold tracking-tight truncate">{workflow.name}</div>
        <div className="font-mono text-[11px] text-dim mt-0.5">
          {typeof nodeCount === 'number' ? `${nodeCount} node${nodeCount === 1 ? '' : 's'}` : 'DAG'}
          {(workflow as any).webhook_enabled ? ' · webhook trigger' : ''}
        </div>
      </div>
      {!loading && !canManage && !canLaunch && <span className="ml-auto font-mono text-[10px] text-dim">read only</span>}
      {(canManage || canLaunch) && (
        <div className="ml-auto flex items-center gap-1.5" onClick={e => e.stopPropagation()}>
          {canManage && <button onClick={() => edit(workflow)} className="h-8 px-2.5 rounded-md text-[12px] font-medium flex items-center gap-1.5 text-ink2 hover:bg-white/5"><Pencil size={13} /> Edit</button>}
          {canLaunch && <button onClick={() => launch(workflow)} className="h-8 px-3 rounded-md text-[12px] font-semibold flex items-center gap-1.5 bg-acc/90 text-[#04211d] hover:bg-acc"><Rocket size={13} /> Launch</button>}
          {canManage && <button onClick={() => remove(workflow)} className="w-8 h-8 grid place-items-center rounded-md text-faint hover:text-err hover:bg-white/5" title="Delete"><Trash2 size={15} /></button>}
        </div>
      )}
    </div>
  );
};

export default WorkflowsPage;
