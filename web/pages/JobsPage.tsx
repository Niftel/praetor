import React, { useState, useEffect, useMemo, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Job, Template } from '../types';
import { toast, confirmDialog } from '../components/ui/toast';
import { Play, Square, ChevronDown } from 'lucide-react';

const TERMINAL = ['successful', 'failed', 'error', 'canceled'];
const ACTIVE = ['pending', 'queued', 'running', 'waiting'];
const isActive = (s: string) => ACTIVE.includes(s);
const isFail = (s: string) => s === 'failed' || s === 'error';

type Filter = 'all' | 'running' | 'failed' | 'converged';

const tone = (s: string): { label: string; text: string; dot: string } => {
  if (s === 'successful') return { label: 'successful', text: 'text-ok', dot: 'bg-ok' };
  if (isFail(s)) return { label: s, text: 'text-err', dot: 'bg-err' };
  if (s === 'canceled') return { label: 'canceled', text: 'text-mut', dot: 'bg-mut' };
  if (isActive(s)) return { label: s || 'running', text: 'text-run', dot: 'bg-run' };
  return { label: s || '—', text: 'text-mut', dot: 'bg-mut' };
};

const rel = (iso?: string, ref?: number): string => {
  if (!iso) return '';
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return '';
  const s = Math.floor(((ref ?? Date.now()) - t) / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
};

const clock = (ms: number): string => {
  if (!Number.isFinite(ms) || ms < 0) return '—';
  const s = Math.floor(ms / 1000);
  const pad = (n: number) => String(n).padStart(2, '0');
  const h = Math.floor(s / 3600);
  return h > 0 ? `${h}:${pad(Math.floor(s / 60) % 60)}:${pad(s % 60)}` : `${pad(Math.floor(s / 60))}:${pad(s % 60)}`;
};

const dur = (job: Job, now: number): string => {
  if (!job.started_at) return '—';
  const start = new Date(job.started_at).getTime();
  const end = job.finished_at ? new Date(job.finished_at).getTime() : (isActive(job.status) ? now : NaN);
  if (!Number.isFinite(end)) return '—';
  return clock(end - start);
};

const startClock = (iso?: string) => {
  if (!iso) return '—';
  try { return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }); } catch { return '—'; }
};

