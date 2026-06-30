import React, { useEffect, useMemo, useState } from 'react';
import { api } from '../services/api';
import { Workflow, WorkflowNode, WorkflowEdge, WorkflowJob, WorkflowNodeType, WorkflowEdgeType } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import Badge from '../components/ui/Badge';
import { Plus, Trash2, Rocket, GitBranch, RefreshCw, Check, X, Eye } from 'lucide-react';

// ---------------------------------------------------------------------------
// Layered DAG layout (no external graph library): assign each node a column by
// its longest path from a root, stack nodes within a column, and draw edges as
// curves colored by edge type. Used for both the builder preview and run view.
// ---------------------------------------------------------------------------
const NODE_W = 168;
const NODE_H = 52;
const GAP_X = 56;
const GAP_Y = 26;
const MARGIN = 16;

interface Placed extends WorkflowNode { x: number; y: number; }

function layoutDag(nodes: WorkflowNode[], edges: WorkflowEdge[]) {
  const byKey = new Map(nodes.map(n => [n.node_key, n]));
  const depth = new Map<string, number>(nodes.map(n => [n.node_key, 0]));
  // Relax longest-path depths; cap iterations so a stray cycle can't loop forever.
  for (let i = 0; i < nodes.length; i++) {
    let changed = false;
    for (const e of edges) {
      if (!byKey.has(e.parent_key) || !byKey.has(e.child_key)) continue;
      const d = (depth.get(e.parent_key) ?? 0) + 1;
      if (d > (depth.get(e.child_key) ?? 0)) { depth.set(e.child_key, d); changed = true; }
    }
    if (!changed) break;
  }
  const columns = new Map<number, string[]>();
  for (const n of nodes) {
    const d = depth.get(n.node_key) ?? 0;
    if (!columns.has(d)) columns.set(d, []);
    columns.get(d)!.push(n.node_key);
  }
  const placed = new Map<string, Placed>();
  let maxRows = 0;
  for (const [d, keys] of columns) {
    maxRows = Math.max(maxRows, keys.length);
    keys.forEach((k, row) => {
      placed.set(k, {
        ...byKey.get(k)!,
        x: MARGIN + d * (NODE_W + GAP_X),
        y: MARGIN + row * (NODE_H + GAP_Y),
      });
    });
  }
  const cols = Math.max(1, columns.size);
  const width = MARGIN * 2 + cols * NODE_W + (cols - 1) * GAP_X;
  const height = MARGIN * 2 + Math.max(1, maxRows) * NODE_H + Math.max(0, maxRows - 1) * GAP_Y;
  return { placed, width, height };
}

const EDGE_COLOR: Record<WorkflowEdgeType, string> = {
  success: '#16a34a',
  failure: '#dc2626',
  always: '#6b7280',
};

// Map a run-time node status to a fill + badge tone.
function statusFill(status?: string): { fill: string; stroke: string; text: string } {
  switch (status) {
    case 'successful':
    case 'approved': return { fill: '#dcfce7', stroke: '#16a34a', text: '#166534' };
    case 'failed':
    case 'error':
    case 'lost':
    case 'rejected': return { fill: '#fee2e2', stroke: '#dc2626', text: '#991b1b' };
    case 'running': return { fill: '#dbeafe', stroke: '#2563eb', text: '#1e40af' };
    case 'awaiting_approval': return { fill: '#fef3c7', stroke: '#d97706', text: '#92400e' };
    case 'skipped': return { fill: '#f1f5f9', stroke: '#94a3b8', text: '#475569' };
    case 'pending': return { fill: '#f8fafc', stroke: '#cbd5e1', text: '#64748b' };
    default: return { fill: '#ffffff', stroke: '#cbd5e1', text: '#334155' };
  }
}

interface DagViewProps {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  statusByKey?: Record<string, string>; // run view: node_key -> status
  templateName?: (id?: number | null) => string;
}

