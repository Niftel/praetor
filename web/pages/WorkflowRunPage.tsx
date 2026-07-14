import React, { useEffect, useState, useCallback, useRef, useMemo } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { api } from '../services/api';
import { WorkflowJob, WorkflowJobNode, WorkflowEdge, WorkflowNodeType } from '../types';
import {
  ArrowLeft, RotateCcw, Play, Pause, ArrowDownToLine, ArrowUpFromLine,
  Check, X, Plus, Minus, Maximize2, ExternalLink, Copy,
} from 'lucide-react';
import WorkflowLaunchModal, { WorkflowLaunchOptions } from '../components/WorkflowLaunchModal';

const TERMINAL = ['successful', 'failed', 'error', 'canceled'];
const NODE_W = 158, NODE_H = 62, COL_GAP = 66, ROW_GAP = 26, MARGIN = 40;

type Vis = 'ok' | 'run' | 'await' | 'pend' | 'err';
const visOf = (s: string): Vis => {
  if (s === 'successful' || s === 'approved') return 'ok';
  if (s === 'running') return 'run';
  if (s === 'awaiting_approval' || s === 'awaiting_event') return 'await';
  if (s === 'failed' || s === 'error' || s === 'lost' || s === 'rejected') return 'err';
  return 'pend';
};
const LED: Record<Vis, string> = { ok: 'bg-ok', run: 'bg-run', await: 'bg-changed', pend: 'bg-faint', err: 'bg-err' };
const nodeIcon = (t: WorkflowNodeType) =>
  t === 'approval' ? Pause : t === 'webhook_in' ? ArrowDownToLine : t === 'webhook_out' ? ArrowUpFromLine : Play;
const typeLed = (t: WorkflowNodeType, v: Vis): string =>
  v === 'pend' ? (t === 'webhook_in' ? 'bg-acc' : t === 'webhook_out' ? 'bg-violet' : 'bg-faint') : LED[v];
const subline = (n: WorkflowJobNode): string => {
  const s = n.status;
  if (s === 'awaiting_approval') return 'awaiting approval';
  if (s === 'awaiting_event') return 'waiting for event';
  if (s === 'running') return 'running';
  if (s === 'successful' || s === 'approved') return n.node_type === 'job' ? 'converged' : s;
  return s;
};

const EDGE = { success: '#3ad07f', failure: '#f2685f', always: '#565f70', active: '#5aa2ff' };

interface Placed { node: WorkflowJobNode; x: number; y: number; }
function layout(nodes: WorkflowJobNode[], edges: WorkflowEdge[]) {
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
  const width = MARGIN * 2 + cols.size * NODE_W + Math.max(0, cols.size - 1) * COL_GAP;
  const height = MARGIN * 2 + Math.max(1, maxRows) * NODE_H + Math.max(0, maxRows - 1) * ROW_GAP;
  return { placed, width, height };
}

// Compact ansible-stdout parse for the inspector: per-host best outcome + tail.
function parseNodeLog(plain: string) {
  const rank: Record<string, number> = { ok: 0, changed: 1, failed: 2, unreachable: 3 };
  const hosts: Record<string, string> = {};
  const tail: string[] = [];
  for (const raw of plain.split('\n')) {
    const line = raw.replace(/\x1b\[[0-9;]*m/g, '').trimEnd();
    if (!line) continue;
    tail.push(line);
    const m = line.match(/^(ok|changed|failed|fatal|unreachable):\s+\[([^\]]+?)\]/);
    if (m) {
      const s = m[1] === 'fatal' ? (/UNREACHABLE/.test(line) ? 'unreachable' : 'failed') : m[1];
      const h = m[2];
      if (!(h in hosts) || rank[s] > rank[hosts[h]]) hosts[h] = s;
    }
  }
  const t = { ok: 0, changed: 0, failed: 0 };
  for (const h in hosts) { const s = hosts[h]; if (s === 'ok') t.ok++; else if (s === 'changed') t.changed++; else t.failed++; }
  return { stats: t, tail: tail.slice(-16) };
}

