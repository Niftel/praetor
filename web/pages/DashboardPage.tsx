import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../services/api';
import { UnifiedJob, Schedule } from '../types';

// Operator mission-control board: a readout ribbon, then the past→present→future
// spine (running now / recent runs) with a compact 7-day trend. No hero-metric
// tiles. Wired to /jobs and /schedules.

const RUNNING = ['running', 'waiting'];
const ACTIVE = ['running', 'waiting', 'pending', 'queued'];

function rel(iso?: string) {
  if (!iso) return '—';
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return `${Math.floor(s)}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}
function dur(a?: string, b?: string) {
  if (!a) return '—';
  const end = b ? new Date(b).getTime() : Date.now();
  let s = Math.max(0, Math.floor((end - new Date(a).getTime()) / 1000));
  const m = Math.floor(s / 60); s = s % 60;
  return `${m}m${String(s).padStart(2, '0')}s`;
}
function elapsed(a?: string) {
  if (!a) return '00:00';
  let s = Math.max(0, Math.floor((Date.now() - new Date(a).getTime()) / 1000));
  const m = Math.floor(s / 60); s = s % 60;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

const DashboardPage: React.FC = () => {
  const navigate = useNavigate();
  const [jobs, setJobs] = useState<UnifiedJob[]>([]);
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    const load = () => {
      api.getJobs().then(d => setJobs(d || [])).catch(() => {});
      api.getSchedules().then(d => setSchedules(d || [])).catch(() => {});
    };
    load();
    const h = setInterval(load, 5000);
    return () => clearInterval(h);
  }, []);
  // re-render clock for elapsed timers
  useEffect(() => { const h = setInterval(() => setTick(t => t + 1), 1000); return () => clearInterval(h); }, []);

  const stats = useMemo(() => {
    const now = Date.now();
    const dayStart = new Date(); dayStart.setHours(0, 0, 0, 0);
    const in24 = (j: UnifiedJob) => j.finished_at && now - new Date(j.finished_at).getTime() < 86400000;
    const today = (j: UnifiedJob) => j.started_at && new Date(j.started_at).getTime() >= dayStart.getTime();
    const running = jobs.filter(j => RUNNING.includes(j.status));
    const active = jobs.filter(j => ACTIVE.includes(j.status));
    const jobsToday = jobs.filter(today).length;
    const succ24 = jobs.filter(j => in24(j) && j.status === 'successful').length;
    const fail24 = jobs.filter(j => in24(j) && (j.status === 'failed' || j.status === 'error')).length;
    const rate = succ24 + fail24 > 0 ? Math.round((succ24 / (succ24 + fail24)) * 100) : 100;
    const failedToday = jobs.filter(j => today(j) && (j.status === 'failed' || j.status === 'error')).length;
    const recent = jobs.filter(j => !ACTIVE.includes(j.status)).slice(0, 6);
    return { running, active, jobsToday, rate, failedToday, recent };
  }, [jobs]);

  const trend = useMemo(() => {
    const days: { name: string; ok: number; fail: number }[] = [];
    const byKey = new Map<string, { ok: number; fail: number }>();
    const now = new Date();
    for (let i = 6; i >= 0; i--) {
      const d = new Date(now); d.setDate(now.getDate() - i);
      const key = d.toISOString().slice(0, 10);
      const o = { ok: 0, fail: 0 };
      byKey.set(key, o);
      days.push({ name: d.toLocaleDateString(undefined, { weekday: 'short' }), ...o });
    }
    for (const j of jobs) {
      if (!j.started_at) continue;
      const b = byKey.get(new Date(j.started_at).toISOString().slice(0, 10));
      if (!b) continue;
      if (j.status === 'successful') b.ok++;
      else if (j.status === 'failed' || j.status === 'error') b.fail++;
    }
    return days.map((d, i) => ({ ...d, ...[...byKey.values()][i] }));
  }, [jobs]);
  const maxBar = Math.max(1, ...trend.map(d => d.ok + d.fail));

  // upcoming schedules within 6h, rotating
  const upcoming = useMemo(() => {
    const now = Date.now();
    return schedules
      .filter(s => s.enabled && s.next_run)
      .map(s => ({ s, t: new Date(s.next_run).getTime() }))
      .filter(x => x.t > now)
      .sort((a, b) => a.t - b.t);
  }, [schedules]);
  const soon6 = upcoming.filter(x => x.t - Date.now() < 6 * 3600000);
  const rotating = soon6.length ? soon6 : upcoming.slice(0, 1);
  const [rotIdx, setRotIdx] = useState(0);
  useEffect(() => {
    if (rotating.length < 2) return;
    const h = setInterval(() => setRotIdx(i => (i + 1) % rotating.length), 2800);
    return () => clearInterval(h);
  }, [rotating.length]);
  const nextUp = rotating[rotIdx % Math.max(1, rotating.length)];

  return (
    <div className="p-[24px_30px_50px]" style={{ padding: '24px 30px 50px' }}>
      {/* readout ribbon */}
      <div className="flex items-stretch border border-line rounded-[12px] bg-panel2 overflow-hidden mb-[22px]">
        <Seg label="Running now"><span className="text-run flex items-center gap-2.5"><Dot cls="bg-run ring-run" pulse /> {stats.running.length}</span></Seg>
        <Seg label="Jobs today"><span className="text-ink">{stats.jobsToday}</span></Seg>
        <Seg label="Success rate · 24h">
          <span className="text-ink">{stats.rate}<span className="text-[12px] text-dim font-normal">%</span></span>
          <span className="w-[96px] h-[5px] rounded-[3px] bg-line overflow-hidden flex mt-1">
            <i className="h-full bg-ok" style={{ width: `${stats.rate}%` }} /><i className="h-full bg-err" style={{ width: `${100 - stats.rate}%` }} />
          </span>
        </Seg>
        <Seg label="Failed today"><span className="text-err">{stats.failedToday}</span></Seg>
        <div className="p-[15px_22px] ml-auto text-right flex flex-col gap-1.5 items-end">
          <span className="font-mono text-[9px] tracking-[0.13em] uppercase text-dim">Upcoming · next 6h</span>
          <div className="relative h-[19px] min-w-[220px] overflow-hidden">
            {nextUp ? (
              <div key={nextUp.s.id} className="font-mono text-[12.5px] text-ink2 whitespace-nowrap dash-rise">
                <span className="text-ink">{new Date(nextUp.s.next_run).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
                {' · '}{nextUp.s.name}
                <span className="text-dim"> · {rel(nextUp.s.next_run).replace(' ago', '')}</span>
              </div>
            ) : <span className="font-mono text-[11px] text-dim">nothing scheduled</span>}
          </div>
        </div>
      </div>

      <div className="grid grid-cols-[1.65fr_1fr] gap-5 items-start max-[1000px]:grid-cols-1">
        {/* spine */}
        <div>
          <Panel title="Running now" count={`${stats.running.length} active`} dot="bg-run" onAll={() => navigate('/jobs')} allLabel="jobs →">
            {stats.running.length === 0 && <Empty>Nothing running right now.</Empty>}
            {stats.running.map(j => (
              <div key={j.id} onClick={() => navigate(`/jobs/${j.id}`)} className="flex items-center gap-[15px] p-[14px_17px] border-b border-line last:border-0 cursor-pointer hover:bg-white/[.02]">
                <span className="font-mono text-[12px] text-run min-w-[42px]">#{j.id}</span>
                <div className="flex-1 min-w-0">
                  <div className="font-mono text-[12.5px] text-ink truncate">{j.name || `job #${j.id}`}</div>
                  <div className="font-mono text-[10.5px] text-dim mt-1">{j.status} · started {rel(j.started_at || j.created_at)}</div>
                </div>
                <span className="font-mono text-[12px] text-ink2 min-w-[52px] text-right tabular-nums">{elapsed(j.started_at)}</span>
              </div>
            ))}
          </Panel>

          <div className="mt-5">
            <Panel title="Recent runs" onAll={() => navigate('/jobs')} allLabel="history →">
              {stats.recent.length === 0 && <Empty>No finished runs yet.</Empty>}
              {stats.recent.map(j => (
                <div key={j.id} onClick={() => navigate(`/jobs/${j.id}`)} className="grid grid-cols-[16px_40px_1fr_auto] items-center gap-[13px] p-[11px_17px] border-b border-line last:border-0 cursor-pointer hover:bg-white/[.02]">
                  <StatusDot status={j.status} />
                  <span className="font-mono text-[12px] text-mut">#{j.id}</span>
                  <div className="min-w-0">
                    <div className="font-mono text-[12.5px] text-ink2 truncate">{j.name || `job #${j.id}`}</div>
                    <div className="font-mono text-[10.5px] text-dim mt-0.5">{dur(j.started_at, j.finished_at)} · {j.status}</div>
                  </div>
                  <span className="font-mono text-[11px] text-dim text-right whitespace-nowrap">{rel(j.finished_at || j.started_at)}</span>
                </div>
              ))}
            </Panel>
          </div>
        </div>

        {/* trend */}
        <div>
          <div className="border border-line rounded-[12px] bg-panel2 overflow-hidden">
            <div className="flex items-center gap-2.5 p-[13px_17px] border-b border-line">
              <span className="font-mono text-[10px] tracking-[0.14em] uppercase text-mut">Execution · 7 days</span>
            </div>
            <div className="p-[18px_17px_14px]">
              <div className="flex items-end gap-3 h-[96px]">
                {trend.map((d, i) => (
                  <div key={i} className="flex-1 flex flex-col justify-end gap-[3px] h-full">
                    <div className="flex flex-col-reverse gap-[2px] rounded-t-[3px] overflow-hidden">
                      <div className="bg-ok" style={{ height: `${(d.ok / maxBar) * 84}px` }} />
                      {d.fail > 0 && <div className="bg-err" style={{ height: `${(d.fail / maxBar) * 84}px` }} />}
                    </div>
                  </div>
                ))}
              </div>
              <div className="flex gap-3 mt-1.5">
                {trend.map((d, i) => <div key={i} className="flex-1 font-mono text-[9.5px] text-faint text-center">{d.name}</div>)}
              </div>
              <div className="flex gap-[18px] mt-3 font-mono text-[10px] text-dim">
                <span className="flex items-center gap-1.5"><i className="w-2 h-2 rounded-[2px] bg-ok" />successful</span>
                <span className="flex items-center gap-1.5"><i className="w-2 h-2 rounded-[2px] bg-err" />failed</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <style>{`@keyframes dashRise{from{opacity:0;transform:translateY(9px)}to{opacity:1;transform:translateY(0)}}
        .dash-rise{animation:dashRise .45s cubic-bezier(.2,.85,.25,1)}
        @media (prefers-reduced-motion:reduce){.dash-rise{animation:none}}`}</style>
    </div>
  );
};

