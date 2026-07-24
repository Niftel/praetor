import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import Anser from 'anser';
import {
  AlertTriangle, ArrowLeft, Check, CheckCircle2, Copy, Download,
  ExternalLink, Loader, RotateCcw, Square, TerminalSquare, XCircle,
} from 'lucide-react';
import { api, DiagnosticEvent, unwrap } from '../services/api';
import { Job, JobTemplate } from '../types';
import { useRunDiagnostics } from '../hooks/useRunDiagnostics';
import { useCapabilities } from '../lib/useCapabilities';
import { buildHostRows, buildTaskRows, failureGuidance, HostRow, Outcome, TaskRow } from '../lib/executionDiagnostics';
import { confirmDialog, toast } from '../components/ui/toast';

type View = 'overview' | 'tasks' | 'hosts' | 'failures' | 'output';

const TERMINAL = new Set(['successful', 'failed', 'error', 'canceled', 'lost']);
const ACTIVE = new Set(['pending', 'queued', 'running', 'waiting']);

const tone = (state?: string) => {
  if (state === 'successful') return { label: 'Successful', text: 'text-ok', dot: 'bg-ok' };
  if (state === 'failed' || state === 'error' || state === 'lost') return { label: state === 'lost' ? 'Lost' : 'Failed', text: 'text-err', dot: 'bg-err' };
  if (state === 'canceled') return { label: 'Canceled', text: 'text-mut', dot: 'bg-mut' };
  return { label: state ? state[0].toUpperCase() + state.slice(1) : 'Unknown', text: 'text-run', dot: 'bg-run' };
};

const outcomeTone: Record<Outcome, string> = {
  ok: 'text-ok', changed: 'text-changed', failed: 'text-err', unreachable: 'text-violet',
  skipped: 'text-dim', running: 'text-run', unknown: 'text-mut',
};

const fmtDuration = (start?: string, finish?: string, now = Date.now()) => {
  if (!start) return '—';
  const elapsed = Math.max(0, (finish ? new Date(finish).getTime() : now) - new Date(start).getTime());
  const seconds = Math.floor(elapsed / 1000);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const pad = (value: number) => String(value).padStart(2, '0');
  return hours ? `${hours}:${pad(minutes)}:${pad(seconds % 60)}` : `${pad(minutes)}:${pad(seconds % 60)}`;
};

export const renderAnsiLine = (line: string) =>
  line ? Anser.ansiToHtml(Anser.escapeForHtml(line), { use_classes: false }) : '&nbsp;';

