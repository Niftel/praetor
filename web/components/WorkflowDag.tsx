import React, { useMemo } from 'react';
import { WorkflowNode, WorkflowEdge, WorkflowEdgeType } from '../types';

// Layered DAG layout (no external graph library): assign each node a column by
// its longest path from a root, stack nodes within a column, and draw edges as
// curves colored by edge type. Shared by the builder, detail and run views.
const NODE_W = 168;
const NODE_H = 52;
const GAP_X = 76;
const GAP_Y = 28;
const MARGIN = 16;

// edgePath routes a parent's right-center to a child's left-center as an
// orthogonal elbow with rounded corners: horizontal out, vertical, horizontal
// in. The final segment is horizontal so the arrowhead meets the child flush.
function edgePath(x1: number, y1: number, x2: number, y2: number): string {
  if (Math.abs(y2 - y1) < 1) return `M${x1},${y1} L${x2},${y2}`; // same row → straight
  const midX = x1 + Math.max(20, (x2 - x1) / 2);
  const dir = y2 > y1 ? 1 : -1;
  const r = Math.min(10, Math.abs(x2 - midX), Math.abs(y2 - y1) / 2);
  return [
    `M${x1},${y1}`,
    `L${midX - r},${y1}`,
    `Q${midX},${y1} ${midX},${y1 + dir * r}`,
    `L${midX},${y2 - dir * r}`,
    `Q${midX},${y2} ${midX + r},${y2}`,
    `L${x2},${y2}`,
  ].join(' ');
}

interface Placed extends WorkflowNode { x: number; y: number; }

function layoutDag(nodes: WorkflowNode[], edges: WorkflowEdge[]) {
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
      placed.set(k, { ...byKey.get(k)!, x: MARGIN + d * (NODE_W + GAP_X), y: MARGIN + row * (NODE_H + GAP_Y) });
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

export function statusFill(status?: string): { fill: string; stroke: string; text: string } {
  switch (status) {
    case 'successful':
    case 'approved': return { fill: '#dcfce7', stroke: '#16a34a', text: '#166534' };
    case 'failed':
    case 'error':
    case 'lost':
    case 'rejected': return { fill: '#fee2e2', stroke: '#dc2626', text: '#991b1b' };
    case 'running': return { fill: '#dbeafe', stroke: '#2563eb', text: '#1e40af' };
    case 'awaiting_approval': return { fill: '#fef3c7', stroke: '#d97706', text: '#92400e' };
    case 'awaiting_event': return { fill: '#f3e8ff', stroke: '#9333ea', text: '#6b21a8' };
    case 'skipped': return { fill: '#f1f5f9', stroke: '#94a3b8', text: '#475569' };
    case 'pending': return { fill: '#f8fafc', stroke: '#cbd5e1', text: '#64748b' };
    default: return { fill: '#ffffff', stroke: '#cbd5e1', text: '#334155' };
  }
}

interface WorkflowDagProps {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  statusByKey?: Record<string, string>;        // run view: node_key -> status
  templateName?: (id?: number | null) => string; // build/detail view label
}

const WorkflowDag: React.FC<WorkflowDagProps> = ({ nodes, edges, statusByKey, templateName }) => {
  const { placed, width, height } = useMemo(() => layoutDag(nodes, edges), [nodes, edges]);
  if (nodes.length === 0) {
    return <div className="text-sm text-gray-400 italic py-8 text-center">No nodes to display.</div>;
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
          return (
            <path key={i} d={edgePath(x1, y1, x2, y2)}
              fill="none" stroke={EDGE_COLOR[e.edge_type]} strokeWidth={2} markerEnd={`url(#arrow-${e.edge_type})`} />
          );
        })}
        {Array.from(placed.values()).map(n => {
          const st = statusByKey?.[n.node_key];
          // Builder/detail (no live status): tint by node type. Run view: by status.
          const typeTone: Record<string, { fill: string; stroke: string; text: string }> = {
            approval: { fill: '#fef3c7', stroke: '#d97706', text: '#92400e' },
            webhook_in: { fill: '#f3e8ff', stroke: '#9333ea', text: '#6b21a8' },
            webhook_out: { fill: '#cffafe', stroke: '#0891b2', text: '#155e75' },
            job: { fill: '#eef2ff', stroke: '#6366f1', text: '#3730a3' },
          };
          const tone = statusByKey ? statusFill(st) : (typeTone[n.node_type] || typeTone.job);
          const typeLabel: Record<string, string> = {
            approval: 'approval', webhook_in: 'wait for event', webhook_out: n.webhook_url || 'call out',
          };
          const sub = statusByKey
            ? (st || 'pending')
            : (n.node_type === 'job'
              ? (templateName ? templateName(n.job_template_id) : 'job')
              : (typeLabel[n.node_type] || n.node_type));
          const icon = n.node_type === 'approval' ? '⏸ '
            : n.node_type === 'webhook_in' ? '📥 '
            : n.node_type === 'webhook_out' ? '📤 ' : '▶ ';
          return (
            <g key={n.node_key}>
              <rect x={n.x} y={n.y} width={NODE_W} height={NODE_H} rx={8} fill={tone.fill} stroke={tone.stroke} strokeWidth={1.5} />
              <text x={n.x + 10} y={n.y + 21} fontSize={13} fontWeight={600} fill={tone.text}>
                {(n.name && n.name.length > 20) ? n.name.slice(0, 19) + '…' : (n.name || n.node_key)}
              </text>
              <text x={n.x + 10} y={n.y + 39} fontSize={11} fill={tone.text} opacity={0.85}>
                {icon}{String(sub).length > 22 ? String(sub).slice(0, 21) + '…' : sub}
              </text>
            </g>
          );
        })}
      </svg>
    </div>
  );
};

export default WorkflowDag;