const DagView: React.FC<DagViewProps> = ({ nodes, edges, statusByKey, templateName }) => {
  const { placed, width, height } = useMemo(() => layoutDag(nodes, edges), [nodes, edges]);
  if (nodes.length === 0) {
    return <div className="text-sm text-gray-400 italic py-8 text-center">Add nodes to see the graph.</div>;
  }
  return (
    <div className="overflow-auto border border-gray-200 rounded-md bg-gray-50">
      <svg width={width} height={height} className="block">
        <defs>
          {Object.entries(EDGE_COLOR).map(([k, c]) => (
            <marker key={k} id={`arrow-${k}`} markerWidth="8" markerHeight="8" refX="7" refY="3" orient="auto">
              <path d="M0,0 L7,3 L0,6 Z" fill={c} />
            </marker>
          ))}
        </defs>
        {edges.map((e, i) => {
          const p = placed.get(e.parent_key); const c = placed.get(e.child_key);
          if (!p || !c) return null;
          const x1 = p.x + NODE_W, y1 = p.y + NODE_H / 2;
          const x2 = c.x, y2 = c.y + NODE_H / 2;
          const mx = (x1 + x2) / 2;
          return (
            <path key={i} d={`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`}
              fill="none" stroke={EDGE_COLOR[e.edge_type]} strokeWidth={2}
              markerEnd={`url(#arrow-${e.edge_type})`} />
          );
        })}
        {Array.from(placed.values()).map(n => {
          const st = statusByKey?.[n.node_key];
          const tone = statusByKey ? statusFill(st) : (n.node_type === 'approval'
            ? { fill: '#fef3c7', stroke: '#d97706', text: '#92400e' }
            : { fill: '#eef2ff', stroke: '#6366f1', text: '#3730a3' });
          const sub = n.node_type === 'approval'
            ? (st || 'approval')
            : (statusByKey ? (st || 'pending') : (templateName ? templateName(n.job_template_id) : 'job'));
          return (
            <g key={n.node_key}>
              <rect x={n.x} y={n.y} width={NODE_W} height={NODE_H} rx={8}
                fill={tone.fill} stroke={tone.stroke} strokeWidth={1.5} />
              <text x={n.x + 10} y={n.y + 21} fontSize={13} fontWeight={600} fill={tone.text}>
                {n.name.length > 20 ? n.name.slice(0, 19) + '…' : n.name || n.node_key}
              </text>
              <text x={n.x + 10} y={n.y + 39} fontSize={11} fill={tone.text} opacity={0.8}>
                {n.node_type === 'approval' ? '⏸ ' : '▶ '}{String(sub).length > 22 ? String(sub).slice(0, 21) + '…' : sub}
              </text>
            </g>
          );
        })}
      </svg>
    </div>
  );
};

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------
const EDGE_TYPES: WorkflowEdgeType[] = ['success', 'failure', 'always'];

