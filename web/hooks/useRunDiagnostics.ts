import { useEffect, useRef, useState } from 'react';
import { api, DiagnosticEvent, RunDiagnostics, streamJobDiagnostics } from '../services/api';

type ConnectionState = 'connecting' | 'live' | 'polling' | 'closed';

export interface LiveRunDiagnostics {
  summary: RunDiagnostics['summary'] | null;
  events: DiagnosticEvent[];
  loading: boolean;
  error: string | null;
  connection: ConnectionState;
}

const TERMINAL = new Set(['successful', 'failed', 'canceled', 'error', 'lost']);
const MAX_EVENTS = 5000;

/**
 * One authoritative browser model for a run. The initial bounded page drain and
 * the live stream share an exclusive sequence cursor, so reconnects reconcile
 * without gaps or duplicates. Raw event_data and stdout never enter this hook.
 */
export function useRunDiagnostics(runId?: string): LiveRunDiagnostics {
  const [summary, setSummary] = useState<RunDiagnostics['summary'] | null>(null);
  const [events, setEvents] = useState<DiagnosticEvent[]>([]);
  const [loading, setLoading] = useState(Boolean(runId));
  const [error, setError] = useState<string | null>(null);
  const [connection, setConnection] = useState<ConnectionState>('connecting');
  const cursorRef = useRef(0);

  useEffect(() => {
    cursorRef.current = 0;
    setSummary(null);
    setEvents([]);
    setError(null);
    setLoading(Boolean(runId));
    setConnection(runId ? 'connecting' : 'closed');
    if (!runId) return;

    let active = true;
    let terminal = false;
    let controller: AbortController | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let wakeRetry: (() => void) | null = null;
    let wakeVisible: (() => void) | null = null;

    const accept = (event: DiagnosticEvent) => {
      if (!active || event.seq <= cursorRef.current) return;
      cursorRef.current = event.seq;
      setEvents(current => [...current, event].slice(-MAX_EVENTS));
    };

    const applySummary = (next: RunDiagnostics['summary']) => {
      if (!active) return;
      setSummary(next);
      terminal = TERMINAL.has(next.state) && cursorRef.current >= next.last_event_seq;
      if (terminal) setConnection('closed');
    };

    const poll = async (drain: boolean) => {
      setConnection('polling');
      let pages = 0;
      do {
        const page = await api.getJobDiagnostics(runId, cursorRef.current, 200);
        if (!active) return;
        for (const event of page.events || []) accept(event);
        applySummary(page.summary);
        pages += 1;
        if (!drain || page.next_cursor == null || pages >= 25) break;
      } while (active && !terminal);
    };

    const waitForVisible = () => new Promise<void>(resolve => {
      if (document.visibilityState === 'visible') return resolve();
      wakeVisible = () => { wakeVisible = null; resolve(); };
    });

    const waitForRetry = (delay: number) => new Promise<void>(resolve => {
      wakeRetry = () => {
        wakeRetry = null;
        retryTimer = null;
        resolve();
      };
      retryTimer = setTimeout(() => wakeRetry?.(), delay);
    });

    const follow = async () => {
      let failures = 0;
      try {
        await poll(true);
        setError(null);
      } catch (cause) {
        if (active) setError(cause instanceof Error ? cause.message : 'Diagnostics are unavailable');
      } finally {
        if (active) setLoading(false);
      }

      while (active && !terminal) {
        await waitForVisible();
        if (!active || terminal) break;
        controller = new AbortController();
        setConnection('connecting');
        try {
          setConnection('live');
          await streamJobDiagnostics(runId, cursorRef.current, {
            onEvent: accept,
            onTerminal: (state, cursor) => {
              cursorRef.current = Math.max(cursorRef.current, cursor);
              setSummary(current => current ? { ...current, state, last_event_seq: cursor } : current);
              terminal = true;
              setConnection('closed');
            },
          }, controller.signal);
          failures = 0;
          setError(null);
        } catch (cause) {
          if (!active || controller.signal.aborted) continue;
          failures += 1;
          setError(cause instanceof Error ? cause.message : 'Live updates interrupted');
        }
        if (!active || terminal) break;
        try {
          await poll(false);
        } catch { /* the reconnect loop remains authoritative */ }
        await waitForRetry(Math.min(1000 * (2 ** failures), 10000));
      }
    };

    const visibilityChanged = () => {
      if (document.visibilityState === 'hidden') controller?.abort();
      else wakeVisible?.();
    };
    document.addEventListener('visibilitychange', visibilityChanged);
    void follow();

    return () => {
      active = false;
      controller?.abort();
      if (retryTimer) clearTimeout(retryTimer);
      wakeRetry?.();
      wakeVisible?.();
      document.removeEventListener('visibilitychange', visibilityChanged);
    };
  }, [runId]);

  return { summary, events, loading, error, connection };
}
