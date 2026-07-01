import React, { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { api } from '../services/api';
import { Job } from '../types';
import Card from '../components/ui/Card';
import Badge from '../components/ui/Badge';
import RunLifecycle from '../components/RunLifecycle';
import { ArrowLeft, Copy, Check, Download, Terminal, Loader } from 'lucide-react';
import Anser from 'anser';

const TERMINAL_STATES = ['successful', 'failed', 'error', 'canceled'];

const statusVariant = (s?: string): 'success' | 'error' | 'info' | 'warning' | 'neutral' => {
  if (s === 'successful') return 'success';
  if (s === 'failed' || s === 'error') return 'error';
  if (s === 'running') return 'info';
  if (s === 'pending') return 'warning';
  return 'neutral';
};

const JobDetailPage = () => {
  const { jobId } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const id = Number(jobId);

  const [job, setJob] = useState<Job | null>((location.state as any)?.job || null);
  const [logs, setLogs] = useState<string>('');
  const [logsLoaded, setLogsLoaded] = useState(false);
  const [copied, setCopied] = useState(false);
  const [highlightTask, setHighlightTask] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);
  // Tail cursor: the highest chunk seq we've already appended. -1 = fetch all.
  const sinceRef = useRef<number>(-1);
  // Guards against overlapping loads: two effects can fire loadLogs in the same
  // commit (initial + terminal), and without this both would fetch since=-1
  // before the cursor advances and double-append the whole log.
  const loadingRef = useRef(false);

  // Pre-render each log line once per logs change (Anser is expensive). A line
  // that opens an ansible task carries its task name so a lifecycle event can
  // scroll to and flash it — tying the narration to the real output.
  const renderedLines = useMemo(() => logs.split('\n').map((line) => {
    const m = line.match(/^TASK \[(.+?)\]/);
    return { task: m ? m[1].toLowerCase() : null, html: line ? Anser.ansiToHtml(line, { use_classes: false }) : '&nbsp;' };
  }), [logs]);

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

  const runId = job?.current_run_id || '';
  const isTerminal = job ? TERMINAL_STATES.includes(job.status) : false;

  // Resolve the job (name/status/run id). Navigation passes it via state for an
  // instant render; on a hard reload we fall back to the jobs list.
  const loadJob = useCallback(async () => {
    try {
      const list = await api.getJobs();
      const jobs: Job[] = Array.isArray(list) ? list : (list?.items || []);
      const found = jobs.find(j => j.id === id);
      if (found) setJob(found);
    } catch { /* keep whatever we have */ }
  }, [id]);

  // Incremental tail: fetch only chunks newer than our cursor and append them,
  // so output streams in as the runner ships it instead of refetching the whole
  // log each poll. Advances sinceRef with the server's tail cursor.
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

  // Initial load + polling while the job is active.
  useEffect(() => {
    if (!job) loadJob();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  // Reset and re-tail whenever the run changes (or first resolves).
  useEffect(() => {
    sinceRef.current = -1;
    setLogs('');
    setLogsLoaded(false);
    if (runId) loadLogs(runId);
  }, [runId, loadLogs]);

  // Stream while running (fast incremental poll); one final fetch on completion
  // to catch the trailing chunks.
  useEffect(() => {
    if (isTerminal) {
      if (runId) loadLogs(runId);
      return;
    }
    const h = setInterval(() => {
      loadJob();
      if (runId) loadLogs(runId);
    }, 1200);
    return () => clearInterval(h);
  }, [isTerminal, runId, loadJob, loadLogs]);

  // Fallback for runs with no object-store output (lifecycle-only or legacy):
  // once finished with an empty log, show the event stdout snippets instead.
  useEffect(() => {
    if (!isTerminal || !logsLoaded || logs || !runId || sinceRef.current >= 0) return;
    api.getJobEvents(runId).then(evs => {
      const t = (evs || []).map((e: any) => e.stdout_snippet).filter(Boolean).join('\n');
      if (t) setLogs(t);
    }).catch(() => { });
  }, [isTerminal, logsLoaded, logs, runId]);

  // Keep the terminal pinned to the tail while the job streams — unless the user
  // just jumped to a task, in which case don't yank them away from it.
  useEffect(() => {
    if (!isTerminal && !highlightTask && logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [logs, isTerminal, highlightTask]);

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

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-3 min-w-0">
          <button onClick={() => navigate('/jobs')} className="text-gray-500 hover:text-gray-900 p-2 rounded-lg hover:bg-gray-100 shrink-0" title="Back to jobs">
            <ArrowLeft size={20} />
          </button>
          <div className="min-w-0">
            <h1 className="text-2xl font-bold text-gray-900 truncate">{job?.name || `Job #${id}`}</h1>
            <p className="text-sm text-gray-500 mt-0.5">
              Job #{id}
              {job?.started_at ? ` · started ${new Date(job.started_at).toLocaleString()}` : ''}
              {runId ? ` · run ${runId.slice(0, 8)}` : ''}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {!isTerminal && <Loader size={16} className="animate-spin text-brand-500" />}
          <Badge variant={statusVariant(job?.status)}>{job?.status || 'loading…'}</Badge>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-[320px_1fr] gap-5 items-start">
        {/* Narration */}
        <Card title="Execution lifecycle">
          {runId
            ? <RunLifecycle runId={runId} onLocate={locateTask} />
            : <p className="text-xs text-gray-400 py-2">Waiting for the runner to start…</p>}
        </Card>

        {/* Logs terminal */}
        <div className="rounded-lg overflow-hidden shadow-sm border border-[#1e1e1e]">
          <div className="flex items-center justify-between bg-[#2d2d2d] px-4 py-2.5 border-b border-[#1e1e1e]">
            <div className="flex items-center gap-2 text-gray-300 text-sm font-medium">
              <Terminal size={14} /> Output
              {!isTerminal && runId && (
                <span className="flex items-center gap-1 text-[11px] font-normal text-emerald-400 ml-1">
                  <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse" /> live
                </span>
              )}
            </div>
            <div className="flex items-center gap-1">
              <button onClick={copyLogs} className="p-1.5 rounded hover:bg-[#3d3d3d] text-gray-400 hover:text-white transition-colors" title="Copy logs">
                {copied ? <Check size={16} className="text-green-400" /> : <Copy size={16} />}
              </button>
              <button onClick={downloadLogs} className="p-1.5 rounded hover:bg-[#3d3d3d] text-gray-400 hover:text-white transition-colors" title="Download logs">
                <Download size={16} />
              </button>
            </div>
          </div>
          <div
            ref={logRef}
            className="font-mono text-sm h-[calc(100vh-260px)] min-h-[360px] overflow-y-auto whitespace-pre-wrap text-[#d4d4d4] p-5 leading-relaxed scroll-smooth"
            style={{ fontFamily: 'Consolas, Monaco, "Andale Mono", "Ubuntu Mono", monospace', backgroundColor: '#1e1e1e' }}
          >
            {logs
              ? renderedLines.map((ln, i) => (
                <div
                  key={i}
                  data-task={ln.task || undefined}
                  className={ln.task && highlightTask === ln.task ? 'bg-amber-400/25 -mx-5 px-5 rounded-sm transition-colors duration-300' : undefined}
                  dangerouslySetInnerHTML={{ __html: ln.html }}
                />
              ))
              : <span className="text-gray-500">{isTerminal ? 'No output.' : (logsLoaded ? 'Waiting for output…' : 'Loading…')}</span>}
          </div>
          <div className="bg-[#007acc] text-white px-3 py-1 text-xs flex justify-between font-sans">
            <span>{logs ? logs.split('\n').length : 0} lines</span>
            <span>UTF-8</span>
          </div>
        </div>
      </div>
    </div>
  );
};

export default JobDetailPage;
