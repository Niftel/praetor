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
  const [logs, setLogs] = useState<string>('Loading logs…');
  const [copied, setCopied] = useState(false);
  const [highlightTask, setHighlightTask] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

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

  const loadLogs = useCallback(async (rid: string) => {
    if (!rid) return;
    try {
      let full = '';
      try { full = await api.getJobLogs(rid); } catch { full = ''; }
      if (!full || !full.trim()) {
        const events = await api.getJobEvents(rid);
        full = (events || []).map((e: any) => e.stdout_snippet).filter(Boolean).join('\n');
      }
      setLogs(full || 'No output yet.');
    } catch {
      setLogs('Failed to load logs.');
    }
  }, []);

  // Initial load + polling while the job is active.
  useEffect(() => {
    if (!job) loadJob();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  useEffect(() => {
    if (runId) loadLogs(runId);
  }, [runId, loadLogs]);

  useEffect(() => {
    if (isTerminal) return;
    const h = setInterval(() => {
      loadJob();
      if (runId) loadLogs(runId);
    }, 3000);
    return () => clearInterval(h);
  }, [isTerminal, runId, loadJob, loadLogs]);

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
            {renderedLines.map((ln, i) => (
              <div
                key={i}
                data-task={ln.task || undefined}
                className={ln.task && highlightTask === ln.task ? 'bg-amber-400/25 -mx-5 px-5 rounded-sm transition-colors duration-300' : undefined}
                dangerouslySetInnerHTML={{ __html: ln.html }}
              />
            ))}
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
