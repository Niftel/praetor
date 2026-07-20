import React, { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Job } from '../types';
import RunLifecycle from '../components/RunLifecycle';
import { toast, confirmDialog } from '../components/ui/toast';
import {
  ArrowLeft, Copy, Check, Download, RotateCcw, Square,
  CheckCircle2, Circle, XCircle, AlertTriangle,
} from 'lucide-react';
import Anser from 'anser';

const TERMINAL_STATES = ['successful', 'failed', 'error', 'canceled'];
const ACTIVE_STATES = ['pending', 'queued', 'running', 'waiting'];

// ── Host-outcome model, parsed from the playbook's own stdout ────────────────
// Praetor emits engine-narration events (RUNNER_ONLINE, checkpoints) but not a
// per-task/per-host event stream — the authoritative record of what happened on
// each host is the reassembled log. So the plays/tasks spine and the host matrix
// are derived here from the real output rather than fabricated. Precedence when a
// host has mixed results across a task: unreachable > failed > changed > ok.
type HostStatus = 'ok' | 'changed' | 'failed' | 'unreachable' | 'skipped' | 'running';
const RANK: Record<HostStatus, number> = { ok: 0, skipped: 0, changed: 1, running: 1, failed: 2, unreachable: 3 };

interface Task { name: string; play: string; results: Record<string, HostStatus>; }
interface Play { name: string; tasks: Task[]; }
interface Parsed {
  plays: Play[];
  tasks: Task[];
  hosts: Record<string, HostStatus>;
  hostOrder: string[];
  hasRecap: boolean;
  totals: { ok: number; changed: number; failed: number; unreachable: number };
}

const RESULT_RE = /^(ok|changed|skipping|failed|fatal|unreachable):\s+\[([^\]]+?)\]/;
const RECAP_RE = /^(\S+)\s*:\s*ok=(\d+)\s+changed=(\d+)\s+unreachable=(\d+)\s+failed=(\d+)/;

const worse = (a: HostStatus, b: HostStatus) => (RANK[b] > RANK[a] ? b : a);

