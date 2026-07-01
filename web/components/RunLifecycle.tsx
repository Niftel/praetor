import React, { useEffect, useRef, useState } from 'react';
import { api } from '../services/api';
import { Zap, Play, Flag, RotateCcw, CheckCircle2, XCircle, Loader } from 'lucide-react';

// The engine-narration event types, in the order they occur. These surface
// Praetor's differentiators — agentless bootstrap and checkpoint/resume — on the
// run timeline. Bulk task output is deliberately excluded; this is the story of
// the run, not its logs.
type Kind =
  | 'RUNNER_ONLINE'
  | 'RESUMED_FROM_CHECKPOINT'
  | 'JOB_STARTED'
  | 'CHECKPOINT_SAVED'
  | 'JOB_COMPLETED'
  | 'JOB_FAILED';

const TERMINAL: Kind[] = ['JOB_COMPLETED', 'JOB_FAILED'];

interface JobEvent {
  seq: number;
  event_type: string;
  stdout_snippet?: string;
  task_name?: string;
  created_at: string;
}

const META: Record<Kind, { label: string; icon: React.ReactNode; accent: string; ring: string }> = {
  RUNNER_ONLINE: { label: 'Runner online · agentless', icon: <Zap size={14} />, accent: 'text-emerald-600', ring: 'bg-emerald-100 text-emerald-700' },
  JOB_STARTED: { label: 'Job started', icon: <Play size={14} />, accent: 'text-blue-600', ring: 'bg-blue-100 text-blue-700' },
  CHECKPOINT_SAVED: { label: 'Checkpoint saved', icon: <Flag size={14} />, accent: 'text-sky-600', ring: 'bg-sky-100 text-sky-700' },
  RESUMED_FROM_CHECKPOINT: { label: 'Resumed from checkpoint', icon: <RotateCcw size={14} />, accent: 'text-amber-600', ring: 'bg-amber-100 text-amber-700' },
  JOB_COMPLETED: { label: 'Completed', icon: <CheckCircle2 size={14} />, accent: 'text-emerald-600', ring: 'bg-emerald-100 text-emerald-700' },
  JOB_FAILED: { label: 'Failed', icon: <XCircle size={14} />, accent: 'text-red-600', ring: 'bg-red-100 text-red-700' },
};

// Dark-mode ring colours for the terminal modal, keyed the same way.
const DARK_RING: Record<Kind, string> = {
  RUNNER_ONLINE: 'bg-emerald-500/15 text-emerald-400',
  JOB_STARTED: 'bg-blue-500/15 text-blue-400',
  CHECKPOINT_SAVED: 'bg-sky-500/15 text-sky-400',
  RESUMED_FROM_CHECKPOINT: 'bg-amber-500/15 text-amber-400',
  JOB_COMPLETED: 'bg-emerald-500/15 text-emerald-400',
  JOB_FAILED: 'bg-red-500/15 text-red-400',
};

const isKind = (t: string): t is Kind => t in META;

const timeStr = (iso: string) => {
  try { return new Date(iso).toLocaleTimeString(); } catch { return ''; }
};

interface Props {
  runId?: string;
  dark?: boolean;
}

// RunLifecycle renders the engine-narration timeline for a single execution run.
// It polls the run's events until a terminal event appears, then stops.
const RunLifecycle: React.FC<Props> = ({ runId, dark = false }) => {
  const [events, setEvents] = useState<JobEvent[]>([]);
  const [loaded, setLoaded] = useState(false);
  const doneRef = useRef(false);

  useEffect(() => {
    if (!runId) return;
    doneRef.current = false;
    let active = true;

    const load = async () => {
      try {
        const all: JobEvent[] = (await api.getJobEvents(runId)) || [];
        if (!active) return;
        const lifecycle = all.filter(e => isKind(e.event_type)).sort((a, b) => a.seq - b.seq);
        setEvents(lifecycle);
        setLoaded(true);
        if (lifecycle.some(e => TERMINAL.includes(e.event_type as Kind))) doneRef.current = true;
      } catch {
        if (active) setLoaded(true);
      }
    };

    load();
    const h = setInterval(() => { if (!doneRef.current) load(); }, 2500);
    return () => { active = false; clearInterval(h); };
  }, [runId]);

  if (!runId) return null;

  const muted = dark ? 'text-gray-500' : 'text-gray-400';
  const title = dark ? 'text-gray-300' : 'text-gray-900';
  const sub = dark ? 'text-gray-400' : 'text-gray-500';
  const rail = dark ? 'bg-white/10' : 'bg-gray-200';

  if (loaded && events.length === 0) {
    return <p className={`text-xs ${muted} px-1 py-2`}>No lifecycle events yet.</p>;
  }
  if (!loaded) {
    return (
      <div className={`flex items-center gap-2 text-xs ${muted} px-1 py-2`}>
        <Loader size={12} className="animate-spin" /> Loading lifecycle…
      </div>
    );
  }

  return (
    <ol className="relative space-y-3">
      {/* connecting rail */}
      <span className={`absolute left-[11px] top-1 bottom-1 w-px ${rail}`} aria-hidden="true" />
      {events.map((e, i) => {
        const kind = e.event_type as Kind;
        const meta = META[kind];
        const ring = dark ? DARK_RING[kind] : meta.ring;
        return (
          <li key={e.seq ?? i} className="relative flex items-start gap-3 pl-0">
            <span className={`relative z-10 flex h-6 w-6 items-center justify-center rounded-full ${ring}`}>
              {meta.icon}
            </span>
            <div className="min-w-0 flex-1 -mt-0.5">
              <div className="flex items-baseline justify-between gap-2">
                <span className={`text-sm font-medium ${dark ? meta.accent.replace('600', '400') : meta.accent}`}>{meta.label}</span>
                <span className={`text-[11px] tabular-nums ${muted} shrink-0`}>{timeStr(e.created_at)}</span>
              </div>
              {e.stdout_snippet && <p className={`text-xs ${sub} mt-0.5 break-words`}>{e.stdout_snippet}</p>}
            </div>
          </li>
        );
      })}
    </ol>
  );
};

export default RunLifecycle;