const WorkflowsPage = () => {
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [templates, setTemplates] = useState<any[]>([]);
  const [orgs, setOrgs] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // Builder state
  const [builderOpen, setBuilderOpen] = useState(false);
  const [name, setName] = useState('');
  const [orgId, setOrgId] = useState<number | ''>('');
  const [nodes, setNodes] = useState<WorkflowNode[]>([]);
  const [edges, setEdges] = useState<WorkflowEdge[]>([]);
  const [nodeSeq, setNodeSeq] = useState(1);
  const [saving, setSaving] = useState(false);

  // View / run state
  const [viewWf, setViewWf] = useState<Workflow | null>(null);
  const [runJobId, setRunJobId] = useState<number | null>(null);
  const [runWf, setRunWf] = useState<Workflow | null>(null);
  const [runJob, setRunJob] = useState<WorkflowJob | null>(null);

  const templateName = (id?: number | null) => {
    const t = templates.find(t => t.id === id);
    return t ? t.name : (id ? `template ${id}` : 'no template');
  };

  const load = () => {
    setLoading(true);
    Promise.all([
      api.getWorkflows().catch(() => []),
      api.getTemplates().catch(() => ({})),
      api.getOrganizations().catch(() => ({})),
    ]).then(([wf, tpls, o]) => {
      setWorkflows(wf || []);
      setTemplates(tpls?.items || tpls || []);
      setOrgs(o?.items || o || []);
    }).finally(() => setLoading(false));
  };
  useEffect(() => { load(); }, []);

  // Poll the running workflow job.
  useEffect(() => {
    if (runJobId == null) return;
    let active = true;
    const tick = () => api.getWorkflowJob(runJobId).then(j => { if (active) setRunJob(j); }).catch(() => { });
    tick();
    const h = setInterval(tick, 2000);
    return () => { active = false; clearInterval(h); };
  }, [runJobId]);

  const openBuilder = () => {
    setName(''); setOrgId(orgs[0]?.id ?? ''); setNodes([]); setEdges([]); setNodeSeq(1); setError('');
    setBuilderOpen(true);
  };

  const addNode = () => {
    const key = `n${nodeSeq}`;
    setNodeSeq(s => s + 1);
    setNodes(ns => [...ns, { node_key: key, name: '', node_type: 'job', job_template_id: templates[0]?.id ?? null }]);
  };
  const updateNode = (key: string, patch: Partial<WorkflowNode>) =>
    setNodes(ns => ns.map(n => n.node_key === key ? { ...n, ...patch } : n));
  const removeNode = (key: string) => {
    setNodes(ns => ns.filter(n => n.node_key !== key));
    setEdges(es => es.filter(e => e.parent_key !== key && e.child_key !== key));
  };
  const addEdge = () => {
    if (nodes.length < 2) return;
    setEdges(es => [...es, { parent_key: nodes[0].node_key, child_key: nodes[1].node_key, edge_type: 'success' }]);
  };
  const updateEdge = (i: number, patch: Partial<WorkflowEdge>) =>
    setEdges(es => es.map((e, idx) => idx === i ? { ...e, ...patch } : e));
  const removeEdge = (i: number) => setEdges(es => es.filter((_, idx) => idx !== i));

  const save = async () => {
    setError('');
    if (!name.trim()) return setError('Name is required.');
    if (orgId === '') return setError('Organization is required.');
    if (nodes.length === 0) return setError('Add at least one node.');
    for (const n of nodes) {
      if (!n.name.trim()) return setError('Every node needs a name.');
      if (n.node_type === 'job' && !n.job_template_id) return setError(`Node "${n.name}" needs a job template.`);
    }
    for (const e of edges) {
      if (e.parent_key === e.child_key) return setError('An edge cannot connect a node to itself.');
    }
    setSaving(true);
    try {
      await api.createWorkflow({
        organization_id: orgId,
        name: name.trim(),
        nodes: nodes.map(n => ({
          node_key: n.node_key,
          node_type: n.node_type,
          name: n.name.trim(),
          job_template_id: n.node_type === 'job' ? n.job_template_id : null,
        })),
        edges,
      });
      setBuilderOpen(false);
      load();
    } catch (e: any) {
      setError(e.message || 'Failed to create workflow.');
    } finally {
      setSaving(false);
    }
  };

  const onView = async (wf: Workflow) => {
    // The detail endpoint returns nodes/edges but not name; keep the list row's
    // name so the modal title reads correctly.
    try { setViewWf({ ...wf, ...(await api.getWorkflow(wf.id)) }); } catch { /* ignore */ }
  };

  const onDelete = async (wf: Workflow) => {
    if (!confirm(`Delete workflow "${wf.name}"?`)) return;
    try { await api.deleteWorkflow(wf.id); setWorkflows(ws => ws.filter(w => w.id !== wf.id)); } catch { /* ignore */ }
  };

  const onLaunch = async (wf: Workflow) => {
    try {
      const detail = await api.getWorkflow(wf.id);
      const res = await api.launchWorkflow(wf.id);
      setRunWf(detail);
      setRunJob(null);
      setRunJobId(res.workflow_job_id);
    } catch (e: any) {
      alert(e.message || 'Launch failed.');
    }
  };

  const approve = async (nodeId: number, ok: boolean) => {
    try {
      await (ok ? api.approveWorkflowNode(nodeId) : api.denyWorkflowNode(nodeId));
      if (runJobId != null) api.getWorkflowJob(runJobId).then(setRunJob).catch(() => { });
    } catch (e: any) { alert(e.message || 'Approval failed.'); }
  };

  const runStatusByKey: Record<string, string> = {};
  (runJob?.nodes || []).forEach(n => { runStatusByKey[n.node_key] = n.status; });

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 flex items-center gap-2">
            <GitBranch size={24} /> Workflows
          </h1>
          <p className="text-sm text-gray-500 mt-1">Chain job templates into a DAG with success / failure / always edges and manual approval gates.</p>
        </div>
        <div className="flex gap-2">
          <button onClick={load} disabled={loading} className="text-gray-600 hover:text-gray-900 p-2 rounded-lg hover:bg-gray-100" title="Refresh">
            <RefreshCw size={20} className={loading ? 'animate-spin' : ''} />
          </button>
          <Button icon={<Plus size={16} />} onClick={openBuilder}>New Workflow</Button>
        </div>
      </div>

      <Card className="overflow-hidden">
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
                <td className="px-4 py-2 text-sm text-gray-500">{orgs.find(o => o.id === wf.organization_id)?.name || wf.organization_id}</td>
                <td className="px-4 py-2 text-right space-x-1 whitespace-nowrap" onClick={e => e.stopPropagation()}>
                  <Button variant="ghost" size="sm" icon={<Eye size={14} />} onClick={() => onView(wf)}>View</Button>
                  <Button variant="secondary" size="sm" icon={<Rocket size={14} />} onClick={() => onLaunch(wf)}>Launch</Button>
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

          <div className="grid grid-cols-2 gap-4">
            {/* Nodes */}
            <div>
              <div className="flex justify-between items-center mb-2">
                <h4 className="text-sm font-semibold text-gray-700">Nodes</h4>
                <Button variant="secondary" size="sm" icon={<Plus size={14} />} onClick={addNode}>Add node</Button>
              </div>
              <div className="space-y-2 max-h-64 overflow-auto pr-1">
                {nodes.map(n => (
                  <div key={n.node_key} className="flex items-center gap-2 bg-gray-50 rounded-md p-2 border border-gray-200">
                    <span className="text-xs font-mono text-gray-400 w-8">{n.node_key}</span>
                    <input value={n.name} onChange={e => updateNode(n.node_key, { name: e.target.value })} placeholder="name" className="flex-1 border border-gray-300 rounded px-2 py-1 text-sm min-w-0" />
                    <select value={n.node_type} onChange={e => updateNode(n.node_key, { node_type: e.target.value as WorkflowNodeType })} className="border border-gray-300 rounded px-1 py-1 text-xs">
                      <option value="job">job</option>
                      <option value="approval">approval</option>
                    </select>
                    {n.node_type === 'job' && (
                      <select value={n.job_template_id ?? ''} onChange={e => updateNode(n.node_key, { job_template_id: Number(e.target.value) })} className="border border-gray-300 rounded px-1 py-1 text-xs max-w-[120px]">
                        <option value="">template…</option>
                        {templates.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
                      </select>
                    )}
                    <button onClick={() => removeNode(n.node_key)} className="text-gray-400 hover:text-red-600"><Trash2 size={14} /></button>
                  </div>
                ))}
                {nodes.length === 0 && <p className="text-xs text-gray-400 italic">No nodes yet.</p>}
              </div>
            </div>

            {/* Edges */}
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
            <DagView nodes={nodes} edges={edges} templateName={templateName} />
          </div>

          {error && <p className="text-sm text-red-600">{error}</p>}
          <div className="flex justify-end gap-2 pt-2 border-t border-gray-100">
            <Button variant="secondary" onClick={() => setBuilderOpen(false)}>Cancel</Button>
            <Button onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Create workflow'}</Button>
          </div>
        </div>
      </Modal>

      {/* Read-only view */}
      <Modal isOpen={!!viewWf} onClose={() => setViewWf(null)} title={viewWf ? `Workflow: ${viewWf.name}` : ''} size="full">
        {viewWf && <DagView nodes={viewWf.nodes || []} edges={viewWf.edges || []} templateName={templateName} />}
      </Modal>

      {/* Run view */}
      <Modal isOpen={runJobId != null} onClose={() => { setRunJobId(null); setRunWf(null); setRunJob(null); }}
        title="Workflow Run" size="full">
        <div className="space-y-4">
          <div className="flex items-center gap-3">
            <span className="text-sm text-gray-500">Status:</span>
            <Badge variant={runJob?.status === 'successful' ? 'success' : runJob?.status === 'failed' ? 'error' : 'info'}>
              {runJob?.status || 'launching…'}
            </Badge>
            <span className="text-xs text-gray-400">job #{runJobId}</span>
          </div>
          {runWf && <DagView nodes={runWf.nodes || []} edges={runWf.edges || []} statusByKey={runStatusByKey} />}
          {/* Approval gates awaiting a decision */}
          {(runJob?.nodes || []).filter(n => n.status === 'awaiting_approval').map(n => {
            const def = runWf?.nodes?.find(x => x.node_key === n.node_key);
            return (
              <div key={n.id} className="flex items-center justify-between bg-amber-50 border border-amber-200 rounded-md p-3">
                <span className="text-sm text-amber-900">⏸ Approval required: <b>{def?.name || n.node_key}</b></span>
                <div className="flex gap-2">
                  <Button variant="primary" size="sm" icon={<Check size={14} />} onClick={() => approve(n.id, true)}>Approve</Button>
                  <Button variant="danger" size="sm" icon={<X size={14} />} onClick={() => approve(n.id, false)}>Deny</Button>
                </div>
              </div>
            );
          })}
        </div>
      </Modal>
    </div>
  );
};

export default WorkflowsPage;