function parseRun(plain: string, running: boolean): Parsed {
  const plays: Play[] = [];
  const tasks: Task[] = [];
  const hosts: Record<string, HostStatus> = {};
  const hostOrder: string[] = [];
  let curPlay = '';
  let curTask: Task | null = null;
  let inRecap = false;
  let hasRecap = false;

  const touchHost = (h: string, s: HostStatus) => {
    if (!(h in hosts)) { hosts[h] = s; hostOrder.push(h); }
    else hosts[h] = worse(hosts[h], s);
  };

  for (const raw of plain.split('\n')) {
    const line = raw.replace(/\x1b\[[0-9;]*m/g, '').trimEnd();
    if (!line) continue;

    if (/^PLAY RECAP/.test(line)) { inRecap = true; hasRecap = true; continue; }
    if (inRecap) {
      const m = line.match(RECAP_RE);
      if (m) {
        const [, host, , changed, unreach, failed] = m;
        const s: HostStatus = +unreach > 0 ? 'unreachable' : +failed > 0 ? 'failed' : +changed > 0 ? 'changed' : 'ok';
        if (!(host in hosts)) hostOrder.push(host);
        hosts[host] = s;
      }
      continue;
    }

    let m = line.match(/^PLAY \[(.*?)\]/);
    if (m) { curPlay = m[1] || 'play'; plays.push({ name: curPlay, tasks: [] }); curTask = null; continue; }

    m = line.match(/^(?:TASK|RUNNING HANDLER) \[(.+?)\]/);
    if (m) {
      curTask = { name: m[1], play: curPlay, results: {} };
      tasks.push(curTask);
      (plays[plays.length - 1] || (plays.push({ name: curPlay || 'play', tasks: [] }), plays[0])).tasks.push(curTask);
      continue;
    }

    m = line.match(RESULT_RE);
    if (m) {
      const kind = m[1];
      const host = m[2];
      let s: HostStatus =
        kind === 'skipping' ? 'skipped' :
        kind === 'fatal' ? (/UNREACHABLE/.test(line) ? 'unreachable' : 'failed') :
        (kind as HostStatus);
      if (curTask) curTask.results[host] = worse(curTask.results[host] || 'ok', s);
      touchHost(host, s);
    }
  }

  // While the run is live and no recap has landed, the hosts working the current
  // task are still in flight — surface that rather than showing a stale outcome.
  if (running && !hasRecap && curTask) {
    for (const h of hostOrder) if (!(h in curTask.results)) hosts[h] = 'running';
  }

  const totals = { ok: 0, changed: 0, failed: 0, unreachable: 0 };
  for (const h of hostOrder) {
    const s = hosts[h];
    if (s === 'unreachable') totals.unreachable++;
    else if (s === 'failed') totals.failed++;
    else if (s === 'changed') totals.changed++;
    else if (s === 'ok') totals.ok++;
  }
  return { plays, tasks, hosts, hostOrder, hasRecap, totals };
}

const statusTone = (s?: string): { label: string; color: string; dot: string } => {
  if (s === 'successful') return { label: 'successful', color: 'text-ok', dot: 'bg-ok' };
  if (s === 'failed' || s === 'error') return { label: s, color: 'text-err', dot: 'bg-err' };
  if (s === 'canceled') return { label: 'canceled', color: 'text-mut', dot: 'bg-mut' };
  if (ACTIVE_STATES.includes(s || '')) return { label: s || 'running', color: 'text-run', dot: 'bg-run' };
  return { label: s || '—', color: 'text-mut', dot: 'bg-mut' };
};

const fmtClock = (ms: number) => {
  if (!Number.isFinite(ms) || ms < 0) ms = 0;
  const s = Math.floor(ms / 1000);
  const m = Math.floor(s / 60);
  const h = Math.floor(m / 60);
  const pad = (n: number) => String(n).padStart(2, '0');
  return h > 0 ? `${h}:${pad(m % 60)}:${pad(s % 60)}` : `${pad(m)}:${pad(s % 60)}`;
};

const relTime = (iso?: string) => {
  if (!iso) return '';
  try { return new Date(iso).toLocaleTimeString(); } catch { return ''; }
};

const HOST_DOT: Record<HostStatus, string> = {
  ok: 'bg-ok', changed: 'bg-changed', failed: 'bg-err',
  unreachable: 'bg-violet', skipped: 'bg-dim', running: 'bg-run',
};
const HOST_LABEL_COLOR: Record<HostStatus, string> = {
  ok: 'text-dim', changed: 'text-changed', failed: 'text-err',
  unreachable: 'text-violet', skipped: 'text-dim', running: 'text-run',
};

const JobDetailPage = () => {
  const { jobId } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const id = Number(jobId);

  const [job, setJob] = useState<Job | null>((location.state as any)?.job || null);
  const [logs, setLogs] = useState('');
  const [logsLoaded, setLogsLoaded] = useState(false);
  const [copied, setCopied] = useState(false);
  const [now, setNow] = useState(Date.now());
  const [highlightTask, setHighlightTask] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const logRef = useRef<HTMLDivElement>(null);
  const sinceRef = useRef<number>(-1);
  const loadingRef = useRef(false);

  const runId = job?.current_run_id || '';
  const isTerminal = job ? TERMINAL_STATES.includes(job.status) : false;
  const isRunning = job ? ACTIVE_STATES.includes(job.status) : false;

  // Pre-render each log line once (Anser is expensive). A TASK line carries its
  // name so a lifecycle event can scroll to and flash it in the output.
  const renderedLines = useMemo(() => logs.split('\n').map((line) => {
    const m = line.match(/^TASK \[(.+?)\]/);
    return {
      task: m ? m[1].toLowerCase() : null,
      html: line ? Anser.ansiToHtml(Anser.escapeForHtml(line), { use_classes: false }) : '&nbsp;',
    };
  }), [logs]);

  const parsed = useMemo(() => parseRun(logs, isRunning), [logs, isRunning]);
  const curTaskIdx = parsed.tasks.length - 1;
  const hostCount = parsed.hostOrder.length;

  const locateTask = useCallback((taskName: string) => {
    const key = taskName.toLowerCase();
    setHighlightTask(key);
    requestAnimationFrame(() => {
      const sel = key.replace(/["\\]/g, '\\$&');
      const el = logRef.current?.querySelector(`[data-task="${sel}"]`) as HTMLElement | null;
      el?.scrollIntoView({ block: 'center', behavior: 'smooth' });
    });
    setTimeout(() => setHighlightTask(null), 2600);
  }, []);

  const loadJob = useCallback(async () => {
    try {
      const jobs: Job[] = unwrap(await api.getJobs());
      const found = jobs.find(j => j.id === id);
      if (found) setJob(found);
    } catch { /* keep whatever we have */ }
  }, [id]);

  const loadLogs = useCallback(async (rid: string) => {
    if (!rid || loadingRef.current) return;
    loadingRef.current = true;
    try {
      const { text, lastSeq } = await api.getJobLogsSince(rid, sinceRef.current);
      if (typeof lastSeq === 'number' && !Number.isNaN(lastSeq)) sinceRef.current = lastSeq;
      if (text) setLogs(prev => prev + text);
      setLogsLoaded(true);
    } catch {
      setLogsLoaded(true);
    } finally {
      loadingRef.current = false;
    }
  }, []);

  useEffect(() => { if (!job) loadJob(); /* eslint-disable-next-line */ }, [id]);

  // Reset and re-tail whenever the run changes (or first resolves).
  useEffect(() => {
    sinceRef.current = -1;
    setLogs('');
    setLogsLoaded(false);
    if (runId) loadLogs(runId);
  }, [runId, loadLogs]);

  // Stream while active; one final fetch on completion for the trailing chunks.
  useEffect(() => {
    if (isTerminal) { if (runId) loadLogs(runId); return; }
    const h = setInterval(() => { loadJob(); if (runId) loadLogs(runId); }, 1200);
    return () => clearInterval(h);
  }, [isTerminal, runId, loadJob, loadLogs]);

  // Live wall-clock for the elapsed timer while the job runs.
  useEffect(() => {
    if (isTerminal) return;
    const h = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(h);
  }, [isTerminal]);

  // Fallback for runs with no object-store output (lifecycle-only / legacy).
  useEffect(() => {
    if (!isTerminal || !logsLoaded || logs || !runId || sinceRef.current >= 0) return;
    api.getJobEvents(runId).then(evs => {
      const t = (evs || []).map((e: any) => e.stdout_snippet).filter(Boolean).join('\n');
      if (t) setLogs(t);
    }).catch(() => { });
  }, [isTerminal, logsLoaded, logs, runId]);

  // Pin to the tail while streaming — unless the user just jumped to a task.
  useEffect(() => {
    if (!isTerminal && !highlightTask && logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [logs, isTerminal, highlightTask]);

  const elapsed = useMemo(() => {
    const start = job?.started_at ? new Date(job.started_at).getTime() : NaN;
    const end = job?.finished_at ? new Date(job.finished_at).getTime() : now;
    if (!Number.isFinite(start)) return '';
    return fmtClock(end - start);
  }, [job?.started_at, job?.finished_at, now]);

  const plain = () => logs.replace(/\x1b\[[0-9;]*m/g, '');
  const copyLogs = async () => {
    await navigator.clipboard.writeText(plain());
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };
  const downloadLogs = () => {
    const blob = new Blob([plain()], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = `job-${id}-logs.txt`;
    document.body.appendChild(a); a.click(); document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  const cancel = async () => {
    if (!job) return;
    if (!(await confirmDialog(`Cancel job "${job.name}"?`, { confirmText: 'Cancel job', destructive: true }))) return;
    setBusy(true);
    try { await api.cancelJob(job.id); toast.info('Cancellation requested'); await loadJob(); }
    catch { toast.error('Failed to cancel job'); }
    finally { setBusy(false); }
  };

  const relaunch = async () => {
    if (!job) return;
    const tmpl = (job as any).unified_job_template_id;
    if (!tmpl) { toast.error('This job has no template to relaunch'); return; }
    setBusy(true);
    try {
      const res = await api.launchJob({ unified_job_template_id: tmpl, name: job.name, relaunch_source_job_id: job.id });
      toast.success('Relaunched');
      const newId = res?.id ?? res?.job?.id;
      if (newId) navigate(`/jobs/${newId}`, { state: { job: res.job ?? res } });
      else navigate('/jobs');
    } catch { toast.error('Failed to relaunch'); }
    finally { setBusy(false); }
  };

  const tone = statusTone(job?.status);
  const lineCount = logs ? logs.split('\n').length : 0;
  // Convergence track: real proportion of host outcomes observed so far.
  const totalOut = parsed.totals.ok + parsed.totals.changed + parsed.totals.failed + parsed.totals.unreachable || 1;
  const pct = (n: number) => `${(n / totalOut) * 100}%`;

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink">
      {/* ── Identity bar ─────────────────────────────────────────────────── */}
      <div className="flex items-center gap-4 px-5 py-3 border-b border-line shrink-0">
        <button
          onClick={() => navigate('/jobs')}
          className="w-7 h-7 grid place-items-center rounded-md border border-line2 text-mut hover:text-ink hover:border-white/20 transition-colors shrink-0"
          title="Back to jobs"
        >
          <ArrowLeft size={15} />
        </button>
        <div className="flex flex-col gap-0.5 min-w-0">
          <div className="flex items-center gap-2.5 min-w-0">
            <span className={`h-[7px] w-[7px] rounded-full shrink-0 ${tone.dot} ${isRunning ? 'animate-pulse' : ''}`} />
            <span className="text-[15px] font-semibold tracking-tight truncate">{job?.name || `Job #${id}`}</span>
            <span className="font-mono text-xs text-dim shrink-0">#{id}</span>
          </div>
          <div className="flex flex-wrap gap-x-3.5 gap-y-0.5 font-mono text-[11px] text-mut">
            <span className={tone.color}>{tone.label}</span>
            {job?.started_at && <span>started <b className="text-ink2 font-medium">{relTime(job.started_at)}</b></span>}
            {runId && <span>run <b className="text-ink2 font-medium">{runId.slice(0, 8)}</b></span>}
          </div>
        </div>
        <div className="ml-auto flex items-center gap-2 shrink-0">
          {isTerminal && (
            <button
              onClick={relaunch} disabled={busy}
              className="h-[30px] px-3 rounded-md text-xs font-semibold flex items-center gap-1.5 border border-line2 text-ink hover:border-white/25 disabled:opacity-50 transition-colors"
            >
              <RotateCcw size={13} /> Relaunch
            </button>
          )}
          {isRunning && (
            <button
              onClick={cancel} disabled={busy}
              className="h-[30px] px-3 rounded-md text-xs font-semibold flex items-center gap-1.5 border border-err/40 text-err/90 hover:bg-err/10 disabled:opacity-50 transition-colors"
            >
              <Square size={12} /> Cancel
            </button>
          )}
        </div>
      </div>

      {/* ── Convergence bar ──────────────────────────────────────────────── */}
      <div className="flex items-center h-10 border-b border-line bg-panel2 shrink-0 font-mono text-[11px]">
        <div className="flex items-center gap-2.5 h-full px-4 border-r border-line">
          <span className={`h-[7px] w-[7px] rounded-full ${tone.dot} ${isRunning ? 'animate-pulse' : ''}`} />
          <span className="text-[10px] uppercase tracking-[0.12em]" style={{ color: 'var(--run)' }}>{isRunning ? 'Running' : tone.label}</span>
          <span className="text-sm text-ink tabular-nums tracking-tight">{elapsed}</span>
        </div>
        <div className="flex items-center gap-1.5 h-full px-4 border-r border-line">
          <span className="text-dim uppercase tracking-[0.08em] text-[10px]">Task</span>
          <span className="text-ink tabular-nums">{curTaskIdx >= 0 ? curTaskIdx + 1 : 0}{parsed.hasRecap ? ` / ${parsed.tasks.length}` : ''}</span>
        </div>
        <div className="flex-1 h-1.5 mx-[18px] rounded bg-line overflow-hidden flex">
          <i className="h-full bg-ok" style={{ width: pct(parsed.totals.ok) }} />
          <i className="h-full bg-changed" style={{ width: pct(parsed.totals.changed) }} />
          <i className="h-full bg-err" style={{ width: pct(parsed.totals.failed) }} />
          <i className="h-full bg-violet" style={{ width: pct(parsed.totals.unreachable) }} />
        </div>
        <div className="flex items-center gap-1.5 h-full px-4 border-l border-line">
          <span className="text-dim uppercase tracking-[0.08em] text-[10px]">Hosts</span>
          <span className="text-ink tabular-nums">{hostCount || '—'}</span>
        </div>
      </div>

      {/* ── Work area: spine | log | hosts ───────────────────────────────── */}
      <div className="grid grid-cols-[250px_1fr_280px] flex-1 min-h-0 max-[1100px]:grid-cols-[210px_1fr] max-[820px]:grid-cols-1">
        {/* Plays & tasks spine */}
        <div className="flex flex-col min-h-0 border-r border-line max-[820px]:hidden">
          <PaneHead title="Plays & tasks" count={`${parsed.plays.length} play${parsed.plays.length === 1 ? '' : 's'}`} />
          <div className="overflow-auto scroll-tint py-1.5">
            {parsed.plays.length === 0 && (
              <p className="text-[11px] text-dim px-4 py-3">{logsLoaded ? 'Waiting for the first play…' : 'Loading…'}</p>
            )}
            {parsed.plays.map((play, pi) => (
              <div key={pi}>
                <div className="font-mono text-[10px] tracking-[0.1em] uppercase text-dim px-4 pt-3 pb-1.5">
                  Play · {play.name} <span className="text-faint">({play.tasks.length})</span>
                </div>
                {play.tasks.map((t, ti) => {
                  const done = Object.keys(t.results).length;
                  const global = parsed.tasks.indexOf(t);
                  const isCur = isRunning && global === curTaskIdx;
                  const failed = Object.values(t.results).some(s => s === 'failed' || s === 'unreachable');
                  const state: HostStatus | 'cur' | 'pend' =
                    isCur ? 'cur' : failed ? 'failed' : done ? 'ok' : 'pend';
                  return (
                    <button
                      key={ti}
                      onClick={() => locateTask(t.name)}
                      className={`w-full flex items-center gap-2.5 px-4 py-1.5 text-left text-[12.5px] transition-colors
                        ${state === 'cur' ? 'text-[#cfe0ff] bg-run/[0.06] shadow-[inset_2px_0_0_var(--run)]'
                          : state === 'pend' ? 'text-faint hover:text-mut'
                          : 'text-ink2 hover:bg-white/[0.02]'}`}
                    >
                      <TaskTick state={state} />
                      <span className="flex-1 truncate">{t.name}</span>
                      <span className="font-mono text-[10.5px] text-dim tabular-nums shrink-0">
                        {done ? `${done}/${hostCount || done}` : '—'}
                      </span>
                    </button>
                  );
                })}
              </div>
            ))}
          </div>
        </div>

        {/* Live log */}
        <div className="flex flex-col min-h-0" style={{ background: '#070809' }}>
          <div className="flex items-center gap-2.5 h-[42px] px-4 border-b border-line bg-panel2 shrink-0">
            <span className="font-mono text-[10px] tracking-[0.14em] uppercase text-mut">Output</span>
            {isRunning && (
              <span className="flex items-center gap-1.5 font-mono text-[10px] uppercase tracking-[0.08em]" style={{ color: 'var(--run)' }}>
                <span className="h-1.5 w-1.5 rounded-full bg-run animate-pulse" /> live
              </span>
            )}
            <span className="ml-auto flex items-center gap-1">
              <button onClick={copyLogs} className="p-1.5 rounded text-mut hover:text-ink hover:bg-white/5 transition-colors" title="Copy logs">
                {copied ? <Check size={15} className="text-ok" /> : <Copy size={15} />}
              </button>
              <button onClick={downloadLogs} className="p-1.5 rounded text-mut hover:text-ink hover:bg-white/5 transition-colors" title="Download logs">
                <Download size={15} />
              </button>
            </span>
          </div>
          <div
            ref={logRef}
            className="flex-1 overflow-auto scroll-tint font-mono text-[12px] leading-[1.65] py-2.5 px-0"
            style={{ fontFamily: 'var(--mono)' }}
          >
            {logs ? renderedLines.map((ln, i) => (
              <div
                key={i}
                data-task={ln.task || undefined}
                className={`px-4 whitespace-pre-wrap break-words ${ln.task && highlightTask === ln.task ? 'bg-changed/20 transition-colors duration-300' : ''}`}
                dangerouslySetInnerHTML={{ __html: ln.html }}
              />
            )) : (
              <p className="px-4 text-dim">{isTerminal ? 'No output.' : (logsLoaded ? 'Waiting for output…' : 'Loading…')}</p>
            )}
          </div>
          <div className="flex items-center justify-between px-4 py-1 border-t border-line bg-panel2 font-mono text-[10.5px] text-dim shrink-0">
            <span className="tabular-nums">{lineCount} lines</span>
            <span>{runId ? `run ${runId.slice(0, 8)} · ` : ''}UTF-8</span>
          </div>
        </div>

        {/* Hosts + lifecycle narration */}
        <div className="flex flex-col min-h-0 border-l border-line overflow-auto scroll-tint max-[1100px]:hidden">
          <PaneHead title="Hosts" count={hostCount ? `${hostCount} target${hostCount === 1 ? '' : 's'}` : '—'} />
          <div className="grid grid-cols-2 border-b border-line">
            <HostStat n={parsed.totals.ok} label="OK" color="text-ok" />
            <HostStat n={parsed.totals.changed} label="Changed" color="text-changed" />
            <HostStat n={parsed.totals.failed} label="Failed" color="text-err" />
            <HostStat n={parsed.totals.unreachable} label="Unreachable" color="text-violet" />
          </div>
          {parsed.hostOrder.map(h => {
            const s = parsed.hosts[h];
            return (
              <div key={h} className="flex items-center gap-2.5 px-4 py-2 border-b border-line font-mono text-[12px]">
                <span className={`h-[7px] w-[7px] rounded-full shrink-0 ${HOST_DOT[s]} ${s === 'running' ? 'ring-[3px] ring-run/20' : ''}`} />
                <span className="flex-1 text-ink truncate">{h}</span>
                <span className={`text-[10.5px] uppercase tracking-[0.05em] ${HOST_LABEL_COLOR[s]}`}>{s}</span>
              </div>
            );
          })}
          {hostCount === 0 && <p className="text-[11px] text-dim px-4 py-3">No host results yet.</p>}

          {/* Engine narration — Praetor's real differentiator (agentless boot,
              checkpoint/resume), polled from the run's lifecycle events. */}
          {runId && (
            <div className="px-4 py-3 border-t border-line mt-auto">
              <div className="font-mono text-[9.5px] tracking-[0.12em] uppercase text-mut mb-2.5">Execution engine</div>
              <RunLifecycle runId={runId} dark onLocate={locateTask} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

const PaneHead: React.FC<{ title: string; count: string }> = ({ title, count }) => (
  <div className="flex items-center justify-between h-[42px] px-4 border-b border-line shrink-0">
    <span className="font-mono text-[10px] tracking-[0.14em] uppercase text-mut">{title}</span>
    <span className="font-mono text-[10.5px] text-dim">{count}</span>
  </div>
);

const HostStat: React.FC<{ n: number; label: string; color: string }> = ({ n, label, color }) => (
  <div className="px-4 py-2.5 border-r border-b border-line last:border-r-0">
    <div className={`font-mono text-lg font-semibold tabular-nums ${n ? color : 'text-faint'}`}>{n}</div>
    <div className="font-mono text-[9.5px] tracking-[0.08em] uppercase text-mut mt-0.5">{label}</div>
  </div>
);

const TaskTick: React.FC<{ state: HostStatus | 'cur' | 'pend' }> = ({ state }) => {
  if (state === 'cur') return <span className="w-3.5 h-3.5 grid place-items-center shrink-0"><span className="w-[11px] h-[11px] rounded-full border-2 border-run/30 border-t-run animate-spin" /></span>;
  if (state === 'failed') return <XCircle size={13} className="text-err shrink-0" strokeWidth={2.4} />;
  if (state === 'unreachable') return <AlertTriangle size={13} className="text-violet shrink-0" strokeWidth={2.4} />;
  if (state === 'pend') return <Circle size={13} className="text-faint shrink-0" strokeWidth={2.4} />;
  return <CheckCircle2 size={13} className="text-ok shrink-0" strokeWidth={2.4} />;
};

export default JobDetailPage;
