import React, { useMemo } from 'react';
import { Play, Pause, ArrowDownToLine, ArrowUpFromLine } from 'lucide-react';
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

// Edge color encodes branch semantics, drawn from the design-system semantic
// ramp: success = emerald, failure = red, always = neutral. (DESIGN.md §2.)
const EDGE_COLOR: Record<WorkflowEdgeType, string> = {
  success: '#3ad07f',
  failure: '#f2685f',
  always: '#565f70',
};

// Node tones map onto the app's semantic tokens (emerald/amber/red/cobalt) plus
// the neutral ramp — no bespoke greens/blues/purples. Waiting states that share
// a hue are told apart by their type icon + status sub-line, never by color
// alone (DESIGN.md "The State-Never-By-Color-Alone Rule").
export function statusFill(status?: string): { fill: string; stroke: string; text: string } {
  switch (status) {
    case 'successful':
    case 'approved': return { fill: 'rgba(58,208,127,.12)', stroke: '#3ad07f', text: '#9fe7bf' }; // success
    case 'failed':
    case 'error':
    case 'lost':
    case 'rejected': return { fill: 'rgba(242,104,95,.12)', stroke: '#f2685f', text: '#f4a29b' }; // error
    case 'running': return { fill: 'rgba(90,162,255,.14)', stroke: '#5aa2ff', text: '#a9caff' };  // active
    case 'awaiting_approval': return { fill: 'rgba(224,178,58,.12)', stroke: '#e0b23a', text: '#e8cd88' }; // needs a human
    case 'awaiting_event': return { fill: 'rgba(77,224,200,.10)', stroke: '#4de0c8', text: '#7fe6d4' };    // inbound event
    case 'skipped': return { fill: 'rgba(255,255,255,.03)', stroke: '#3a4150', text: '#606978' };  // muted
    case 'pending': return { fill: '#0e1016', stroke: '#3a4150', text: '#606978' };  // neutral
    default: return { fill: '#0e1016', stroke: '#3a4150', text: '#c8cdd8' };
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
    return <div className="text-sm text-dim italic py-8 text-center">No nodes to display.</div>;
  }
  return (
    <div className="overflow-auto scroll-tint border border-line rounded-lg bg-[#090a0e]">
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
          // Builder/detail (no live status): tint by node type on the system's
          // own tokens — cobalt for the common job node, amber for approvals
          // (they pause for a human), and the sanctioned DAG status palette for
          // the webhooks: signal teal for inbound (waiting on an event), dispatch
          // violet for outbound (calling out). See DESIGN.md "Workflow Status
          // Palette" — these hues are permitted here and nowhere else.
          const typeTone: Record<string, { fill: string; stroke: string; text: string }> = {
            approval: { fill: 'rgba(224,178,58,.12)', stroke: '#e0b23a', text: '#e8cd88' },
            webhook_in: { fill: 'rgba(77,224,200,.10)', stroke: '#4de0c8', text: '#7fe6d4' },
            webhook_out: { fill: 'rgba(176,107,255,.12)', stroke: '#b06bff', text: '#c9a8ff' },
            job: { fill: 'rgba(90,162,255,.14)', stroke: '#5aa2ff', text: '#a9caff' },
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
          // Lucide glyphs (rendered as nested SVGs) replace the old emoji so the
          // DAG shares the app's icon language: play = job, pause = approval,
          // inbound/outbound arrows = webhook in/out.
          const NodeIcon = n.node_type === 'approval' ? Pause
            : n.node_type === 'webhook_in' ? ArrowDownToLine
            : n.node_type === 'webhook_out' ? ArrowUpFromLine : Play;
          const subText = String(sub).length > 20 ? String(sub).slice(0, 19) + '…' : sub;
          return (
            <g key={n.node_key}>
              <rect x={n.x} y={n.y} width={NODE_W} height={NODE_H} rx={8} fill={tone.fill} stroke={tone.stroke} strokeWidth={1.5} />
              <text x={n.x + 10} y={n.y + 21} fontSize={13} fontWeight={600} fill={tone.text}>
                {(n.name && n.name.length > 20) ? n.name.slice(0, 19) + '…' : (n.name || n.node_key)}
              </text>
              <NodeIcon x={n.x + 10} y={n.y + 30} width={13} height={13} color={tone.text} strokeWidth={2} aria-hidden="true" />
              <text x={n.x + 28} y={n.y + 40} fontSize={11} fill={tone.text} opacity={0.85}>
                {subText}
              </text>
            </g>
          );
        })}
      </svg>
    </div>
  );
};

export default WorkflowDag;
