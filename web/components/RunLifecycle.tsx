import React, { useEffect, useRef, useState } from 'react';
import { api } from '../services/api';
import { Zap, Play, Flag, RotateCcw, CheckCircle2, XCircle, Loader, ChevronDown, ChevronRight, CornerDownRight } from 'lucide-react';

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
  event_data?: any;
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

// Dark-mode ring colours, keyed the same way.
const DARK_RING: Record<Kind, string> = {
  RUNNER_ONLINE: 'bg-emerald-500/15 text-emerald-400',
  JOB_STARTED: 'bg-blue-500/15 text-blue-400',
  CHECKPOINT_SAVED: 'bg-sky-500/15 text-sky-400',
  RESUMED_FROM_CHECKPOINT: 'bg-amber-500/15 text-amber-400',
  JOB_COMPLETED: 'bg-emerald-500/15 text-emerald-400',
  JOB_FAILED: 'bg-red-500/15 text-red-400',
};

const isKind = (t: string): t is Kind => t in META;
const timeStr = (iso: string) => { try { return new Date(iso).toLocaleTimeString(); } catch { return ''; } };

// Events that map to a concrete task in the log (their task_name is a real
// ansible task), so clicking them can jump to that TASK in the output.
const hasTask = (e: JobEvent) => !!e.task_name && (e.event_type === 'CHECKPOINT_SAVED' || e.event_type === 'RESUMED_FROM_CHECKPOINT');

interface Props {
  runId?: string;
  dark?: boolean;
  // When provided, checkpoint/resume events expose a "jump to task" control that
  // asks the parent to locate that task in the run's output.
  onLocate?: (taskName: string) => void;
}

// RunLifecycle renders the engine-narration timeline for a single execution run.
// It polls the run's events until a terminal event appears, then stops. Each
// event is expandable to reveal its raw event_data — the evidence behind the
// label — and checkpoint/resume events can jump to their task in the log.
const RunLifecycle: React.FC<Props> = ({ runId, dark = false, onLocate }) => {
  const [events, setEvents] = useState<JobEvent[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [open, setOpen] = useState<number | null>(null);
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
  const sub = dark ? 'text-gray-400' : 'text-gray-500';
  const rail = dark ? 'bg-white/10' : 'bg-gray-200';
  const detailBg = dark ? 'bg-white/5 border-white/10' : 'bg-gray-50 border-gray-200';
  const detailKey = dark ? 'text-gray-500' : 'text-gray-400';
  const detailVal = dark ? 'text-gray-300' : 'text-gray-700';

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

  // Render event_data (plus seq/time) as a compact key/value evidence panel.
  const renderDetail = (e: JobEvent) => {
    const entries: [string, any][] = [['seq', e.seq], ['at', new Date(e.created_at).toLocaleString()]];
    const d = e.event_data;
    if (d && typeof d === 'object') {
      for (const [k, v] of Object.entries(d)) entries.push([k, v]);
    }
    return (
      <div className={`mt-1.5 rounded-md border ${detailBg} px-2.5 py-1.5 font-mono text-[11px] leading-relaxed`}>
        {entries.map(([k, v]) => (
          <div key={k} className="flex gap-2">
            <span className={`${detailKey} shrink-0`}>{k}</span>
            <span className={`${detailVal} break-all`}>{typeof v === 'object' ? JSON.stringify(v) : String(v)}</span>
          </div>
        ))}
      </div>
    );
  };

  return (
    <ol className="relative space-y-3">
      <span className={`absolute left-[11px] top-1 bottom-1 w-px ${rail}`} aria-hidden="true" />
      {events.map((e, i) => {
        const kind = e.event_type as Kind;
        const meta = META[kind];
        const ring = dark ? DARK_RING[kind] : meta.ring;
        const isOpen = open === e.seq;
        const accent = dark ? meta.accent.replace('600', '400') : meta.accent;
        const canLocate = !!onLocate && hasTask(e);
        return (
          <li key={e.seq ?? i} className="relative flex items-start gap-3">
            <span className={`relative z-10 flex h-6 w-6 items-center justify-center rounded-full ${ring}`}>
              {meta.icon}
            </span>
            <div className="min-w-0 flex-1 -mt-0.5">
              <button
                onClick={() => setOpen(isOpen ? null : e.seq)}
                className="w-full flex items-baseline justify-between gap-2 text-left group"
                title="Show details"
              >
                <span className={`text-sm font-medium ${accent} flex items-center gap-1`}>
                  {isOpen ? <ChevronDown size={12} className={muted} /> : <ChevronRight size={12} className={muted} />}
                  {meta.label}
                </span>
                <span className={`text-[11px] tabular-nums ${muted} shrink-0`}>{timeStr(e.created_at)}</span>
              </button>
              {e.stdout_snippet && <p className={`text-xs ${sub} mt-0.5 break-words pl-4`}>{e.stdout_snippet}</p>}
              {canLocate && (
                <button
                  onClick={(ev) => { ev.stopPropagation(); onLocate!(e.task_name!); }}
                  className={`mt-1 ml-4 inline-flex items-center gap-1 text-[11px] font-medium ${dark ? 'text-sky-400 hover:text-sky-300' : 'text-sky-600 hover:text-sky-700'}`}
                  title={`Jump to TASK [${e.task_name}] in the output`}
                >
                  <CornerDownRight size={11} /> Jump to TASK [{e.task_name}]
                </button>
              )}
              {isOpen && <div className="pl-4">{renderDetail(e)}</div>}
            </div>
          </li>
        );
      })}
    </ol>
  );
};

export default RunLifecycle;
