import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../services/api';
import { Workflow, WorkflowNode, WorkflowEdge, WorkflowNodeType, WorkflowEdgeType, WorkflowRunSummary } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import Badge from '../components/ui/Badge';
import WorkflowDag from '../components/WorkflowDag';
import { Plus, Trash2, Rocket, Workflow as WorkflowIcon, RefreshCw, Eye, ChevronDown, ChevronRight } from 'lucide-react';

const EDGE_TYPES: WorkflowEdgeType[] = ['success', 'failure', 'always'];

const statusVariant = (s: string): 'success' | 'error' | 'info' | 'neutral' => {
  if (s === 'successful') return 'success';
  if (s === 'failed' || s === 'error') return 'error';
  if (s === 'running') return 'info';
  return 'neutral';
};

const WorkflowsPage = () => {
  const navigate = useNavigate();
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [runs, setRuns] = useState<WorkflowRunSummary[]>([]);
  const [templates, setTemplates] = useState<any[]>([]);
  const [orgs, setOrgs] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);

  // Builder state
  const [builderOpen, setBuilderOpen] = useState(false);
  const [name, setName] = useState('');
  const [orgId, setOrgId] = useState<number | ''>('');
  const [nodes, setNodes] = useState<WorkflowNode[]>([]);
  const [edges, setEdges] = useState<WorkflowEdge[]>([]);
  const [nodeSeq, setNodeSeq] = useState(1);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  // Webhook trigger (a remote event launches the whole workflow)
  const [whEnabled, setWhEnabled] = useState(false);
  const [whService, setWhService] = useState('generic');
  const [whKey, setWhKey] = useState('');

  // Template preview modal
  const [viewWf, setViewWf] = useState<Workflow | null>(null);

  // Recent runs grouped by workflow (runs arrive newest-first).
  const [collapsed, setCollapsed] = useState<Record<number, boolean>>({});
  const runGroups = useMemo(() => {
    const m = new Map<number, { id: number; name: string; runs: WorkflowRunSummary[] }>();
    for (const r of runs) {
      if (!m.has(r.workflow_template_id)) m.set(r.workflow_template_id, { id: r.workflow_template_id, name: r.template_name, runs: [] });
      m.get(r.workflow_template_id)!.runs.push(r);
    }
    return [...m.values()];
  }, [runs]);
  const toggleGroup = (id: number) => setCollapsed(c => ({ ...c, [id]: !c[id] }));

  const templateName = (id?: number | null) => {
    const t = templates.find(t => t.id === id);
    return t ? t.name : (id ? `template ${id}` : 'no template');
  };
  const orgName = (id: number) => orgs.find(o => o.id === id)?.name || id;

  const load = () => {
    setLoading(true);
    Promise.all([
      api.getWorkflows().catch(() => []),
      api.getWorkflowJobs().catch(() => []),
      api.getTemplates().catch(() => ({})),
      api.getOrganizations().catch(() => ({})),
    ]).then(([wf, rs, tpls, o]) => {
      setWorkflows(wf || []);
      setRuns(rs || []);
      setTemplates(tpls?.items || tpls || []);
      setOrgs(o?.items || o || []);
    }).finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, []);

  // Builder helpers
  const openBuilder = () => {
    setName(''); setOrgId(orgs[0]?.id ?? ''); setNodes([]); setEdges([]); setNodeSeq(1); setError('');
    setWhEnabled(false); setWhService('generic'); setWhKey('');
    setBuilderOpen(true);
  };
  const addNode = () => {
    const key = `n${nodeSeq}`;
    setNodeSeq(s => s + 1);
    setNodes(ns => [...ns, { node_key: key, name: '', node_type: 'job', job_template_id: templates[0]?.id ?? null }]);
  };
  const updateNode = (key: string, patch: Partial<WorkflowNode>) => setNodes(ns => ns.map(n => n.node_key === key ? { ...n, ...patch } : n));
  const removeNode = (key: string) => {
    setNodes(ns => ns.filter(n => n.node_key !== key));
    setEdges(es => es.filter(e => e.parent_key !== key && e.child_key !== key));
  };
  const addEdge = () => {
    if (nodes.length < 2) return;
    setEdges(es => [...es, { parent_key: nodes[0].node_key, child_key: nodes[1].node_key, edge_type: 'success' }]);
  };
  const updateEdge = (i: number, patch: Partial<WorkflowEdge>) => setEdges(es => es.map((e, idx) => idx === i ? { ...e, ...patch } : e));
  const removeEdge = (i: number) => setEdges(es => es.filter((_, idx) => idx !== i));

  const save = async () => {
    setError('');
    if (!name.trim()) return setError('Name is required.');
    if (orgId === '') return setError('Organization is required.');
    if (nodes.length === 0) return setError('Add at least one node.');
    for (const n of nodes) {
      if (!n.name.trim()) return setError('Every node needs a name.');
      if (n.node_type === 'job' && !n.job_template_id) return setError(`Node "${n.name}" needs a job template.`);
      if (n.node_type === 'webhook_out' && !n.webhook_url?.trim()) return setError(`Node "${n.name}" needs a URL to call.`);
    }
    if (whEnabled && !whKey.trim()) return setError('A webhook trigger needs a secret key.');
    for (const e of edges) if (e.parent_key === e.child_key) return setError('An edge cannot connect a node to itself.');
    setSaving(true);
    try {
      await api.createWorkflow({
        organization_id: orgId, name: name.trim(),
        webhook_enabled: whEnabled,
        webhook_service: whEnabled ? whService : '',
        webhook_key: whEnabled ? whKey.trim() : '',
        nodes: nodes.map(n => ({
          node_key: n.node_key, node_type: n.node_type, name: n.name.trim(),
          job_template_id: n.node_type === 'job' ? n.job_template_id : null,
          webhook_url: n.node_type === 'webhook_out' ? (n.webhook_url || '').trim() : '',
          webhook_body: n.node_type === 'webhook_out' ? (n.webhook_body || '') : '',
        })),
        edges,
      });
      setBuilderOpen(false);
      load();
    } catch (e: any) {
      setError(e.message || 'Failed to create workflow.');
    } finally { setSaving(false); }
  };

  const onView = async (wf: Workflow) => {
    try { setViewWf({ ...wf, ...(await api.getWorkflow(wf.id)) }); } catch { /* ignore */ }
  };
  const onDelete = async (wf: Workflow) => {
    if (!confirm(`Delete workflow "${wf.name}"?`)) return;
    try { await api.deleteWorkflow(wf.id); setWorkflows(ws => ws.filter(w => w.id !== wf.id)); } catch { /* ignore */ }
  };
  const onLaunch = async (wf: Workflow) => {
    try {
      const res = await api.launchWorkflow(wf.id);
      navigate(`/workflows/runs/${res.workflow_job_id}`); // go straight to the persistent run page
    } catch (e: any) { alert(e.message || 'Launch failed.'); }
  };

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 flex items-center gap-2"><WorkflowIcon size={24} /> Workflows</h1>
          <p className="text-sm text-gray-500 mt-1">Chain job templates into a DAG with success / failure / always edges, approval gates, and webhook steps — call out to a URL, or pause until a remote event. Trigger whole workflows from inbound webhooks.</p>
        </div>
        <div className="flex gap-2">
          <button onClick={load} disabled={loading} className="text-gray-600 hover:text-gray-900 p-2 rounded-lg hover:bg-gray-100" title="Refresh">
            <RefreshCw size={20} className={loading ? 'animate-spin' : ''} />
          </button>
          <Button icon={<Plus size={16} />} onClick={openBuilder}>New Workflow</Button>
        </div>
      </div>

      {/* Templates */}
      <Card title="Templates" className="overflow-hidden">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
              <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Org</th>
              <th className="px-4 py-2 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100">
            {workflows.map(wf => (
              <tr key={wf.id} className="hover:bg-gray-50 cursor-pointer" onClick={() => onView(wf)}>
                <td className="px-4 py-2 text-sm font-medium text-brand-600 hover:underline">{wf.name}</td>
                <td className="px-4 py-2 text-sm text-gray-500">{orgName(wf.organization_id)}</td>
                <td className="px-4 py-2 text-right space-x-1 whitespace-nowrap" onClick={e => e.stopPropagation()}>
                  <Button variant="ghost" size="sm" icon={<Eye size={14} />} onClick={() => onView(wf)}>View</Button>
                  <Button variant="primary" size="sm" icon={<Rocket size={14} />} onClick={() => onLaunch(wf)}>Launch</Button>
                  <Button variant="ghost" size="sm" icon={<Trash2 size={14} />} onClick={() => onDelete(wf)} />
                </td>
              </tr>
            ))}
            {workflows.length === 0 && !loading && (
              <tr><td colSpan={3} className="px-4 py-6 text-center text-sm text-gray-500">No workflows yet. Create one to chain templates together.</td></tr>
            )}
          </tbody>
        </table>
      </Card>

      {/* Recent runs — grouped by workflow */}
      <Card title="Recent runs" className="overflow-hidden">
        {runs.length === 0 && !loading ? (
          <p className="px-4 py-6 text-center text-sm text-gray-500">No runs yet. Launch a workflow to see it here.</p>
        ) : (
          <div className="divide-y divide-gray-200">
            {runGroups.map(g => {
              const open = !collapsed[g.id];
              const active = g.runs.filter(r => r.status === 'running').length;
              return (
                <div key={g.id}>
                  <button onClick={() => toggleGroup(g.id)} className="w-full flex items-center justify-between px-4 py-3 hover:bg-gray-50 text-left">
                    <div className="flex items-center gap-2 min-w-0">
                      {open ? <ChevronDown size={16} className="text-gray-400" /> : <ChevronRight size={16} className="text-gray-400" />}
                      <span className="text-sm font-semibold text-gray-800 truncate">{g.name}</span>
                      <span className="text-xs text-gray-400 whitespace-nowrap">{g.runs.length} run{g.runs.length !== 1 ? 's' : ''}</span>
                      {active > 0 && <Badge variant="info">{active} running</Badge>}
                    </div>
                    <span className="text-xs text-gray-400 whitespace-nowrap">latest {new Date(g.runs[0].created_at).toLocaleString()}</span>
                  </button>
                  {open && (
                    <table className="min-w-full">
                      <tbody className="divide-y divide-gray-50">
                        {g.runs.map(r => (
                          <tr key={r.id} className="hover:bg-gray-50 cursor-pointer" onClick={() => navigate(`/workflows/runs/${r.id}`)}>
                            <td className="pl-10 pr-4 py-2 text-sm font-medium text-brand-600 hover:underline w-28">#{r.id}</td>
                            <td className="px-4 py-2 text-sm w-40"><Badge variant={statusVariant(r.status)}>{r.status}</Badge></td>
                            <td className="px-4 py-2 text-sm text-gray-500">{r.created_at ? new Date(r.created_at).toLocaleString() : '—'}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </Card>

      {/* Builder */}
      <Modal isOpen={builderOpen} onClose={() => setBuilderOpen(false)} title="New Workflow" size="full">
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Name</label>
              <input value={name} onChange={e => setName(e.target.value)} className="w-full border border-gray-300 rounded-md px-3 py-2 text-sm" placeholder="nightly-deploy" />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Organization</label>
              <select value={orgId} onChange={e => setOrgId(Number(e.target.value))} className="w-full border border-gray-300 rounded-md px-3 py-2 text-sm">
                <option value="">Select…</option>
                {orgs.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}
              </select>
            </div>
          </div>

          {/* Webhook trigger — a remote event launches the whole workflow */}
          <div className="border border-gray-200 rounded-md p-3 bg-gray-50">
            <label className="flex items-center gap-2 text-sm font-medium text-gray-700">
              <input type="checkbox" checked={whEnabled} onChange={e => setWhEnabled(e.target.checked)} />
              Trigger this workflow from an inbound webhook
            </label>
            {whEnabled && (
              <div className="grid grid-cols-2 gap-3 mt-3">
                <div>
                  <label className="block text-xs font-medium text-gray-600 mb-1">Provider</label>
                  <select value={whService} onChange={e => setWhService(e.target.value)} className="w-full border border-gray-300 rounded px-2 py-1 text-sm">
                    <option value="generic">Generic (token)</option>
                    <option value="github">GitHub (HMAC)</option>
                    <option value="gitlab">GitLab (token)</option>
                  </select>
                </div>
                <div>
                  <label className="block text-xs font-medium text-gray-600 mb-1">Secret key</label>
                  <input value={whKey} onChange={e => setWhKey(e.target.value)} placeholder="shared secret" className="w-full border border-gray-300 rounded px-2 py-1 text-sm font-mono" />
                </div>
                <p className="col-span-2 text-[11px] text-gray-500">
                  After saving, POST to <span className="font-mono">/api/v1/webhooks/workflow-templates/&lt;id&gt;/{whService}</span> with this secret to launch a run.
                </p>
              </div>
            )}
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <div className="flex justify-between items-center mb-2">
                <h4 className="text-sm font-semibold text-gray-700">Nodes</h4>
                <Button variant="secondary" size="sm" icon={<Plus size={14} />} onClick={addNode}>Add node</Button>
              </div>
              <div className="space-y-2 max-h-64 overflow-auto pr-1">
                {nodes.map(n => (
                  <div key={n.node_key} className="bg-gray-50 rounded-md p-2 border border-gray-200 space-y-2">
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-mono text-gray-400 w-8">{n.node_key}</span>
                      <input value={n.name} onChange={e => updateNode(n.node_key, { name: e.target.value })} placeholder="name" className="flex-1 border border-gray-300 rounded px-2 py-1 text-sm min-w-0" />
                      <select value={n.node_type} onChange={e => updateNode(n.node_key, { node_type: e.target.value as WorkflowNodeType })} className="border border-gray-300 rounded px-1 py-1 text-xs">
                        <option value="job">job</option>
                        <option value="approval">approval</option>
                        <option value="webhook_in">webhook in (wait)</option>
                        <option value="webhook_out">webhook out (call)</option>
                      </select>
                      {n.node_type === 'job' && (
                        <select value={n.job_template_id ?? ''} onChange={e => updateNode(n.node_key, { job_template_id: Number(e.target.value) })} className="border border-gray-300 rounded px-1 py-1 text-xs max-w-[120px]">
                          <option value="">template…</option>
                          {templates.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
                        </select>
                      )}
                      <button onClick={() => removeNode(n.node_key)} className="text-gray-400 hover:text-red-600"><Trash2 size={14} /></button>
                    </div>
                    {n.node_type === 'webhook_out' && (
                      <input value={n.webhook_url || ''} onChange={e => updateNode(n.node_key, { webhook_url: e.target.value })}
                        placeholder="https://example.com/hook  (URL to POST)" className="w-full border border-gray-300 rounded px-2 py-1 text-xs font-mono" />
                    )}
                    {n.node_type === 'webhook_in' && (
                      <p className="text-[11px] text-purple-700 pl-10">Pauses here until an external system POSTs the node's callback URL (shown on the run page).</p>
                    )}
                  </div>
                ))}
                {nodes.length === 0 && <p className="text-xs text-gray-400 italic">No nodes yet.</p>}
              </div>
            </div>

            <div>
              <div className="flex justify-between items-center mb-2">
                <h4 className="text-sm font-semibold text-gray-700">Edges</h4>
                <Button variant="secondary" size="sm" icon={<Plus size={14} />} onClick={addEdge} disabled={nodes.length < 2}>Add edge</Button>
              </div>
              <div className="space-y-2 max-h-64 overflow-auto pr-1">
                {edges.map((e, i) => (
                  <div key={i} className="flex items-center gap-1 bg-gray-50 rounded-md p-2 border border-gray-200 text-xs">
                    <select value={e.parent_key} onChange={ev => updateEdge(i, { parent_key: ev.target.value })} className="border border-gray-300 rounded px-1 py-1 flex-1 min-w-0">
                      {nodes.map(n => <option key={n.node_key} value={n.node_key}>{n.name || n.node_key}</option>)}
                    </select>
                    <select value={e.edge_type} onChange={ev => updateEdge(i, { edge_type: ev.target.value as WorkflowEdgeType })} className="border border-gray-300 rounded px-1 py-1">
                      {EDGE_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
                    </select>
                    <span className="text-gray-400">→</span>
                    <select value={e.child_key} onChange={ev => updateEdge(i, { child_key: ev.target.value })} className="border border-gray-300 rounded px-1 py-1 flex-1 min-w-0">
                      {nodes.map(n => <option key={n.node_key} value={n.node_key}>{n.name || n.node_key}</option>)}
                    </select>
                    <button onClick={() => removeEdge(i)} className="text-gray-400 hover:text-red-600"><Trash2 size={14} /></button>
                  </div>
                ))}
                {edges.length === 0 && <p className="text-xs text-gray-400 italic">No edges — nodes will all start at once.</p>}
              </div>
            </div>
          </div>

          <div>
            <h4 className="text-sm font-semibold text-gray-700 mb-2">Preview</h4>
            <WorkflowDag nodes={nodes} edges={edges} templateName={templateName} />
          </div>

          {error && <p className="text-sm text-red-600">{error}</p>}
          <div className="flex justify-end gap-2 pt-2 border-t border-gray-100">
            <Button variant="secondary" onClick={() => setBuilderOpen(false)}>Cancel</Button>
            <Button onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Create workflow'}</Button>
          </div>
        </div>
      </Modal>

      {/* Template preview */}
      <Modal isOpen={!!viewWf} onClose={() => setViewWf(null)} title={viewWf ? `Workflow: ${viewWf.name}` : ''} size="full">
        {viewWf && (
          <div className="space-y-4">
            {viewWf.webhook_enabled && (
              <div className="text-xs bg-cyan-50 border border-cyan-200 rounded-md px-3 py-2 text-cyan-900">
                <b>Webhook trigger enabled</b> ({viewWf.webhook_service || 'generic'}). POST to{' '}
                <span className="font-mono">/api/v1/webhooks/workflow-templates/{viewWf.id}/{viewWf.webhook_service || 'generic'}</span>{' '}
                with the configured secret to launch a run.
              </div>
            )}
            <WorkflowDag nodes={viewWf.nodes || []} edges={viewWf.edges || []} templateName={templateName} />
            <div className="flex justify-end">
              <Button icon={<Rocket size={16} />} onClick={() => { const w = viewWf; setViewWf(null); if (w) onLaunch(w); }}>Launch</Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default WorkflowsPage;
