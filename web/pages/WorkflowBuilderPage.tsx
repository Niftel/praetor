import React, { useEffect, useMemo, useRef, useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { WorkflowNode, WorkflowEdge, WorkflowNodeType, WorkflowEdgeType } from '../types';
import { Input } from '../components/ui/Input';
import Button from '../components/ui/Button';
import { toast } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';
import { useCapabilities } from '../lib/useCapabilities';
import {
  ArrowLeft, Check, Play, Pause, ArrowDownToLine, ArrowUpFromLine, Plus, Trash2,
  Minus, Maximize2, GitFork, ChevronRight, ChevronDown,
} from 'lucide-react';

const NODE_W = 158, NODE_H = 62, COL_GAP = 66, ROW_GAP = 26, MARGIN = 40;
const EDGE_TYPES: WorkflowEdgeType[] = ['success', 'failure', 'always'];
const EDGE_COLOR = { success: '#3ad07f', failure: '#f2685f', always: '#565f70' };

// Definition-mode node tint by type (no live status in the builder).
const TYPE_TONE: Record<string, { led: string; icon: string; border: string }> = {
  job: { led: 'bg-run', icon: 'text-run', border: 'border-run/40' },
  approval: { led: 'bg-changed', icon: 'text-changed', border: 'border-changed/40' },
  webhook_in: { led: 'bg-acc', icon: 'text-acc2', border: 'border-acc/40' },
  webhook_out: { led: 'bg-violet', icon: 'text-violet', border: 'border-violet/40' },
};
const nodeIcon = (t: WorkflowNodeType) =>
  t === 'approval' ? Pause : t === 'webhook_in' ? ArrowDownToLine : t === 'webhook_out' ? ArrowUpFromLine : Play;

interface Placed { node: WorkflowNode; x: number; y: number; }
function layout(nodes: WorkflowNode[], edges: WorkflowEdge[]) {
  const byKey = new Map(nodes.map(n => [n.node_key, n]));
  const depth = new Map<string, number>(nodes.map(n => [n.node_key, 0]));
  for (let i = 0; i < nodes.length; i++) {
    let changed = false;
    for (const e of edges) {
      if (!byKey.has(e.parent_key) || !byKey.has(e.child_key)) continue;
      const d = (depth.get(e.parent_key) ?? 0) + 1;
      if (d > (depth.get(e.child_key) ?? 0)) { depth.set(e.child_key, d); changed = true; }
    }
    if (!changed) break;
  }
  const cols = new Map<number, string[]>();
  for (const n of nodes) { const d = depth.get(n.node_key) ?? 0; if (!cols.has(d)) cols.set(d, []); cols.get(d)!.push(n.node_key); }
  const placed = new Map<string, Placed>();
  let maxRows = 0;
  for (const [d, keys] of cols) {
    maxRows = Math.max(maxRows, keys.length);
    keys.forEach((k, row) => placed.set(k, { node: byKey.get(k)!, x: MARGIN + d * (NODE_W + COL_GAP), y: MARGIN + row * (NODE_H + ROW_GAP) }));
  }
  const width = MARGIN * 2 + Math.max(1, cols.size) * NODE_W + Math.max(0, cols.size - 1) * COL_GAP;
  const height = MARGIN * 2 + Math.max(1, maxRows) * NODE_H + Math.max(0, maxRows - 1) * ROW_GAP;
  return { placed, width, height };
}

const Toggle: React.FC<{ on: boolean; onChange: (v: boolean) => void }> = ({ on, onChange }) => (
  <button type="button" onClick={() => onChange(!on)} className={`relative w-9 h-[21px] rounded-full shrink-0 transition-colors ${on ? 'bg-acc' : 'bg-line2'}`}>
    <span className={`absolute top-[2.5px] w-4 h-4 rounded-full transition-transform ${on ? 'translate-x-[15px] bg-[#06231e]' : 'translate-x-[2.5px] bg-[#c3c9d4]'}`} />
  </button>
);

const psel = 'w-full bg-panel border border-line2 rounded-md px-2.5 h-8 text-[12.5px] text-ink2 font-mono outline-none focus:border-acc/60';

const WorkflowBuilderPage = () => {
  const { orgId: orgIdStr, workflowId } = useParams();
  const orgId = Number(orgIdStr);
  const editingId = workflowId ? Number(workflowId) : null;
  const navigate = useNavigate();
  const { capabilities: orgCapabilities, loading: orgCapabilityLoading } = useCapabilities('organization', editingId ? null : orgId);
  const { capabilities: workflowCapabilities, loading: workflowCapabilityLoading } = useCapabilities('workflow_template', editingId);

  const [orgName, setOrgName] = useState('');
  const [templates, setTemplates] = useState<any[]>([]);
  const [name, setName] = useState('');
  const [nodes, setNodes] = useState<WorkflowNode[]>([]);
  const [edges, setEdges] = useState<WorkflowEdge[]>([]);
  const [nodeSeq, setNodeSeq] = useState(1);
  const [whEnabled, setWhEnabled] = useState(false);
  const [whService, setWhService] = useState('generic');
  const [whKey, setWhKey] = useState('');
  const [allowSim, setAllowSim] = useState(false);
  const [selKey, setSelKey] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  // After a failed save attempt, highlight the specific fields that need
  // attention (reactively — a highlight clears as soon as its field is valid).
  const [showErrors, setShowErrors] = useState(false);
  const [showEdges, setShowEdges] = useState(true);

  // Fade the IDE in on mount, and out before leaving — so opening/closing the
  // builder is a soft transition rather than an abrupt route swap. (Under
  // prefers-reduced-motion the global CSS collapses the transition to instant.)
  const [entered, setEntered] = useState(false);
  useEffect(() => { const r = requestAnimationFrame(() => setEntered(true)); return () => cancelAnimationFrame(r); }, []);
  const leave = (to: string) => { setEntered(false); setTimeout(() => navigate(to), 200); };

  // pan/zoom
  const [scale, setScale] = useState(1);
  const [tx, setTx] = useState(0);
  const [ty, setTy] = useState(0);
  const viewRef = useRef<HTMLDivElement>(null);
  const pan = useRef<{ x: number; y: number; tx: number; ty: number } | null>(null);
  const fitted = useRef(false);

  useEffect(() => {
    api.getTemplates().then(t => setTemplates(unwrap(t).filter((x: any) => x.organization_id === orgId))).catch(() => { });
    api.getOrganizations().then(o => setOrgName(unwrap<{ id: number; name: string }>(o).find(x => x.id === orgId)?.name ?? `Org ${orgId}`)).catch(() => setOrgName(`Org ${orgId}`));
  }, [orgId]);

  useEffect(() => {
    if (!editingId) return;
    api.getWorkflow(editingId).then(full => {
      setName(full.name ?? '');
      const ns: WorkflowNode[] = (full.nodes || []).map((n: any) => ({
        node_key: n.node_key, node_type: n.node_type, name: n.name || '',
        job_template_id: n.job_template_id ?? null, webhook_url: n.webhook_url || '', webhook_body: n.webhook_body || '',
      }));
      setNodes(ns);
      setEdges(full.edges || []);
      const maxN = ns.reduce((m, n) => { const x = /^n(\d+)$/.exec(n.node_key); return x ? Math.max(m, +x[1]) : m; }, 0);
      setNodeSeq(maxN + 1);
      setWhEnabled(!!full.webhook_enabled); setWhService(full.webhook_service || 'generic'); setAllowSim(!!full.allow_simultaneous);
    }).catch((e: any) => toast.error(e.message || 'Failed to load workflow'));
  }, [editingId]);

  const { placed, width, height } = useMemo(() => layout(nodes, edges), [nodes, edges]);

  const fit = useCallback(() => {
    const v = viewRef.current; if (!v || width === 0) return;
    const sc = Math.max(0.4, Math.min(1.1, (v.clientWidth - 48) / width, (v.clientHeight - 48) / height));
    setScale(sc); setTx((v.clientWidth - width * sc) / 2); setTy((v.clientHeight - height * sc) / 2);
  }, [width, height]);
  useEffect(() => { if (!fitted.current && nodes.length && viewRef.current) { fit(); fitted.current = true; } }, [nodes.length, fit]);

  const zoom = (f: number) => setScale(s => Math.min(1.8, Math.max(0.4, s * f)));
  const onWheel = (e: React.WheelEvent) => { if (e.ctrlKey || e.metaKey) { e.preventDefault(); zoom(e.deltaY < 0 ? 1.1 : 0.9); } };
  const onDown = (e: React.MouseEvent) => { pan.current = { x: e.clientX, y: e.clientY, tx, ty }; };
  const onMove = (e: React.MouseEvent) => { const p = pan.current; if (!p) return; setTx(p.tx + (e.clientX - p.x)); setTy(p.ty + (e.clientY - p.y)); };
  const onUp = () => { pan.current = null; };

  const addNode = () => {
    const key = `n${nodeSeq}`;
    setNodeSeq(s => s + 1);
    setNodes(ns => [...ns, { node_key: key, name: '', node_type: 'job', job_template_id: templates[0]?.id ?? null }]);
    setSelKey(key);
  };
  const updateNode = (key: string, patch: Partial<WorkflowNode>) => setNodes(ns => ns.map(n => n.node_key === key ? { ...n, ...patch } : n));
  const removeNode = (key: string) => { setNodes(ns => ns.filter(n => n.node_key !== key)); setEdges(es => es.filter(e => e.parent_key !== key && e.child_key !== key)); if (selKey === key) setSelKey(null); };
  const addEdge = () => { if (nodes.length < 2) return; setEdges(es => [...es, { parent_key: nodes[0].node_key, child_key: nodes[1].node_key, edge_type: 'success' }]); };
  const updateEdge = (i: number, patch: Partial<WorkflowEdge>) => setEdges(es => es.map((e, idx) => idx === i ? { ...e, ...patch } : e));
  const removeEdge = (i: number) => setEdges(es => es.filter((_, idx) => idx !== i));

  const templateName = (nid?: number | null) => templates.find(t => t.id === nid)?.name;
  const nodeLabel = (k: string) => { const n = nodes.find(x => x.node_key === k); return n?.name || k; };

    // Which fields are incomplete — drives the inline highlighting.
  const nodeBad = (n: WorkflowNode) => !n.name.trim()
    || (n.node_type === 'job' && !n.job_template_id)
    || (n.node_type === 'webhook_out' && !n.webhook_url?.trim());
  const edgeBad = (e: WorkflowEdge) => e.parent_key === e.child_key;
  const nameBad = showErrors && !name.trim();
  const whKeyBad = showErrors && whEnabled && !whKey.trim() && !editingId;

  const save = async () => {
    const badNode = nodes.find(nodeBad);
    const invalid = !name.trim() || nodes.length === 0 || !!badNode
      || (whEnabled && !whKey.trim() && !editingId) || edges.some(edgeBad);
    if (invalid) {
      setShowErrors(true);
      if (badNode) setSelKey(badNode.node_key); // open the offending node's editor
      if (nodes.length === 0) toast.info('Add at least one node to save.');
      return;
    }
    setSaving(true);
    const payload = {
      organization_id: orgId, name: name.trim(),
      webhook_enabled: whEnabled, webhook_service: whEnabled ? whService : '', webhook_key: whEnabled ? whKey.trim() : '',
      allow_simultaneous: allowSim,
      nodes: nodes.map(n => ({
        node_key: n.node_key, node_type: n.node_type, name: n.name.trim(),
        job_template_id: n.node_type === 'job' ? n.job_template_id : null,
        webhook_url: n.node_type === 'webhook_out' ? (n.webhook_url || '').trim() : '',
        webhook_body: n.node_type === 'webhook_out' ? (n.webhook_body || '') : '',
      })),
      edges,
    };
    try {
      if (editingId) await api.updateWorkflow(editingId, payload); else await api.createWorkflow(payload);
      toast.success(editingId ? 'Workflow saved' : 'Workflow created');
      leave(`/workflows/org/${orgId}`);
    } catch (e: any) { toast.error(e.message || `Failed to ${editingId ? 'update' : 'create'} workflow.`); }
    finally { setSaving(false); }
  };

  const selected = selKey ? nodes.find(n => n.node_key === selKey) || null : null;

  const permissionLoading = editingId ? workflowCapabilityLoading : orgCapabilityLoading;
  const permitted = editingId ? workflowCapabilities.manage : !!orgCapabilities.add_workflow_template;
  if (permissionLoading) return <PageSpinner />;
  if (!permitted) {
    return (
      <div className="h-full grid place-items-center bg-bg text-ink px-6">
        <div className="max-w-md text-center">
          <h1 className="text-xl font-semibold">Read-only access</h1>
          <p className="mt-2 text-sm text-mut">You can inspect this workflow from its organization page, but your role cannot create or edit workflows.</p>
          <Button className="mt-5" variant="secondary" onClick={() => navigate(`/workflows/org/${orgId}`)}>Back to workflows</Button>
        </div>
      </div>
    );
  }

  return (
    <div className={`flex flex-col h-full min-h-0 bg-bg text-ink transition-opacity duration-200 ease-out ${entered ? 'opacity-100' : 'opacity-0'}`}>
      {/* Identity bar */}
      <div className="flex items-center gap-4 px-5 py-3 border-b border-line shrink-0">
        <button onClick={() => leave(`/workflows/org/${orgId}`)} className="w-7 h-7 grid place-items-center rounded-md border border-line2 text-mut hover:text-ink hover:border-white/20" title="Back to workflows"><ArrowLeft size={15} /></button>
        <GitFork size={17} className="text-acc2 shrink-0" />
        <div className="flex flex-col">
          <input value={name} onChange={e => setName(e.target.value)} placeholder="Workflow name"
            className={`text-[16px] font-semibold tracking-tight bg-transparent border-b pb-0.5 outline-none w-[min(360px,40vw)] ${nameBad ? 'border-err' : 'border-transparent hover:border-line focus:border-acc'}`} />
          {nameBad && <span className="text-[11px] text-err mt-1">Give the workflow a name.</span>}
        </div>
        <span className="font-mono text-[11px] text-dim">{orgName} · {nodes.length} nodes · {edges.length} edges</span>
        <div className="ml-auto flex items-center gap-2.5">
          <button onClick={() => leave(`/workflows/org/${orgId}`)} className="h-[32px] px-3.5 rounded-md text-[12.5px] font-medium border border-line2 text-ink2 hover:border-white/25">Cancel</button>
          <Button onClick={save} disabled={saving} icon={<Check size={14} />}>{saving ? 'Saving…' : editingId ? 'Save' : 'Create'}</Button>
        </div>
      </div>

      <div className="grid grid-cols-[minmax(0,1fr)_360px] flex-1 min-h-0 max-[720px]:grid-cols-1 max-[720px]:grid-rows-[minmax(260px,1fr)_minmax(220px,42%)]">
        {/* Canvas workspace */}
        <div ref={viewRef} onWheel={onWheel} onMouseDown={onDown} onMouseMove={onMove} onMouseUp={onUp} onMouseLeave={onUp}
          className="relative overflow-hidden cursor-grab active:cursor-grabbing"
          style={{ background: '#060708', backgroundImage: 'radial-gradient(rgba(255,255,255,.05) 1px, transparent 1px)', backgroundSize: '22px 22px' }}>
          <div className="absolute top-0 left-0 origin-top-left" style={{ transform: `translate(${tx}px, ${ty}px) scale(${scale})`, width, height }}>
            <svg width={width} height={height} className="absolute top-0 left-0 pointer-events-none overflow-visible">
              {edges.map((e, i) => {
                const a = placed.get(e.parent_key), b = placed.get(e.child_key);
                if (!a || !b) return null;
                const x1 = a.x + NODE_W, y1 = a.y + NODE_H / 2, x2 = b.x, y2 = b.y + NODE_H / 2, mx = (x1 + x2) / 2;
                return <path key={i} d={`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`} fill="none" stroke={EDGE_COLOR[e.edge_type]} strokeWidth={1.6} strokeDasharray={e.edge_type === 'always' ? '5 4' : undefined} />;
              })}
            </svg>
            {[...placed.values()].map(({ node, x, y }) => {
              const tone = TYPE_TONE[node.node_type] || TYPE_TONE.job;
              const Icon = nodeIcon(node.node_type);
              const sel = node.node_key === selKey;
              const sub = node.node_type === 'job' ? (templateName(node.job_template_id) || 'no template')
                : node.node_type === 'webhook_out' ? (node.webhook_url || 'call out')
                : node.node_type === 'webhook_in' ? 'wait for event' : 'approval';
              const bad = showErrors && nodeBad(node);
              return (
                <div key={node.node_key} onMouseDown={e => e.stopPropagation()} onClick={() => setSelKey(node.node_key)}
                  className={`absolute rounded-lg p-2.5 flex flex-col gap-1.5 overflow-hidden cursor-pointer bg-panel border ${bad ? 'border-err' : tone.border}
                    ${sel ? '!border-acc shadow-[0_0_0_1px_var(--acc),0_10px_34px_-8px_rgba(77,224,200,.28)]' : bad ? 'shadow-[0_0_0_1px_var(--err),0_10px_28px_-10px_rgba(242,104,95,.35)]' : 'shadow-[0_4px_14px_-6px_rgba(0,0,0,.5)]'}`}
                  style={{ left: x, top: y, width: NODE_W, height: NODE_H }}>
                  <div className="flex items-center gap-2">
                    <span className={`w-[7px] h-[7px] rounded-full shrink-0 ${bad ? 'bg-err' : tone.led}`} />
                    <Icon size={14} className={`shrink-0 ${tone.icon}`} />
                    <span className="text-[12.5px] font-semibold truncate">{node.name || node.node_key}</span>
                  </div>
                  <span className={`font-mono text-[9.5px] pl-[22px] truncate ${bad ? 'text-err/90' : 'text-mut'}`}>{bad ? 'needs attention' : sub}</span>
                </div>
              );
            })}
          </div>

          {/* Zoom tools */}
          <div className="absolute top-3.5 right-3.5 flex flex-col rounded-lg border border-line2 overflow-hidden bg-panel z-10">
            {[{ i: <Plus size={14} />, f: () => zoom(1.2) }, { i: <Minus size={14} />, f: () => zoom(0.83) }, { i: <Maximize2 size={13} />, f: fit }].map((b, k) => (
              <button key={k} onClick={b.f} className="w-[30px] h-[30px] grid place-items-center text-mut hover:text-ink border-b border-line last:border-0">{b.i}</button>
            ))}
          </div>
          {/* Legend */}
          <div className="absolute left-3.5 bottom-3.5 flex gap-3.5 px-3 py-2 rounded-lg border border-line bg-panel/80 backdrop-blur z-10">
            {([['success', EDGE_COLOR.success], ['failure', EDGE_COLOR.failure], ['always', EDGE_COLOR.always]] as const).map(([l, c]) => (
              <span key={l} className="flex items-center gap-1.5 font-mono text-[10px] text-mut"><i className="w-3.5 h-0.5 rounded" style={{ background: c }} />{l}</span>
            ))}
          </div>
          {nodes.length === 0 && (
            <div className="absolute inset-0 grid place-items-center">
              <div className="text-center">
                <GitFork size={36} className="mx-auto mb-3 text-dim opacity-40" />
                <p className="text-sm text-mut mb-4">Empty workflow. Add a node to begin.</p>
                <Button icon={<Plus size={15} />} onClick={addNode}>Add node</Button>
              </div>
            </div>
          )}
        </div>

        {/* Editor panel */}
        <div className="border-l border-line bg-panel2 flex flex-col min-h-0 overflow-auto scroll-tint max-[720px]:border-l-0 max-[720px]:border-t">
          {/* Nodes */}
          <div className="px-4 py-3 border-b border-line">
            <div className="flex items-center mb-2.5">
              <span className="font-mono text-[10px] tracking-[0.14em] uppercase text-mut">Nodes</span>
              <button onClick={addNode} className="ml-auto flex items-center gap-1 text-[11.5px] text-acc2 hover:text-acc"><Plus size={13} /> Add</button>
            </div>
            <div className="space-y-1.5">
              {nodes.map(n => {
                const sel = n.node_key === selKey;
                const tone = TYPE_TONE[n.node_type] || TYPE_TONE.job;
                const bad = showErrors && nodeBad(n);
                const nameFieldBad = showErrors && !n.name.trim();
                const tmplBad = showErrors && n.node_type === 'job' && !n.job_template_id;
                const urlBad = showErrors && n.node_type === 'webhook_out' && !n.webhook_url?.trim();
                return (
                  <div key={n.node_key} className={`rounded-lg border ${sel ? 'border-acc/50 bg-acc/[0.05]' : bad ? 'border-err/60' : 'border-line bg-panel'}`}>
                    <button onClick={() => setSelKey(sel ? null : n.node_key)} className="w-full flex items-center gap-2.5 px-2.5 h-9 text-left">
                      <span className={`w-[7px] h-[7px] rounded-full shrink-0 ${bad ? 'bg-err' : tone.led}`} />
                      <span className="font-mono text-[10px] text-dim">{n.node_key}</span>
                      <span className="text-[12.5px] truncate flex-1">{n.name || <span className={bad ? 'text-err/90' : 'text-dim'}>unnamed</span>}</span>
                      <span className="font-mono text-[9.5px] text-dim">{n.node_type}</span>
                    </button>
                    {sel && (
                      <div className="px-2.5 pb-2.5 space-y-2 border-t border-line pt-2.5">
                        <input value={n.name} onChange={e => updateNode(n.node_key, { name: e.target.value })} placeholder="node name" className={`${psel} ${nameFieldBad ? '!border-err' : ''}`} />
                        <select value={n.node_type} onChange={e => updateNode(n.node_key, { node_type: e.target.value as WorkflowNodeType })} className={psel}>
                          <option value="job">job</option><option value="approval">approval</option>
                          <option value="webhook_in">webhook in (wait)</option><option value="webhook_out">webhook out (call)</option>
                        </select>
                        {n.node_type === 'job' && (
                          <select value={n.job_template_id ?? ''} onChange={e => updateNode(n.node_key, { job_template_id: Number(e.target.value) })} className={`${psel} ${tmplBad ? '!border-err' : ''}`}>
                            <option value="">template…</option>
                            {templates.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
                          </select>
                        )}
                        {n.node_type === 'webhook_out' && (
                          <input value={n.webhook_url || ''} onChange={e => updateNode(n.node_key, { webhook_url: e.target.value })} placeholder="https://…/hook" className={`${psel} ${urlBad ? '!border-err' : ''}`} />
                        )}
                        {n.node_type === 'webhook_in' && <p className="font-mono text-[10px] text-violet">Pauses until an external system POSTs its callback URL (shown on the run page).</p>}
                        {n.node_type === 'approval' && (
                          <p className="rounded-md border border-line bg-panel2 px-2.5 py-2 font-mono text-[10px] leading-relaxed text-dim">
                            Approval expires after 24 hours and follows the failure edge.
                          </p>
                        )}
                        <button onClick={() => removeNode(n.node_key)} className="flex items-center gap-1.5 text-[11.5px] text-err/90 hover:text-err"><Trash2 size={13} /> Remove node</button>
                      </div>
                    )}
                  </div>
                );
              })}
              {nodes.length === 0 && <p className="font-mono text-[11px] text-faint py-1">No nodes yet.</p>}
            </div>
          </div>

          {/* Edges */}
          <div className="px-4 py-3 border-b border-line">
            <button onClick={() => setShowEdges(v => !v)} className="w-full flex items-center mb-2.5">
              {showEdges ? <ChevronDown size={13} className="text-dim" /> : <ChevronRight size={13} className="text-dim" />}
              <span className="font-mono text-[10px] tracking-[0.14em] uppercase text-mut ml-1.5">Edges</span>
              <span className="ml-auto flex items-center gap-1 text-[11.5px] text-acc2 hover:text-acc" onClick={e => { e.stopPropagation(); addEdge(); }}><Plus size={13} /> Add</span>
            </button>
            {showEdges && (
              <div className="space-y-1.5">
                {edges.map((e, i) => (
                  <div key={i} className={`flex items-center gap-1 rounded-lg border bg-panel p-1.5 ${showErrors && edgeBad(e) ? 'border-err/60' : 'border-line'}`} title={showErrors && edgeBad(e) ? 'An edge cannot connect a node to itself.' : undefined}>
                    <select value={e.parent_key} onChange={ev => updateEdge(i, { parent_key: ev.target.value })} className="flex-1 min-w-0 bg-transparent text-[11.5px] text-ink2 outline-none">
                      {nodes.map(n => <option key={n.node_key} value={n.node_key} className="bg-panel">{nodeLabel(n.node_key)}</option>)}
                    </select>
                    <select value={e.edge_type} onChange={ev => updateEdge(i, { edge_type: ev.target.value as WorkflowEdgeType })}
                      className="bg-transparent text-[11px] font-mono outline-none" style={{ color: EDGE_COLOR[e.edge_type] }}>
                      {EDGE_TYPES.map(t => <option key={t} value={t} className="bg-panel text-ink">{t}</option>)}
                    </select>
                    <ChevronRight size={12} className="text-faint shrink-0" />
                    <select value={e.child_key} onChange={ev => updateEdge(i, { child_key: ev.target.value })} className="flex-1 min-w-0 bg-transparent text-[11.5px] text-ink2 outline-none">
                      {nodes.map(n => <option key={n.node_key} value={n.node_key} className="bg-panel">{nodeLabel(n.node_key)}</option>)}
                    </select>
                    <button onClick={() => removeEdge(i)} className="text-faint hover:text-err shrink-0"><Trash2 size={13} /></button>
                  </div>
                ))}
                {edges.length === 0 && <p className="font-mono text-[11px] text-faint py-1">No edges — nodes all start at once.</p>}
              </div>
            )}
          </div>

          {/* Trigger & options */}
          <div className="px-4 py-3">
            <span className="font-mono text-[10px] tracking-[0.14em] uppercase text-mut">Trigger &amp; options</span>
            <div className="flex items-center gap-3 py-2.5 mt-1">
              <div className="flex-1"><div className="text-[12.5px] text-ink2">Allow simultaneous runs</div><div className="font-mono text-[10px] text-dim mt-0.5">off = refused while a run is active</div></div>
              <Toggle on={allowSim} onChange={setAllowSim} />
            </div>
            <div className="flex items-center gap-3 py-2.5 border-t border-line">
              <div className="flex-1"><div className="text-[12.5px] text-ink2">Inbound webhook trigger</div><div className="font-mono text-[10px] text-dim mt-0.5">a remote event launches this workflow</div></div>
              <Toggle on={whEnabled} onChange={setWhEnabled} />
            </div>
            {whEnabled && (
              <div className="space-y-2 pt-1">
                <select value={whService} onChange={e => setWhService(e.target.value)} className={psel}>
                  <option value="generic">Generic (token)</option><option value="github">GitHub (HMAC)</option><option value="gitlab">GitLab (token)</option>
                </select>
                <Input className="font-mono text-xs" value={whKey} onChange={e => setWhKey(e.target.value)} placeholder={editingId ? 'leave blank to keep current' : 'shared secret'} error={whKeyBad ? 'A webhook trigger needs a secret.' : undefined} />
                <p className="font-mono text-[10px] text-dim">After saving, POST to <span className="text-ink2">/api/v1/webhooks/workflow-templates/&lt;id&gt;/{whService}</span> with this secret.</p>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default WorkflowBuilderPage;