const WorkflowRunPage = () => {
  const { jobId } = useParams();
  const navigate = useNavigate();
  const id = Number(jobId);
  const [job, setJob] = useState<WorkflowJob | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [acting, setActing] = useState<number | null>(null);
  const [selKey, setSelKey] = useState<string | null>(null);
  const [showRelaunch, setShowRelaunch] = useState(false);

  const [scale, setScale] = useState(1);
  const [tx, setTx] = useState(0);
  const [ty, setTy] = useState(0);
  const viewRef = useRef<HTMLDivElement>(null);
  const pan = useRef<{ x: number; y: number; tx: number; ty: number } | null>(null);
  const fitted = useRef(false);

  const refresh = useCallback(() => {
    if (!id) return;
    return api.getWorkflowJob(id).then(j => { setJob(j); setError(''); }).catch(() => setError('Could not load this workflow run.')).finally(() => setLoading(false));
  }, [id]);

  useEffect(() => {
    let active = true;
    refresh();
    const h = setInterval(() => { if (active && !(job && TERMINAL.includes(job.status))) refresh(); }, 2500);
    return () => { active = false; clearInterval(h); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, job?.status]);

  const nodes = job?.nodes || [];
  const edges = job?.edges || [];
  const { placed, width, height } = useMemo(() => layout(nodes, edges), [nodes, edges]);

  const fit = useCallback(() => {
    const v = viewRef.current; if (!v || width === 0) return;
    const sc = Math.max(0.4, Math.min(1.1, (v.clientWidth - 48) / width, (v.clientHeight - 48) / height));
    setScale(sc); setTx((v.clientWidth - width * sc) / 2); setTy((v.clientHeight - height * sc) / 2);
  }, [width, height]);
  useEffect(() => { if (!fitted.current && width > 0 && viewRef.current) { fit(); fitted.current = true; } }, [width, height, fit]);

  const zoom = (f: number) => setScale(s => Math.min(1.8, Math.max(0.4, s * f)));
  const onWheel = (e: React.WheelEvent) => { if (e.ctrlKey || e.metaKey) { e.preventDefault(); zoom(e.deltaY < 0 ? 1.1 : 0.9); } };
  const onDown = (e: React.MouseEvent) => { pan.current = { x: e.clientX, y: e.clientY, tx, ty }; };
  const onMove = (e: React.MouseEvent) => { const p = pan.current; if (!p) return; setTx(p.tx + (e.clientX - p.x)); setTy(p.ty + (e.clientY - p.y)); };
  const onUp = () => { pan.current = null; };

  const decide = async (nodeId: number, approve: boolean) => {
    setActing(nodeId);
    try { await (approve ? api.approveWorkflowNode(nodeId) : api.denyWorkflowNode(nodeId)); await refresh(); }
    catch (e: any) { setError(e.message || 'Action failed.'); } finally { setActing(null); }
  };
  const release = async (nodeId: number, cb: string, fail: boolean) => {
    setActing(nodeId);
    try { await api.releaseWorkflowNode(cb, fail); await refresh(); }
    catch (e: any) { setError(e.message || 'Callback failed.'); } finally { setActing(null); }
  };

  const isTerminal = job ? TERMINAL.includes(job.status) : false;
  const isRunning = job?.status === 'running' || job?.status === 'pending';
  const done = nodes.filter(n => ['successful', 'approved', 'failed', 'error', 'skipped', 'rejected'].includes(n.status)).length;
  const running = nodes.filter(n => n.status === 'running').length;
  const okPct = nodes.length ? (nodes.filter(n => n.status === 'successful' || n.status === 'approved').length / nodes.length) * 100 : 0;
  const runPct = nodes.length ? (running / nodes.length) * 100 : 0;
  const pct = nodes.length ? Math.round((done / nodes.length) * 100) : 0;
  const gateNode = nodes.find(n => n.status === 'awaiting_approval') || nodes.find(n => n.status === 'awaiting_event');
  const selected = selKey ? nodes.find(n => n.node_key === selKey) || null : null;
  const badStatus = isTerminal && job?.status !== 'successful';

  const relaunch = async (options: WorkflowLaunchOptions) => {
    const wt = job?.workflow_template_id;
    if (!wt) return;
    const res = await api.launchWorkflow(wt, options);
    navigate(`/workflows/runs/${res.workflow_job_id}`);
  };

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink">
      {/* Identity bar */}
      <div className="flex items-center gap-4 px-5 py-3.5 border-b border-line shrink-0">
        <button onClick={() => navigate('/workflows')} className="w-7 h-7 grid place-items-center rounded-md border border-line2 text-mut hover:text-ink hover:border-white/20" title="Back to workflows"><ArrowLeft size={15} /></button>
        <div className="flex flex-col gap-0.5 min-w-0">
          <div className="flex items-center gap-2.5">
            <span className={`h-[7px] w-[7px] rounded-full ${isRunning ? 'bg-run animate-pulse' : badStatus ? 'bg-err' : 'bg-ok'}`} />
            <span className="text-[16px] font-semibold tracking-tight truncate">{job?.name || 'Workflow run'}</span>
            <span className="font-mono text-[12px] text-dim">run #{id}</span>
          </div>
          <div className="flex gap-3.5 font-mono text-[11px] text-mut">
            <span className={isRunning ? 'text-run' : badStatus ? 'text-err' : 'text-ok'}>{job?.status || (loading ? 'loading…' : '—')}</span>
            <span>{nodes.length} nodes · {edges.length} edges</span>
          </div>
        </div>
        <div className="ml-auto flex items-center gap-2">
          {isTerminal && job?.workflow_template_id != null && (
            <button onClick={() => setShowRelaunch(true)} className="h-[30px] px-3 rounded-md text-xs font-semibold flex items-center gap-1.5 border border-line2 text-ink hover:border-white/25"><RotateCcw size={13} /> Relaunch</button>
          )}
        </div>
      </div>

      {/* Convergence strip */}
      <div className="flex items-center h-10 border-b border-line bg-panel2 shrink-0 font-mono text-[11px]">
        <div className="flex items-center gap-2.5 h-full px-4 border-r border-line">
          <span className={`h-[7px] w-[7px] rounded-full ${isRunning ? 'bg-run animate-pulse' : 'bg-dim'}`} />
          <span className="text-[10px] uppercase tracking-[0.12em]" style={{ color: 'var(--run)' }}>{isRunning ? 'Running' : job?.status || '—'}</span>
        </div>
        <div className="flex items-center gap-1.5 h-full px-4 border-r border-line"><span className="text-dim uppercase tracking-[0.08em] text-[10px]">Nodes</span><span className="text-ink tabular-nums">{done} / {nodes.length}</span></div>
        <div className="flex-1 h-1.5 mx-[18px] rounded bg-line overflow-hidden flex">
          <i className="h-full bg-ok" style={{ width: `${okPct}%` }} />
          <i className="h-full bg-run" style={{ width: `${runPct}%` }} />
        </div>
        <span className="text-ink tabular-nums pr-[18px]">{pct}%</span>
        {gateNode && <div className="flex items-center gap-1.5 h-full px-4 border-l border-line"><span className="text-dim uppercase tracking-[0.08em] text-[10px]">Gate</span><span className="text-changed">{gateNode.status === 'awaiting_approval' ? 'awaiting approval' : 'awaiting event'}</span></div>}
      </div>

      {error && <div className="mx-5 mt-3 text-sm text-err bg-err/10 border border-err/30 rounded-md px-3 py-2 shrink-0">{error}</div>}

      {/* Canvas + inspector */}
      <div className="grid grid-cols-[1fr_328px] flex-1 min-h-0 max-[900px]:grid-cols-1">
        <div ref={viewRef} onWheel={onWheel} onMouseDown={onDown} onMouseMove={onMove} onMouseUp={onUp} onMouseLeave={onUp}
          className="relative overflow-hidden cursor-grab active:cursor-grabbing"
          style={{ background: '#060708', backgroundImage: 'radial-gradient(rgba(255,255,255,.05) 1px, transparent 1px)', backgroundSize: '22px 22px' }}>
          <div className="absolute top-0 left-0 origin-top-left" style={{ transform: `translate(${tx}px, ${ty}px) scale(${scale})`, width, height }}>
            <svg width={width} height={height} className="absolute top-0 left-0 pointer-events-none overflow-visible">
              {edges.map((e, i) => {
                const a = placed.get(e.parent_key), b = placed.get(e.child_key);
                if (!a || !b) return null;
                const x1 = a.x + NODE_W, y1 = a.y + NODE_H / 2, x2 = b.x, y2 = b.y + NODE_H / 2, mx = (x1 + x2) / 2;
                const active = b.node.status === 'running';
                const color = active ? EDGE.active : EDGE[e.edge_type as keyof typeof EDGE] || EDGE.always;
                return <path key={i} d={`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`} fill="none" stroke={color} strokeWidth={active ? 2 : 1.6}
                  strokeDasharray={e.edge_type === 'always' || active ? '5 4' : undefined} className={active ? 'dag-flow' : undefined} />;
              })}
            </svg>
            {[...placed.values()].map(({ node, x, y }) => {
              const v = visOf(node.status);
              const Icon = nodeIcon(node.node_type);
              const sel = node.node_key === selKey;
              return (
                <div key={node.node_key} onMouseDown={e => e.stopPropagation()} onClick={() => setSelKey(node.node_key)}
                  className={`absolute rounded-lg p-2.5 flex flex-col gap-1.5 overflow-hidden cursor-pointer bg-panel border
                    ${v === 'pend' ? 'border-line2 border-dashed opacity-55' : v === 'run' ? 'border-run/45' : v === 'await' ? 'border-changed/40' : v === 'err' ? 'border-err/40' : 'border-line2'}
                    ${sel ? '!border-acc shadow-[0_0_0_1px_var(--acc),0_10px_34px_-8px_rgba(77,224,200,.28)]' : 'shadow-[0_4px_14px_-6px_rgba(0,0,0,.5)]'}`}
                  style={{ left: x, top: y, width: NODE_W, height: NODE_H }}>
                  <div className="flex items-center gap-2">
                    <span className={`w-[7px] h-[7px] rounded-full shrink-0 ${typeLed(node.node_type, v)} ${v === 'run' ? 'ring-[3px] ring-run/20' : ''}`} />
                    <Icon size={14} className={`shrink-0 ${v === 'run' ? 'text-run' : v === 'await' ? 'text-changed' : 'text-mut'}`} />
                    <span className="text-[12.5px] font-semibold truncate">{node.name || node.node_key}</span>
                  </div>
                  <span className="font-mono text-[9.5px] text-mut pl-[22px] truncate">{subline(node)}</span>
                  {v === 'run' && <span className="absolute left-0 right-0 bottom-0 h-0.5 bg-white/5 overflow-hidden"><i className="absolute inset-y-0 bg-run dag-indeterminate" /></span>}
                </div>
              );
            })}
          </div>

          <div className="absolute top-3.5 right-3.5 flex flex-col rounded-lg border border-line2 overflow-hidden bg-panel z-10">
            {[{ i: <Plus size={14} />, f: () => zoom(1.2) }, { i: <Minus size={14} />, f: () => zoom(0.83) }, { i: <Maximize2 size={13} />, f: fit }].map((b, k) => (
              <button key={k} onClick={b.f} className="w-[30px] h-[30px] grid place-items-center text-mut hover:text-ink border-b border-line last:border-0">{b.i}</button>
            ))}
          </div>
          <div className="absolute left-3.5 bottom-3.5 flex gap-3.5 px-3 py-2 rounded-lg border border-line bg-panel/80 backdrop-blur z-10">
            {([['success', EDGE.success], ['failure', EDGE.failure], ['always', EDGE.always], ['active path', EDGE.active]] as const).map(([l, c]) => (
              <span key={l} className="flex items-center gap-1.5 font-mono text-[10px] text-mut"><i className="w-3.5 h-0.5 rounded" style={{ background: c }} />{l}</span>
            ))}
          </div>
          {nodes.length === 0 && <div className="absolute inset-0 grid place-items-center text-dim text-sm">{loading ? 'Loading…' : 'No nodes.'}</div>}
        </div>

        <Inspector node={selected} acting={acting} onDecide={decide} onRelease={release} onOpen={(uid) => navigate(`/jobs/${uid}`)} />
      </div>

      <div className="flex items-center gap-4 px-5 py-1.5 border-t border-line bg-panel2 shrink-0 font-mono text-[10.5px] text-dim">
        <span><b className="text-mut">click</b> inspect node</span><span><b className="text-mut">drag</b> pan</span><span><b className="text-mut">⌘+scroll</b> zoom</span>
        <span className="ml-auto">run #{id} · {nodes.length} nodes · {running} running{gateNode ? ' · 1 gate' : ''}</span>
      </div>
      <WorkflowLaunchModal
        isOpen={showRelaunch}
        workflowName={job?.name || 'Workflow'}
        onClose={() => setShowRelaunch(false)}
        onLaunch={relaunch}
      />
    </div>
  );
};

