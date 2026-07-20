import React, { useEffect, useRef, useState } from 'react';
import { api, DiagnosticEvent, streamJobDiagnostics } from '../services/api';
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
  | 'JOB_FAILED'
  | 'JOB_CANCELED';

const TERMINAL: Kind[] = ['JOB_COMPLETED', 'JOB_FAILED', 'JOB_CANCELED'];

const META: Record<Kind, { label: string; icon: React.ReactNode; accent: string; ring: string }> = {
  RUNNER_ONLINE: { label: 'Runner online · agentless', icon: <Zap size={14} />, accent: 'text-emerald-600', ring: 'bg-emerald-100 text-emerald-700' },
  JOB_STARTED: { label: 'Job started', icon: <Play size={14} />, accent: 'text-blue-600', ring: 'bg-blue-100 text-blue-700' },
  CHECKPOINT_SAVED: { label: 'Checkpoint saved', icon: <Flag size={14} />, accent: 'text-sky-600', ring: 'bg-sky-100 text-sky-700' },
  RESUMED_FROM_CHECKPOINT: { label: 'Resumed from checkpoint', icon: <RotateCcw size={14} />, accent: 'text-amber-600', ring: 'bg-amber-100 text-amber-700' },
  JOB_COMPLETED: { label: 'Completed', icon: <CheckCircle2 size={14} />, accent: 'text-emerald-600', ring: 'bg-emerald-100 text-emerald-700' },
  JOB_FAILED: { label: 'Failed', icon: <XCircle size={14} />, accent: 'text-red-600', ring: 'bg-red-100 text-red-700' },
  JOB_CANCELED: { label: 'Canceled', icon: <XCircle size={14} />, accent: 'text-gray-600', ring: 'bg-gray-100 text-gray-700' },
};

// Dark-mode ring colours, keyed the same way.
const DARK_RING: Record<Kind, string> = {
  RUNNER_ONLINE: 'bg-emerald-500/15 text-emerald-400',
  JOB_STARTED: 'bg-blue-500/15 text-blue-400',
  CHECKPOINT_SAVED: 'bg-sky-500/15 text-sky-400',
  RESUMED_FROM_CHECKPOINT: 'bg-amber-500/15 text-amber-400',
  JOB_COMPLETED: 'bg-emerald-500/15 text-emerald-400',
  JOB_FAILED: 'bg-red-500/15 text-red-400',
  JOB_CANCELED: 'bg-gray-500/15 text-gray-400',
};

const isKind = (t: string): t is Kind => t in META;
const timeStr = (iso: string) => { try { return new Date(iso).toLocaleTimeString(); } catch { return ''; } };

// Events that map to a concrete task in the log (their task_name is a real
// ansible task), so clicking them can jump to that TASK in the output.
const hasTask = (e: DiagnosticEvent) => !!e.task_name && (e.event_type === 'CHECKPOINT_SAVED' || e.event_type === 'RESUMED_FROM_CHECKPOINT');

interface Props {
  runId?: string;
  dark?: boolean;
  // When provided, checkpoint/resume events expose a "jump to task" control that
  // asks the parent to locate that task in the run's output.
  onLocate?: (taskName: string) => void;
}

// RunLifecycle renders the engine-narration timeline for a single execution run.
// It loads a bounded diagnostic history, then follows the resumable redacted
// event stream. Polling is retained only as a fallback for unavailable SSE.
const RunLifecycle: React.FC<Props> = ({ runId, dark = false, onLocate }) => {
  const [events, setEvents] = useState<DiagnosticEvent[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [open, setOpen] = useState<number | null>(null);
  const doneRef = useRef(false);
  const cursorRef = useRef(0);

  useEffect(() => {
    if (!runId) return;
    doneRef.current = false;
    cursorRef.current = 0;
    setEvents([]);
    setLoaded(false);
    let active = true;
    let streamController: AbortController | null = null;
    let fallbackTimer: ReturnType<typeof setTimeout> | null = null;
    let wakeVisibility: (() => void) | null = null;
    let wakeDelay: (() => void) | null = null;

    const accept = (event: DiagnosticEvent) => {
      if (!active || event.seq <= cursorRef.current) return;
      cursorRef.current = event.seq;
      if (isKind(event.event_type)) {
        setEvents(current => [...current, event].slice(-500));
        if (TERMINAL.includes(event.event_type)) doneRef.current = true;
      }
    };

    const poll = async (drain = false) => {
      try {
        let pages = 0;
        do {
          const page = await api.getJobDiagnostics(runId, cursorRef.current, 200);
          if (!active) return;
          for (const event of page.events || []) accept(event);
          if (page.summary && ['successful', 'failed', 'canceled', 'error', 'lost'].includes(page.summary.state)
              && cursorRef.current >= page.summary.last_event_seq) doneRef.current = true;
          pages += 1;
          if (!drain || page.next_cursor == null || pages >= 20) break;
        } while (active);
      } catch {
        // A failed fallback poll is retried by the reconnect loop.
      } finally {
        if (active) setLoaded(true);
      }
    };

    const waitUntilVisible = () => new Promise<void>(resolve => {
      if (document.visibilityState === 'visible') return resolve();
      wakeVisibility = () => {
        wakeVisibility = null;
        resolve();
      };
    });

    const follow = async () => {
      await poll(true);
      let failures = 0;
      while (active && !doneRef.current) {
        await waitUntilVisible();
        if (!active || doneRef.current) break;
        streamController = new AbortController();
        try {
          await streamJobDiagnostics(runId, cursorRef.current, {
            onEvent: accept,
            onTerminal: (_state, cursor) => {
              cursorRef.current = Math.max(cursorRef.current, cursor);
              doneRef.current = true;
            },
          }, streamController.signal);
          failures = 0;
        } catch (error) {
          if (!active || streamController.signal.aborted) continue;
          failures += 1;
        }
        if (!active || doneRef.current) break;
        await poll(false);
        const delay = Math.min(1000 * (2 ** failures), 10000);
        await new Promise<void>(resolve => {
          wakeDelay = () => {
            wakeDelay = null;
            fallbackTimer = null;
            resolve();
          };
          fallbackTimer = setTimeout(() => wakeDelay?.(), delay);
        });
      }
    };

    const visibilityChanged = () => {
      if (document.visibilityState === 'hidden') streamController?.abort();
      else wakeVisibility?.();
    };
    document.addEventListener('visibilitychange', visibilityChanged);
    void follow();
    return () => {
      active = false;
      doneRef.current = true;
      streamController?.abort();
      if (fallbackTimer) clearTimeout(fallbackTimer);
      wakeDelay?.();
      wakeVisibility?.();
      document.removeEventListener('visibilitychange', visibilityChanged);
    };
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
  const renderDetail = (e: DiagnosticEvent) => {
    const entries: [string, unknown][] = [['seq', e.seq], ['at', new Date(e.created_at).toLocaleString()]];
    for (const key of ['host_id', 'task_name', 'play_name', 'outcome', 'changed', 'duration_ms', 'failure_code'] as const) {
      if (e[key] !== undefined) entries.push([key, e[key]]);
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
              {e.task_name && <p className={`text-xs ${sub} mt-0.5 break-words pl-4`}>{e.task_name}</p>}
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