const JobDetailPage = () => {
  const { jobId } = useParams();
  const id = Number(jobId);
  const navigate = useNavigate();
  const location = useLocation();
  const [job, setJob] = useState<Job | null>((location.state as { job?: Job } | null)?.job || null);
  const [view, setView] = useState<View>('overview');
  const [logs, setLogs] = useState('');
  const [logsLoaded, setLogsLoaded] = useState(false);
  const [copied, setCopied] = useState(false);
  const [busy, setBusy] = useState(false);
  const [now, setNow] = useState(Date.now());
  const [templateId, setTemplateId] = useState<number | null>(null);
  const logCursor = useRef(-1);
  const logLoading = useRef(false);

  const runId = job?.current_run_id;
  const diagnostics = useRunDiagnostics(runId);
  const effectiveState = diagnostics.summary?.state || job?.status;
  const isTerminal = TERMINAL.has(effectiveState || '');
  const isActive = ACTIVE.has(effectiveState || '');
  const stateTone = tone(effectiveState);
  const { capabilities, loading: capabilityLoading } = useCapabilities('job_template', templateId);

  const loadJob = useCallback(async () => {
    try {
      const jobs: Job[] = unwrap(await api.getJobs());
      const found = jobs.find(item => item.id === id);
      if (found) setJob(found);
    } catch { /* preserve last known state */ }
  }, [id]);

  useEffect(() => { void loadJob(); }, [loadJob]);
  useEffect(() => {
    if (!job?.unified_job_template_id) return setTemplateId(null);
    let active = true;
    api.getTemplates().then(response => {
      if (!active) return;
      const match = unwrap<JobTemplate>(response).find(item => item.unified_job_template_id === job.unified_job_template_id);
      setTemplateId(match?.id || null);
    }).catch(() => { if (active) setTemplateId(null); });
    return () => { active = false; };
  }, [job?.unified_job_template_id]);

  useEffect(() => {
    if (isTerminal) return;
    const interval = setInterval(() => { void loadJob(); setNow(Date.now()); }, 1500);
    return () => clearInterval(interval);
  }, [isTerminal, loadJob]);

  const loadLogs = useCallback(async () => {
    if (!runId || logLoading.current) return;
    logLoading.current = true;
    try {
      const result = await api.getJobLogsSince(runId, logCursor.current);
      if (typeof result.lastSeq === 'number') logCursor.current = result.lastSeq;
      if (result.text) setLogs(current => current + result.text);
    } catch { /* raw evidence remains optional */ }
    finally { logLoading.current = false; setLogsLoaded(true); }
  }, [runId]);

  useEffect(() => {
    logCursor.current = -1;
    setLogs('');
    setLogsLoaded(false);
  }, [runId]);
  useEffect(() => {
    if (view !== 'output' || !runId) return;
    void loadLogs();
    if (isTerminal) return;
    const interval = setInterval(() => void loadLogs(), 1500);
    return () => clearInterval(interval);
  }, [view, runId, isTerminal, loadLogs]);

  const taskRows = useMemo(() => buildTaskRows(diagnostics.events), [diagnostics.events]);
  const hostRows = useMemo(() => buildHostRows(diagnostics.events), [diagnostics.events]);

  const failures = useMemo(() => diagnostics.events.filter(event =>
    event.failure_code || ['failed', 'unreachable'].includes(event.outcome || '') || event.event_type.includes('FAILED')),
  [diagnostics.events]);
  const lifecycle = useMemo(() => diagnostics.events.filter(event => !event.host_id && !event.task_name).slice(-30), [diagnostics.events]);

  const cancel = async () => {
    if (!job || !(await confirmDialog(`Cancel job "${job.name}"?`, { confirmText: 'Cancel job', destructive: true }))) return;
    setBusy(true);
    try { await api.cancelJob(job.id); toast.info('Cancellation requested'); await loadJob(); }
    catch { toast.error('Failed to cancel job'); }
    finally { setBusy(false); }
  };
  const relaunch = async () => {
    if (!job?.unified_job_template_id) return toast.error('This job has no template to relaunch');
    setBusy(true);
    try {
      const response = await api.launchJob({ unified_job_template_id: job.unified_job_template_id, name: job.name, relaunch_source_job_id: job.id });
      const nextId = response?.id ?? response?.job?.id;
      toast.success('Relaunched');
      navigate(nextId ? `/jobs/${nextId}` : '/jobs', nextId ? { state: { job: response.job ?? response } } : undefined);
    } catch { toast.error('Failed to relaunch'); }
    finally { setBusy(false); }
  };

  const plainLogs = logs.replace(/\x1b\[[0-9;]*m/g, '');
  const renderedLines = useMemo(() => logs.split('\n').map(renderAnsiLine), [logs]);
  const tabs: { id: View; label: string; count?: number }[] = [
    { id: 'overview', label: 'Overview' }, { id: 'tasks', label: 'Tasks', count: taskRows.length },
    { id: 'hosts', label: 'Hosts', count: hostRows.length }, { id: 'failures', label: 'Failures', count: failures.length },
    { id: 'output', label: 'Output' },
  ];

  return (
    <main className="flex h-full min-h-0 flex-col bg-bg text-ink">
      <header className="shrink-0 border-b border-line bg-panel">
        <div className="flex flex-col gap-4 px-4 py-4 sm:px-6 lg:flex-row lg:items-start">
          <button onClick={() => navigate('/jobs')} className="mt-0.5 grid h-8 w-8 shrink-0 place-items-center rounded-md border border-line2 text-mut hover:text-ink" aria-label="Back to jobs"><ArrowLeft size={15} /></button>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2.5">
              <span className={`h-2 w-2 rounded-full ${stateTone.dot} ${isActive ? 'animate-pulse' : ''}`} />
              <h1 className="min-w-0 break-words text-[19px] font-semibold tracking-[-0.02em]">{job?.name || `Job #${id}`}</h1>
              <span className={`font-mono text-xs ${stateTone.text}`}>{stateTone.label}</span>
              {diagnostics.summary?.failure_code && <span className="rounded-md bg-err/10 px-2 py-1 font-mono text-[10px] text-err">{diagnostics.summary.failure_code}</span>}
            </div>
            <dl className="mt-3 flex flex-wrap gap-x-6 gap-y-2 font-mono text-[11px]">
              <HeaderFact label="Phase" value={diagnostics.summary?.current_phase || (isActive ? 'starting' : 'complete')} />
              <HeaderFact label="Elapsed" value={fmtDuration(diagnostics.summary?.started_at || job?.started_at, diagnostics.summary?.finished_at || job?.finished_at, now)} />
              <HeaderFact label="Attempt" value={String(diagnostics.summary?.attempt || 1)} />
              <HeaderFact label="Events" value={String(diagnostics.summary?.last_event_seq ?? diagnostics.events.length)} />
              <HeaderFact label="Updates" value={diagnostics.connection === 'live' ? 'Live' : diagnostics.connection === 'polling' ? 'Polling' : diagnostics.connection === 'connecting' ? 'Reconnecting' : 'Complete'} valueClass={diagnostics.connection === 'live' ? 'text-ok' : ''} />
            </dl>
          </div>
          <div className="flex shrink-0 gap-2">
            {!capabilityLoading && capabilities.execute && isTerminal && <ActionButton onClick={relaunch} disabled={busy} icon={<RotateCcw size={13} />}>Relaunch</ActionButton>}
            {!capabilityLoading && capabilities.execute && isActive && <ActionButton onClick={cancel} disabled={busy} danger icon={<Square size={12} />}>Cancel</ActionButton>}
          </div>
        </div>
        <div role="tablist" aria-label="Job diagnostics" onKeyDown={event => {
          if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
          const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]')];
          const current = buttons.indexOf(document.activeElement as HTMLButtonElement);
          const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (current + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length;
          event.preventDefault(); buttons[next]?.focus(); buttons[next]?.click();
        }} className="flex overflow-x-auto px-4 sm:px-6">
          {tabs.map(tab => <button key={tab.id} role="tab" aria-selected={view === tab.id} onClick={() => setView(tab.id)} className={`flex h-10 shrink-0 items-center gap-2 border-b-2 px-3 text-xs font-medium ${view === tab.id ? 'border-acc text-ink' : 'border-transparent text-mut hover:text-ink'}`}>{tab.label}{tab.count != null && <span className="font-mono text-[10px] text-dim">{tab.count}</span>}</button>)}
        </div>
      </header>

      <section role="tabpanel" className="min-h-0 flex-1 overflow-auto scroll-tint">
        {diagnostics.loading ? <CenteredState icon={<Loader className="animate-spin" size={17} />} text="Loading execution diagnostics…" /> :
          view === 'overview' ? <Overview summary={diagnostics.summary} lifecycle={lifecycle} hosts={hostRows} tasks={taskRows} error={diagnostics.error} navigate={navigate} /> :
          view === 'tasks' ? <TasksView rows={taskRows} /> :
          view === 'hosts' ? <HostsView rows={hostRows} /> :
          view === 'failures' ? <FailuresView events={failures} summaryCode={diagnostics.summary?.failure_code} /> :
          <OutputView lines={renderedLines} loaded={logsLoaded} live={isActive} copied={copied} onCopy={async () => { await navigator.clipboard.writeText(plainLogs); setCopied(true); setTimeout(() => setCopied(false), 1800); }} onDownload={() => { const blob = new Blob([plainLogs], { type: 'text/plain' }); const url = URL.createObjectURL(blob); const anchor = document.createElement('a'); anchor.href = url; anchor.download = `job-${id}-output.txt`; anchor.click(); URL.revokeObjectURL(url); }} />}
      </section>
    </main>
  );
};

const HeaderFact = ({ label, value, valueClass = '' }: { label: string; value: string; valueClass?: string }) => <div><dt className="text-dim">{label}</dt><dd className={`mt-0.5 text-ink2 ${valueClass}`}>{value}</dd></div>;
const ActionButton = ({ children, icon, danger, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { icon: React.ReactNode; danger?: boolean }) => <button {...props} className={`flex h-8 items-center gap-1.5 rounded-md border px-3 text-xs font-semibold disabled:opacity-50 ${danger ? 'border-err/40 text-err hover:bg-err/10' : 'border-line2 text-ink hover:border-white/25'}`}>{icon}{children}</button>;
const CenteredState = ({ icon, text }: { icon: React.ReactNode; text: string }) => <div className="grid min-h-64 place-items-center"><div className="flex items-center gap-2 text-sm text-mut">{icon}{text}</div></div>;

const Overview = ({ summary, lifecycle, hosts, tasks, error, navigate }: { summary: ReturnType<typeof useRunDiagnostics>['summary']; lifecycle: DiagnosticEvent[]; hosts: HostRow[]; tasks: TaskRow[]; error: string | null; navigate: ReturnType<typeof useNavigate> }) => {
  const counts = hosts.reduce((all, host) => ({ ...all, [host.outcome]: (all[host.outcome] || 0) + 1 }), {} as Record<string, number>);
  return <div className="mx-auto w-full max-w-[1180px] p-4 sm:p-6">
    {error && <div className="mb-4 flex items-center gap-2 rounded-lg bg-changed/10 px-3 py-2 text-xs text-changed"><AlertTriangle size={14} />Live updates were interrupted; bounded polling is keeping this view current.</div>}
    <div className="grid gap-6 lg:grid-cols-[minmax(0,1.5fr)_minmax(280px,.8fr)]">
      <section aria-labelledby="execution-story"><SectionHead id="execution-story" title="Execution timeline" detail={`${lifecycle.length} lifecycle events`} />
        <div className="border-t border-line">{lifecycle.length ? lifecycle.map(event => <EventRow key={event.seq} event={event} />) : <Empty text="No lifecycle events have been recorded yet." />}</div>
      </section>
      <div className="space-y-7">
        <section aria-labelledby="outcomes"><SectionHead id="outcomes" title="Outcome summary" detail={`${hosts.length} hosts`} />
          <dl className="mt-3 grid grid-cols-2 border border-line rounded-lg overflow-hidden">
            {(['ok', 'changed', 'failed', 'unreachable'] as Outcome[]).map(value => <div key={value} className="border-b border-r border-line p-3"><dt className={`font-mono text-xl ${outcomeTone[value]}`}>{counts[value] || 0}</dt><dd className="mt-1 text-[11px] capitalize text-mut">{value}</dd></div>)}
          </dl>
          <p className="mt-3 text-xs text-mut">{tasks.length} structured tasks observed. Host identities remain represented by authorized numeric IDs.</p>
        </section>
        <section aria-labelledby="lineage"><SectionHead id="lineage" title="Relaunch lineage" detail={`Attempt ${summary?.attempt || 1}`} />
          <div className="mt-3 space-y-2 font-mono text-xs">
            {summary?.source_job_id ? <button onClick={() => navigate(`/jobs/${summary.source_job_id}`)} className="flex items-center gap-2 text-acc hover:text-acc2"><ArrowLeft size={12} />Source job #{summary.source_job_id}</button> : <p className="text-mut">Original launch</p>}
            {(summary?.subsequent_job_ids || []).map(jobId => <button key={jobId} onClick={() => navigate(`/jobs/${jobId}`)} className="flex items-center gap-2 text-acc hover:text-acc2">Relaunch #{jobId}<ExternalLink size={11} /></button>)}
            {!summary?.subsequent_job_ids?.length && <p className="text-dim">No subsequent attempts.</p>}
          </div>
        </section>
      </div>
    </div>
  </div>;
};

const TasksView = ({ rows }: { rows: TaskRow[] }) => {
  const [query, setQuery] = useState(''); const [visible, setVisible] = useState(100);
  const filtered = rows.filter(row => `${row.name} ${row.play} ${row.outcome}`.toLowerCase().includes(query.toLowerCase()));
  return <div className="mx-auto w-full max-w-[1180px] p-4 sm:p-6"><SectionHead id="tasks-heading" title="Tasks" detail={`${filtered.length} structured tasks`} /><Filter value={query} onChange={value => { setQuery(value); setVisible(100); }} placeholder="Filter by task, play, or outcome" /><div className="mt-3 overflow-x-auto rounded-lg border border-line"><table className="w-full min-w-[680px] text-left text-xs"><thead className="bg-panel2 text-[10px] text-mut"><tr><th className="px-3 py-2.5">Task</th><th className="px-3 py-2.5">Play</th><th className="px-3 py-2.5">Outcome</th><th className="px-3 py-2.5 text-right">Hosts</th><th className="px-3 py-2.5 text-right">Duration</th><th className="px-3 py-2.5 text-right">Last event</th></tr></thead><tbody>{filtered.slice(0, visible).map(row => <tr key={row.key} className="border-t border-line"><td className="max-w-[38ch] px-3 py-2.5 font-medium text-ink">{row.name}</td><td className="px-3 py-2.5 text-mut">{row.play}</td><td className={`px-3 py-2.5 capitalize ${outcomeTone[row.outcome]}`}>{row.outcome}</td><td className="px-3 py-2.5 text-right font-mono">{row.hosts.size}</td><td className="px-3 py-2.5 text-right font-mono">{row.duration ? `${row.duration} ms` : '—'}</td><td className="px-3 py-2.5 text-right font-mono text-dim">#{row.lastSeq}</td></tr>)}</tbody></table>{!filtered.length && <Empty text="No tasks match this filter." />}</div><LoadMore shown={visible} total={filtered.length} onClick={() => setVisible(value => value + 100)} /></div>;
};
const HostsView = ({ rows }: { rows: HostRow[] }) => {
  const [query, setQuery] = useState(''); const [visible, setVisible] = useState(100);
  const filtered = rows.filter(row => `host ${row.id} ${row.outcome}`.includes(query.toLowerCase()));
  return <div className="mx-auto w-full max-w-[1000px] p-4 sm:p-6"><SectionHead id="hosts-heading" title="Hosts" detail={`${filtered.length} authorized host IDs`} /><Filter value={query} onChange={value => { setQuery(value); setVisible(100); }} placeholder="Filter by host ID or outcome" /><div className="mt-3 overflow-x-auto rounded-lg border border-line"><table className="w-full min-w-[560px] text-left text-xs"><thead className="bg-panel2 text-[10px] text-mut"><tr><th className="px-3 py-2.5">Host</th><th className="px-3 py-2.5">Outcome</th><th className="px-3 py-2.5 text-right">Tasks</th><th className="px-3 py-2.5 text-right">Failures</th><th className="px-3 py-2.5 text-right">Last event</th></tr></thead><tbody>{filtered.slice(0, visible).map(row => <tr key={row.id} className="border-t border-line"><td className="px-3 py-2.5 font-mono text-ink">Host #{row.id}</td><td className={`px-3 py-2.5 capitalize ${outcomeTone[row.outcome]}`}>{row.outcome}</td><td className="px-3 py-2.5 text-right font-mono">{row.tasks.size}</td><td className={`px-3 py-2.5 text-right font-mono ${row.failures ? 'text-err' : 'text-dim'}`}>{row.failures}</td><td className="px-3 py-2.5 text-right font-mono text-dim">#{row.lastSeq}</td></tr>)}</tbody></table>{!filtered.length && <Empty text="No hosts match this filter." />}</div><LoadMore shown={visible} total={filtered.length} onClick={() => setVisible(value => value + 100)} /></div>;
};
const FailuresView = ({ events, summaryCode }: { events: DiagnosticEvent[]; summaryCode?: string }) => <div className="mx-auto w-full max-w-[900px] p-4 sm:p-6"><SectionHead id="failures-heading" title="Failures" detail={`${events.length} failure events`} />{summaryCode && <div className="mt-3 rounded-lg bg-err/10 p-4"><div className="font-mono text-xs text-err">{summaryCode}</div><p className="mt-2 max-w-[70ch] text-xs leading-relaxed text-ink2">{failureGuidance(summaryCode)}</p></div>}<div className="mt-4 divide-y divide-line border-y border-line">{events.map(event => <div key={event.seq} className="py-3"><div className="flex flex-wrap items-center gap-2"><XCircle size={13} className="text-err" /><span className="font-medium">{event.task_name || event.event_type}</span><span className="font-mono text-[10px] text-dim">#{event.seq}</span></div><div className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1 pl-5 font-mono text-[10.5px] text-mut">{event.host_id != null && <span>Host #{event.host_id}</span>}{event.play_name && <span>{event.play_name}</span>}{event.failure_code && <span className="text-err">{event.failure_code}</span>}</div>{event.failure_code && <p className="mt-2 pl-5 text-xs text-ink2">{failureGuidance(event.failure_code)}</p>}</div>)}{!events.length && <Empty text="No structured failures were recorded for this run." />}</div></div>;

const OutputView = ({ lines, loaded, live, copied, onCopy, onDownload }: { lines: string[]; loaded: boolean; live: boolean; copied: boolean; onCopy: () => void; onDownload: () => void }) => <div className="flex h-full min-h-[440px] flex-col bg-[#070809]"><div className="flex h-11 shrink-0 items-center gap-2 border-b border-line px-4"><TerminalSquare size={14} className="text-mut" /><span className="font-mono text-[10px] text-mut">Raw execution evidence</span>{live && <span className="ml-2 flex items-center gap-1.5 font-mono text-[10px] text-run"><span className="h-1.5 w-1.5 animate-pulse rounded-full bg-run" />live</span>}<div className="ml-auto flex gap-1"><button onClick={onCopy} aria-label="Copy output" className="rounded p-1.5 text-mut hover:bg-white/5 hover:text-ink">{copied ? <Check size={15} className="text-ok" /> : <Copy size={15} />}</button><button onClick={onDownload} aria-label="Download output" className="rounded p-1.5 text-mut hover:bg-white/5 hover:text-ink"><Download size={15} /></button></div></div><div className="flex-1 overflow-auto scroll-tint px-4 py-3 font-mono text-[12px] leading-[1.65]">{lines.length && lines.some(line => line !== '&nbsp;') ? lines.map((html, index) => <div key={index} className="whitespace-pre-wrap break-words" dangerouslySetInnerHTML={{ __html: html }} />) : <p className="text-dim">{loaded ? 'No raw output is available.' : 'Loading raw output…'}</p>}</div></div>;
const SectionHead = ({ id, title, detail }: { id: string; title: string; detail: string }) => <div className="flex items-baseline justify-between gap-4"><h2 id={id} className="text-sm font-semibold">{title}</h2><span className="font-mono text-[10.5px] text-dim">{detail}</span></div>;
const Filter = ({ value, onChange, placeholder }: { value: string; onChange: (value: string) => void; placeholder: string }) => <input type="search" value={value} onChange={event => onChange(event.target.value)} placeholder={placeholder} aria-label={placeholder} className="mt-3 h-9 w-full max-w-sm rounded-md border border-line2 bg-panel px-3 text-xs text-ink placeholder:text-mut" />;
const LoadMore = ({ shown, total, onClick }: { shown: number; total: number; onClick: () => void }) => shown < total ? <div className="mt-3 text-center"><button onClick={onClick} className="rounded-md border border-line2 px-3 py-2 text-xs text-ink hover:border-white/25">Show next {Math.min(100, total - shown)} <span className="ml-1 font-mono text-dim">{shown}/{total}</span></button></div> : null;
const EventRow = ({ event }: { event: DiagnosticEvent }) => <div className="flex gap-3 py-2.5 text-xs"><span className="mt-1 h-2 w-2 shrink-0 rounded-full bg-run" /><div className="min-w-0 flex-1"><div className="font-medium text-ink">{event.event_type.replaceAll('_', ' ').toLowerCase()}</div><div className="mt-0.5 font-mono text-[10px] text-dim">#{event.seq} · {new Date(event.created_at).toLocaleString()}</div></div></div>;
const Empty = ({ text }: { text: string }) => <p className="px-3 py-8 text-center text-xs text-dim">{text}</p>;

export default JobDetailPage;