const Inspector: React.FC<{
  node: WorkflowJobNode | null; acting: number | null;
  onDecide: (id: number, ok: boolean) => void; onRelease: (id: number, cb: string, fail: boolean) => void; onOpen: (uid: number) => void;
}> = ({ node, acting, onDecide, onRelease, onOpen }) => {
  const [stats, setStats] = useState({ ok: 0, changed: 0, failed: 0 });
  const [tail, setTail] = useState<string[]>([]);

  useEffect(() => {
    setStats({ ok: 0, changed: 0, failed: 0 }); setTail([]);
    if (!node?.run_id) return;
    let active = true;
    const rid = node.run_id;
    const load = () => api.getJobLogs(rid).then(txt => { if (!active) return; const p = parseNodeLog(txt || ''); setStats(p.stats); setTail(p.tail); }).catch(() => { });
    load();
    const h = node.status === 'running' ? setInterval(load, 2500) : null;
    return () => { active = false; if (h) clearInterval(h); };
  }, [node?.run_id, node?.status]);

  if (!node) return <div className="border-l border-line bg-panel2 grid place-items-center text-dim text-[12px] max-[900px]:hidden">Select a node to inspect it.</div>;

  const v = visOf(node.status);
  const lineCls = (s: string) => /^(changed|fatal|unreachable):/.test(s) ? 'text-changed' : /^ok:/.test(s) ? 'text-ok' : /^failed:/.test(s) ? 'text-err' : /^(TASK|PLAY)/.test(s) ? 'text-acc2' : 'text-mut';

  return (
    <div className="border-l border-line bg-panel2 flex flex-col min-h-0 max-[900px]:hidden">
      <div className="px-4 py-3.5 border-b border-line">
        <div className="font-mono text-[10px] tracking-[0.14em] uppercase text-dim">Selected node</div>
        <div className="flex items-center gap-2.5 mt-2">
          <span className={`w-2 h-2 rounded-full ${LED[v]} ${v === 'run' ? 'ring-[3px] ring-run/20' : ''}`} />
          <span className="text-[14px] font-semibold">{node.name || node.node_key}</span>
        </div>
        <div className="font-mono text-[11px] text-mut mt-1.5">{node.node_type} · {node.status}</div>
      </div>

      {node.run_id && (
        <div className="flex border-b border-line">
          <Stat n={stats.ok} l="OK" c="text-ok" />
          <Stat n={stats.changed} l="Changed" c="text-changed" />
          <Stat n={stats.failed} l="Failed" c={stats.failed ? 'text-err' : 'text-faint'} />
        </div>
      )}

      {node.status === 'awaiting_approval' && (
        <div className="p-3 border-b border-line flex gap-2">
          <button disabled={acting === node.id} onClick={() => onDecide(node.id, true)} className="flex-1 h-9 rounded-lg bg-acc text-[#04211d] font-semibold text-[12px] flex items-center justify-center gap-1.5 hover:bg-acc2 disabled:opacity-50"><Check size={14} /> Approve</button>
          <button disabled={acting === node.id} onClick={() => onDecide(node.id, false)} className="flex-1 h-9 rounded-lg border border-err/40 text-err/90 font-semibold text-[12px] flex items-center justify-center gap-1.5 hover:bg-err/10 disabled:opacity-50"><X size={14} /> Deny</button>
        </div>
      )}
      {node.status === 'awaiting_event' && (
        <div className="p-3 border-b border-line space-y-2">
          <div className="flex gap-2">
            <button disabled={acting === node.id || !node.callback_url} onClick={() => node.callback_url && onRelease(node.id, node.callback_url, false)} className="flex-1 h-9 rounded-lg bg-acc text-[#04211d] font-semibold text-[12px] flex items-center justify-center gap-1.5 hover:bg-acc2 disabled:opacity-50"><Check size={14} /> Release</button>
            <button disabled={acting === node.id || !node.callback_url} onClick={() => node.callback_url && onRelease(node.id, node.callback_url, true)} className="flex-1 h-9 rounded-lg border border-err/40 text-err/90 font-semibold text-[12px] flex items-center justify-center gap-1.5 hover:bg-err/10 disabled:opacity-50"><X size={14} /> Fail</button>
          </div>
          {node.callback_url && (
            <div className="flex items-center gap-2 rounded-lg border border-line bg-[#070809] px-2.5 py-1.5">
              <code className="flex-1 font-mono text-[10px] text-mut truncate">POST {node.callback_url}</code>
              <button onClick={() => navigator.clipboard?.writeText(`${window.location.origin}${node.callback_url}`)} className="text-dim hover:text-acc"><Copy size={13} /></button>
            </div>
          )}
        </div>
      )}

      <div className="font-mono text-[10px] tracking-[0.12em] uppercase text-dim px-4 pt-3 pb-1.5">Live tail</div>
      <div className="flex-1 overflow-auto scroll-tint px-4 pb-3 font-mono text-[11px] leading-[1.7]">
        {node.run_id ? (tail.length ? tail.map((s, i) => <div key={i} className={`whitespace-pre-wrap break-all ${lineCls(s)}`}>{s}</div>) : <span className="text-faint">Waiting for output…</span>)
          : <span className="text-faint">{node.node_type === 'job' ? 'No run yet.' : `${node.node_type} node — no job output.`}</span>}
      </div>

      {node.unified_job_id != null && (
        <div className="p-3 border-t border-line">
          <button onClick={() => onOpen(node.unified_job_id!)} className="w-full h-9 rounded-lg border border-acc/60 text-acc bg-acc/[0.06] font-semibold text-[12px] flex items-center justify-center gap-2 hover:bg-acc/10"><ExternalLink size={14} /> Open full run</button>
        </div>
      )}
    </div>
  );
};

const Stat: React.FC<{ n: number; l: string; c: string }> = ({ n, l, c }) => (
  <div className="flex-1 px-3.5 py-2.5 border-r border-line last:border-0">
    <div className={`font-mono text-[16px] font-semibold tabular-nums ${c}`}>{n}</div>
    <div className="font-mono text-[9px] tracking-[0.08em] uppercase text-mut mt-0.5">{l}</div>
  </div>
);

export default WorkflowRunPage;
