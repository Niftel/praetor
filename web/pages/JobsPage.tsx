import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Job, WorkflowRunSummary } from '../types';
import { toast, confirmDialog } from '../components/ui/toast';
import { EmptyState, LoadingState, Page, PageHeader, PageToolbar } from '../components/ui';
import { ChevronDown, ChevronRight, FileText, GitFork, Search, Square } from 'lucide-react';

const ACTIVE = ['pending', 'queued', 'running', 'waiting'];
const isActive = (status: string) => ACTIVE.includes(status);
const isFailure = (status: string) => status === 'failed' || status === 'error';

type StatusFilter = 'all' | 'active' | 'failed' | 'successful';
type TypeFilter = 'all' | 'playbook' | 'workflow';
type RunKind = 'playbook' | 'workflow';

type Execution = {
  key: string;
  id: number;
  kind: RunKind;
  name: string;
  status: string;
  createdAt?: string;
  startedAt?: string;
  finishedAt?: string | null;
  templateId?: number;
  runId?: string;
  job?: Job;
};

const statusTone = (status: string): { text: string; dot: string } => {
  if (status === 'successful') return { text: 'text-ok', dot: 'bg-ok' };
  if (isFailure(status)) return { text: 'text-err', dot: 'bg-err' };
  if (status === 'canceled') return { text: 'text-mut', dot: 'bg-mut' };
  if (isActive(status)) return { text: 'text-run', dot: 'bg-run' };
  return { text: 'text-mut', dot: 'bg-mut' };
};

const dateTime = (iso?: string | null) => {
  if (!iso) return '—';
  const value = new Date(iso);
  return Number.isNaN(value.getTime()) ? '—' : value.toLocaleString([], { dateStyle: 'short', timeStyle: 'short' });
};

const duration = (run: Execution, now: number) => {
  const start = run.startedAt || run.createdAt;
  if (!start) return '—';
  const started = new Date(start).getTime();
  const ended = run.finishedAt ? new Date(run.finishedAt).getTime() : (isActive(run.status) ? now : NaN);
  if (!Number.isFinite(started) || !Number.isFinite(ended)) return '—';
  const seconds = Math.max(0, Math.floor((ended - started) / 1000));
  const pad = (value: number) => String(value).padStart(2, '0');
  const hours = Math.floor(seconds / 3600);
  return hours ? `${hours}:${pad(Math.floor(seconds / 60) % 60)}:${pad(seconds % 60)}` : `${pad(Math.floor(seconds / 60))}:${pad(seconds % 60)}`;
};