const JobsPage = () => {
  const navigate = useNavigate();
  const [jobs, setJobs] = useState<Job[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState('');
  const [filter, setFilter] = useState<Filter>('all');
  const [now, setNow] = useState(Date.now());
  const launching = useRef(false);

  const loadData = () => {
    Promise.all([api.getJobs(), api.getTemplates()])
      .then(([jobsData, templatesData]) => {
        setJobs(unwrap(jobsData));
        setTemplates(unwrap(templatesData));
      })
      .catch(err => console.error(err));
  };

  useEffect(() => {
    loadData();
    const poll = setInterval(loadData, 5000);
    const tick = setInterval(() => setNow(Date.now()), 1000);
    return () => { clearInterval(poll); clearInterval(tick); };
  }, []);

  const counts = useMemo(() => {
    let running = 0, failed = 0, converged = 0, terminal = 0;
    for (const j of jobs) {
      if (isActive(j.status)) running++;
      else if (isFail(j.status)) { failed++; terminal++; }
      else if (j.status === 'successful') { converged++; terminal++; }
      else terminal++;
    }
    return { total: jobs.length, running, failed, converged, terminal };
  }, [jobs]);

  const convergedPct = counts.terminal ? Math.round((counts.converged / counts.terminal) * 100) : 0;

  // 48-hour hourly histogram from real start times; red where a run failed.
  const hist = useMemo(() => {
    const buckets = Array.from({ length: 48 }, () => ({ ok: 0, err: 0 }));
    const nowH = Math.floor(now / 3.6e6);
    let failed = 0;
    for (const j of jobs) {
      const iso = j.started_at || j.created_at;
      if (!iso) continue;
      const h = Math.floor(new Date(iso).getTime() / 3.6e6);
      const idx = 47 - (nowH - h);
      if (idx < 0 || idx > 47) continue;
      if (isFail(j.status)) { buckets[idx].err++; failed++; } else buckets[idx].ok++;
    }
    const max = Math.max(1, ...buckets.map(b => b.ok + b.err));
    return { buckets, max, failed };
  }, [jobs, now]);

  const shown = useMemo(() => jobs.filter(j => {
    if (filter === 'running') return isActive(j.status);
    if (filter === 'failed') return isFail(j.status);
    if (filter === 'converged') return j.status === 'successful';
    return true;
  }), [jobs, filter]);

  const handleLaunch = async () => {
    if (!selectedTemplate || launching.current) return;
    const t = templates.find(t => t.id.toString() === selectedTemplate);
    if (!t) return;
    launching.current = true;
    try {
      await api.launchJob({ unified_job_template_id: (t as any).unified_job_template_id || t.id, name: t.name });
      setSelectedTemplate('');
      loadData();
    } catch { toast.error('Failed to launch job'); }
    finally { launching.current = false; }
  };

  const cancel = async (e: React.MouseEvent, job: Job) => {
    e.stopPropagation();
    if (!(await confirmDialog(`Cancel job "${job.name}"?`, { confirmText: 'Cancel job', destructive: true }))) return;
    try { await api.cancelJob(job.id); toast.info('Cancellation requested'); loadData(); }
    catch { toast.error('Failed to cancel job'); }
  };

  const openJob = (job: Job) => navigate(`/jobs/${job.id}`, { state: { job } });

  const chips: { key: Filter; label: string; n: number }[] = [
    { key: 'all', label: 'All', n: counts.total },
    { key: 'running', label: 'Running', n: counts.running },
    { key: 'failed', label: 'Failed', n: counts.failed },
    { key: 'converged', label: 'Converged', n: counts.converged },
  ];

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink">
      {/* Header: title, readout, 48h histogram */}
      <div className="flex flex-wrap gap-x-7 gap-y-4 items-start px-6 pt-5 pb-1 shrink-0">
        <div>
          <div className="flex items-baseline gap-3">
            <h1 className="text-[19px] font-semibold tracking-tight">Jobs</h1>
            <span className="font-mono text-xs text-dim">{counts.total} shown · {counts.running} active</span>
          </div>
          <div className="flex gap-6 mt-3">
            <Readout n={counts.total} label="In view" />
            <Readout n={`${convergedPct}%`} label="Converged" color="text-ok" />
            <Readout n={counts.failed} label="Failed" color={counts.failed ? 'text-err' : undefined} />
            <Readout n={counts.running} label="Running" color={counts.running ? 'text-run' : undefined} />
          </div>
        </div>
        <div className="ml-auto w-[340px] max-[720px]:w-full max-[720px]:ml-0">
          <div className="flex justify-between font-mono text-[9.5px] tracking-[0.1em] uppercase text-dim mb-2">
            <span>Runs · last 48h</span>
            <span>{hist.failed} failed</span>
          </div>
          <div className="flex items-end gap-[2px] h-[46px]">
            {hist.buckets.map((b, i) => (
              <div key={i} className="flex-1 flex flex-col justify-end gap-px h-full" title={b.ok + b.err ? `${b.ok + b.err} run(s)` : ''}>
                {b.err > 0 && <i className="w-full rounded-sm bg-err" style={{ height: `${(b.err / hist.max) * 100}%` }} />}
                {b.ok > 0 && <i className="w-full rounded-sm bg-ok/75" style={{ height: `${(b.ok / hist.max) * 100}%` }} />}
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Toolbar: filter chips + launch */}
      <div className="flex items-center gap-1.5 px-6 py-3 shrink-0">
        {chips.map(c => (
          <button
            key={c.key}
            onClick={() => setFilter(c.key)}
            className={`font-mono text-[11px] px-2.5 py-1.5 rounded-md border transition-colors
              ${filter === c.key ? 'text-ink bg-white/5 border-line' : 'text-mut border-transparent hover:text-ink'}`}
          >
            {c.label} <span className="text-dim ml-1 tabular-nums">{c.n}</span>
          </button>
        ))}
        <div className="ml-auto flex items-center gap-2">
          <div className="relative">
            <select
              aria-label="Launch template"
              value={selectedTemplate}
              onChange={e => setSelectedTemplate(e.target.value)}
              className="appearance-none h-[30px] pl-3 pr-8 rounded-md bg-panel border border-line2 text-xs text-ink2 font-mono hover:border-white/20 focus:outline-none focus:border-acc/60 max-w-[220px]"
            >
              <option value="">Select template…</option>
              {templates.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
            </select>
            <ChevronDown size={13} className="absolute right-2.5 top-1/2 -translate-y-1/2 text-dim pointer-events-none" />
          </div>
          <button
            onClick={handleLaunch}
            disabled={!selectedTemplate}
            className="h-[30px] px-3.5 rounded-md text-xs font-semibold flex items-center gap-1.5 bg-acc text-[#04211d] hover:bg-acc2 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            <Play size={13} strokeWidth={2.4} /> Launch job
          </button>
        </div>
      </div>

      {/* Table */}
      <div className="grid grid-cols-[130px_60px_1fr_150px_120px_100px] items-center px-6 h-[34px] border-y border-line shrink-0 font-mono text-[9.5px] tracking-[0.1em] uppercase text-dim max-[720px]:grid-cols-[110px_1fr_90px]">
        <span>Status</span>
        <span className="max-[720px]:hidden">ID</span>
        <span>Name</span>
        <span className="max-[720px]:hidden">Started</span>
        <span className="max-[720px]:hidden">Duration</span>
        <span className="text-right">Run</span>
      </div>
      <div className="flex-1 overflow-auto scroll-tint">
        {shown.map(job => {
          const t = tone(job.status);
          return (
            <div
              key={job.id}
              onClick={() => openJob(job)}
              className="grid grid-cols-[130px_60px_1fr_150px_120px_100px] items-center px-6 h-[46px] border-b border-line cursor-pointer hover:bg-white/[0.025] transition-colors max-[720px]:grid-cols-[110px_1fr_90px]"
            >
              <span className={`inline-flex items-center gap-2 text-[12px] ${t.text}`}>
                <span className={`h-[7px] w-[7px] rounded-full ${t.dot} ${isActive(job.status) ? 'animate-pulse' : ''}`} />
                {t.label}
              </span>
              <span className="font-mono text-xs text-mut tabular-nums max-[720px]:hidden">#{job.id}</span>
              <span className="font-medium truncate pr-4">{job.name}</span>
              <span className="font-mono text-[11px] text-mut tabular-nums max-[720px]:hidden">
                {startClock(job.started_at)} <span className="text-dim">{job.started_at ? rel(job.started_at, now) : ''}</span>
              </span>
              <span className="font-mono text-[11px] text-mut tabular-nums max-[720px]:hidden">{dur(job, now)}</span>
              <div className="flex justify-end" onClick={e => e.stopPropagation()}>
                {isActive(job.status) ? (
                  <button
                    onClick={e => cancel(e, job)}
                    className="inline-flex items-center gap-1 text-[11px] font-medium text-err/90 hover:text-err"
                    title="Cancel this job"
                  >
                    <Square size={11} /> Cancel
                  </button>
                ) : (
                  <button onClick={() => openJob(job)} className="text-[11px] font-medium text-acc/80 hover:text-acc">View →</button>
                )}
              </div>
            </div>
          );
        })}
        {shown.length === 0 && (
          <div className="px-6 py-10 text-center text-sm text-dim">
            {filter === 'all' ? 'No jobs yet.' : `No ${filter} jobs.`}
          </div>
        )}
      </div>
    </div>
  );
};

const Readout: React.FC<{ n: React.ReactNode; label: string; color?: string }> = ({ n, label, color }) => (
  <div className="flex flex-col gap-0.5">
    <span className={`font-mono text-[19px] font-semibold tracking-tight tabular-nums ${color || 'text-ink'}`}>{n}</span>
    <span className="text-[10px] uppercase tracking-[0.07em] text-mut">{label}</span>
  </div>
);

export default JobsPage;
