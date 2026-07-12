import React from 'react';
import WorkflowDag from '../components/WorkflowDag';
import { WorkflowNode, WorkflowEdge } from '../types';

// Dev-only visual check for WorkflowDag: renders the graph across every run
// status and node type with no backend, so the design-system color mapping
// (success/error/cobalt/amber + Signal Teal / Dispatch Violet) can be eyeballed
// in isolation. Mounted only under import.meta.env.DEV (see App.tsx) at
// /_preview/workflow-dag — it is stripped from production builds.

// Run-view fixture — exercises every status + all three edge types.
const runNodes: WorkflowNode[] = [
  { node_key: 'a', node_type: 'job', name: 'Build image' },
  { node_key: 'b', node_type: 'job', name: 'Deploy fleet' },
  { node_key: 'c', node_type: 'job', name: 'Smoke test' },
  { node_key: 'd', node_type: 'job', name: 'Rollback' },
  { node_key: 'e', node_type: 'job', name: 'Notify' },
  { node_key: 'f', node_type: 'approval', name: 'Prod gate' },
  { node_key: 'g', node_type: 'webhook_in', name: 'Await signal' },
];
const runEdges: WorkflowEdge[] = [
  { parent_key: 'a', child_key: 'b', edge_type: 'success' },
  { parent_key: 'a', child_key: 'd', edge_type: 'failure' },
  { parent_key: 'b', child_key: 'c', edge_type: 'success' },
  { parent_key: 'c', child_key: 'e', edge_type: 'always' },
  { parent_key: 'd', child_key: 'e', edge_type: 'always' },
  { parent_key: 'f', child_key: 'g', edge_type: 'success' },
];
const runStatus: Record<string, string> = {
  a: 'successful', b: 'running', c: 'pending', d: 'failed', e: 'skipped',
  f: 'awaiting_approval', g: 'awaiting_event',
};

// Builder-view fixture — exercises all four node types (tinted by type).
const buildNodes: WorkflowNode[] = [
  { node_key: 'wi', node_type: 'webhook_in', name: 'Wait for CI' },
  { node_key: 'j', node_type: 'job', name: 'Configure hosts', job_template_id: 1 },
  { node_key: 'ap', node_type: 'approval', name: 'Change window' },
  { node_key: 'wo', node_type: 'webhook_out', name: 'Page on-call' },
];
const buildEdges: WorkflowEdge[] = [
  { parent_key: 'wi', child_key: 'j', edge_type: 'always' },
  { parent_key: 'j', child_key: 'ap', edge_type: 'success' },
  { parent_key: 'ap', child_key: 'wo', edge_type: 'success' },
];

const Section: React.FC<{ title: string; caption: string; children: React.ReactNode }> = ({ title, caption, children }) => (
  <section className="mb-10">
    <h2 className="text-sm font-semibold tracking-tight text-gray-900">{title}</h2>
    <p className="mt-0.5 mb-3 text-xs text-gray-500">{caption}</p>
    {children}
  </section>
);

const WorkflowDagPreview: React.FC = () => (
  <div className="min-h-screen p-8">
    <div className="max-w-5xl mx-auto">
      <header className="mb-8">
        <h1 className="text-xl font-semibold tracking-tight text-gray-900">Workflow DAG — visual check</h1>
        <p className="mt-1 text-sm text-gray-600">
          Dev-only fixture render of every node status and type. No backend required.
        </p>
      </header>

      <Section
        title="Run view — status-driven fills"
        caption="successful · running · pending · failed · skipped · awaiting approval · awaiting event"
      >
        <WorkflowDag nodes={runNodes} edges={runEdges} statusByKey={runStatus} />
      </Section>

      <Section
        title="Builder view — type-driven tints"
        caption="job · approval · webhook-in (Signal Teal) · webhook-out (Dispatch Violet)"
      >
        <WorkflowDag nodes={buildNodes} edges={buildEdges} templateName={() => 'deploy-web'} />
      </Section>
    </div>
  </div>
);

export default WorkflowDagPreview;