const JobsPage = () => {
  const navigate = useNavigate();
  const [jobs, setJobs] = useState<Job[]>([]);
  const [workflows, setWorkflows] = useState<WorkflowRunSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [typeFilter, setTypeFilter] = useState<TypeFilter>('all');
  const [query, setQuery] = useState('');
  const [expanded, setExpanded] = useState<string | null>(null);
  const [now, setNow] = useState(Date.now());

  const loadData = async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const [jobResult, workflowResult] = await Promise.allSettled([api.getJobs(), api.getWorkflowJobs()]);
      if (jobResult.status === 'fulfilled') setJobs(unwrap<Job>(jobResult.value));
      if (workflowResult.status === 'fulfilled') setWorkflows(unwrap<WorkflowRunSummary>(workflowResult.value));
    } finally {
      if (!silent) setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
    const poll = window.setInterval(() => loadData(true), 5000);
    const tick = window.setInterval(() => setNow(Date.now()), 1000);
    return () => { window.clearInterval(poll); window.clearInterval(tick); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const executions = useMemo<Execution[]>(() => [
    ...jobs.map(job => ({
      key: `playbook-${job.id}`,
      id: job.id,
      kind: 'playbook' as const,
      name: job.name,
      status: job.status,
      createdAt: job.created_at,
      startedAt: job.started_at,
      finishedAt: job.finished_at,
      templateId: job.unified_job_template_id,
      runId: job.current_run_id,
      job,
    })),
    ...workflows.map(run => ({
      key: `workflow-${run.id}`,
      id: run.id,
      kind: 'workflow' as const,
      name: run.template_name,
      status: run.status,
      createdAt: run.created_at,
      finishedAt: run.finished_at,
      templateId: run.workflow_template_id,
    })),
  ].sort((a, b) => new Date(b.createdAt || 0).getTime() - new Date(a.createdAt || 0).getTime()), [jobs, workflows]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return executions.filter(run => {
      const matchesStatus = statusFilter === 'all'
        || (statusFilter === 'active' && isActive(run.status))
        || (statusFilter === 'failed' && isFailure(run.status))
        || (statusFilter === 'successful' && run.status === 'successful');
      const matchesType = typeFilter === 'all' || run.kind === typeFilter;
      const matchesQuery = !needle || run.name.toLowerCase().includes(needle) || String(run.id).includes(needle);
      return matchesStatus && matchesType && matchesQuery;
    });
  }, [executions, query, statusFilter, typeFilter]);

  const counts = useMemo(() => ({
    all: executions.length,
    active: executions.filter(run => isActive(run.status)).length,
    failed: executions.filter(run => isFailure(run.status)).length,
    successful: executions.filter(run => run.status === 'successful').length,
  }), [executions]);

  const openRun = (run: Execution) => navigate(run.kind === 'workflow' ? `/workflows/runs/${run.id}` : `/jobs/${run.id}`, run.job ? { state: { job: run.job } } : undefined);

  const cancel = async (run: Execution) => {
    if (!run.job || !(await confirmDialog(`Cancel job "${run.name}"?`, { confirmText: 'Cancel job', destructive: true }))) return;
    try {
      await api.cancelJob(run.id);
      toast.info('Cancellation requested');
      loadData(true);
    } catch {
      toast.error('Failed to cancel job');
    }
  };

  const statusOptions: { key: StatusFilter; label: string }[] = [
    { key: 'all', label: 'All' },
    { key: 'active', label: 'Active' },
    { key: 'failed', label: 'Failed' },
    { key: 'successful', label: 'Successful' },
  ];

  return (
    <Page layout="workspace" className="bg-bg text-ink">
      <PageHeader
        layout="workspace"
        title="Jobs"
        description="Playbook and workflow executions, ordered by most recent activity."
        actions={(
          <div className="flex items-center gap-3 font-mono text-xs tabular-nums">
            <span className="text-dim">{executions.length} executions</span>
            {counts.active > 0 && <span className="text-run">{counts.active} active</span>}
          </div>
        )}
      />

      <PageToolbar className="mb-0 shrink-0 border-b border-line px-4 py-3 sm:px-6">
        <div className="flex items-center gap-1" aria-label="Filter jobs by status">
          {statusOptions.map(option => (
            <button
              key={option.key}
              onClick={() => setStatusFilter(option.key)}
              aria-pressed={statusFilter === option.key}
              className={`font-mono text-[11px] px-2.5 py-1.5 rounded-md border transition-colors ${statusFilter === option.key ? 'text-ink bg-white/5 border-line2' : 'text-mut border-transparent hover:text-ink'}`}
            >
              {option.label} <span className="ml-1 text-dim tabular-nums">{counts[option.key]}</span>
            </button>
          ))}
        </div>
        <select
          aria-label="Filter jobs by type"
          value={typeFilter}
          onChange={event => setTypeFilter(event.target.value as TypeFilter)}
          className="h-[30px] px-2.5 rounded-md bg-panel border border-line2 text-[11px] text-ink2 font-mono hover:border-white/20 focus:border-acc/60"
        >
          <option value="all">All job types</option>
          <option value="playbook">Playbook jobs</option>
          <option value="workflow">Workflow jobs</option>
        </select>
        <label className="relative ml-auto max-[700px]:w-full max-[700px]:mt-1">
          <span className="sr-only">Search jobs</span>
          <Search size={13} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-dim pointer-events-none" />
          <input
            value={query}
            onChange={event => setQuery(event.target.value)}
            placeholder="Search jobs by name or ID"
            className="h-[30px] w-[250px] max-[700px]:w-full pl-8 pr-3 rounded-md bg-panel border border-line2 text-xs text-ink placeholder:text-mut hover:border-white/20 focus:border-acc/60"
          />
        </label>
      </PageToolbar>

      <div className="grid grid-cols-[30px_minmax(220px,1fr)_120px_130px_150px_110px] items-center px-6 h-[34px] border-b border-line shrink-0 font-mono text-[9.5px] tracking-[0.1em] uppercase text-dim max-[820px]:grid-cols-[30px_minmax(0,1fr)_110px_90px]">
        <span aria-hidden="true" />
        <span>Name</span>
        <span>Status</span>
        <span>Type</span>
        <span className="max-[820px]:hidden">Started</span>
        <span className="text-right">Duration</span>
      </div>

      <div className="flex-1 overflow-auto scroll-tint">
        {filtered.map(run => {
          const tone = statusTone(run.status);
          const isExpanded = expanded === run.key;
          const TypeIcon = run.kind === 'workflow' ? GitFork : FileText;
          return (
            <div key={run.key} className="border-b border-line">
              <div className="grid grid-cols-[30px_minmax(220px,1fr)_120px_130px_150px_110px] items-center px-6 min-h-[48px] hover:bg-white/[0.025] transition-colors max-[820px]:grid-cols-[30px_minmax(0,1fr)_110px_90px]">
                <button
                  onClick={() => setExpanded(isExpanded ? null : run.key)}
                  className="w-7 h-7 grid place-items-center rounded text-dim hover:text-ink hover:bg-white/5"
                  aria-label={`${isExpanded ? 'Collapse' : 'Expand'} ${run.name}`}
                  aria-expanded={isExpanded}
                >
                  {isExpanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                </button>
                <button onClick={() => openRun(run)} className="min-w-0 text-left pr-4 group">
                  <span className="block text-[13px] font-medium truncate group-hover:text-acc">{run.name}</span>
                  <span className="block font-mono text-[10.5px] text-dim mt-0.5 tabular-nums">#{run.id}</span>
                </button>
                <span className={`inline-flex items-center gap-2 text-[12px] ${tone.text}`}>
                  <span className={`h-[7px] w-[7px] rounded-full ${tone.dot} ${isActive(run.status) ? 'animate-pulse' : ''}`} />
                  {run.status}
                </span>
                <span className="inline-flex items-center gap-1.5 text-[11px] text-mut"><TypeIcon size={12} /> {run.kind === 'workflow' ? 'Workflow job' : 'Playbook job'}</span>
                <span className="font-mono text-[11px] text-mut tabular-nums max-[820px]:hidden">{dateTime(run.startedAt || run.createdAt)}</span>
                <span className="font-mono text-[11px] text-mut tabular-nums text-right">{duration(run, now)}</span>
              </div>

              {isExpanded && (
                <div className="ml-[54px] mr-6 mb-3 py-3 border-t border-line grid grid-cols-[repeat(4,minmax(0,1fr))_auto] gap-4 items-end max-[900px]:grid-cols-2 max-[600px]:grid-cols-1">
                  <Detail label="Job ID" value={`#${run.id}`} />
                  <Detail label="Template ID" value={run.templateId ? `#${run.templateId}` : '—'} />
                  <Detail label="Created" value={dateTime(run.createdAt)} />
                  <Detail label="Finished" value={dateTime(run.finishedAt)} />
                  <div className="flex justify-end gap-2 max-[900px]:justify-start">
                    {run.kind === 'playbook' && isActive(run.status) && <button onClick={() => cancel(run)} className="h-8 px-3 rounded-md text-[11px] font-medium text-err hover:bg-err/10 inline-flex items-center gap-1.5"><Square size={11} /> Cancel</button>}
                    <button onClick={() => openRun(run)} className="h-8 px-3 rounded-md border border-line2 text-[11px] font-medium text-ink2 hover:text-ink hover:border-white/20">View output</button>
                  </div>
                </div>
              )}
            </div>
          );
        })}

        {!loading && filtered.length === 0 && (
          <div className="p-6">
            <EmptyState
              title={executions.length === 0 ? 'No jobs yet' : 'No jobs match these filters'}
              description={executions.length === 0 ? 'Launch an automation template to create the first execution.' : 'Change or clear the filters to see more executions.'}
              action={executions.length > 0 ? <button onClick={() => { setStatusFilter('all'); setTypeFilter('all'); setQuery(''); }} className="text-[12px] font-medium text-acc hover:text-acc2">Clear filters</button> : undefined}
            />
          </div>
        )}
        {loading && executions.length === 0 && <LoadingState label="Loading jobs" />}
      </div>
    </Page>
  );
};

const Detail: React.FC<{ label: string; value: string }> = ({ label, value }) => (
  <div className="min-w-0">
    <div className="font-mono text-[9.5px] uppercase tracking-[0.1em] text-dim">{label}</div>
    <div className="font-mono text-[11px] text-ink2 mt-1 truncate tabular-nums">{value}</div>
  </div>
);

export default JobsPage;