const Seg: React.FC<{ label: string; children: React.ReactNode }> = ({ label, children }) => (
  <div className="p-[15px_22px] border-r border-line flex flex-col gap-1.5 min-w-0">
    <span className="font-mono text-[9px] tracking-[0.13em] uppercase text-dim">{label}</span>
    <span className="font-mono text-[20px] font-semibold tabular-nums tracking-[-.01em]">{children}</span>
  </div>
);

const Panel: React.FC<{ title: string; count?: string; dot?: string; allLabel?: string; onAll?: () => void; children: React.ReactNode }> = ({ title, count, dot, allLabel, onAll, children }) => (
  <div className="border border-line rounded-[12px] bg-panel2 overflow-hidden">
    <div className="flex items-center gap-2.5 p-[13px_17px] border-b border-line">
      {dot && <span className={`w-[7px] h-[7px] rounded-full ${dot}`} />}
      <span className="font-mono text-[10px] tracking-[0.14em] uppercase text-mut">{title}</span>
      {count && <span className="font-mono text-[10.5px] text-faint">{count}</span>}
      {allLabel && <span onClick={onAll} className="ml-auto font-mono text-[10.5px] text-dim hover:text-acc cursor-pointer">{allLabel}</span>}
    </div>
    {children}
  </div>
);

const Empty: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <div className="p-[26px_17px] text-center text-dim text-[12.5px]">{children}</div>
);

const Dot: React.FC<{ cls: string; pulse?: boolean }> = ({ cls, pulse }) => (
  <span className={`w-[7px] h-[7px] rounded-full flex-none ${cls.split(' ')[0]} ${pulse ? 'ring-[3px] ring-run/[.16]' : ''}`} />
);

const StatusDot: React.FC<{ status: string }> = ({ status }) => {
  const c = status === 'successful' ? 'bg-ok' : (status === 'failed' || status === 'error') ? 'bg-err'
    : ['running', 'waiting'].includes(status) ? 'bg-run' : 'bg-faint';
  return <span className={`w-[7px] h-[7px] rounded-full ${c}`} />;
};

export default DashboardPage;
