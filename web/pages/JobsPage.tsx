import React, { useState, useEffect, useRef } from 'react';
import { api } from '../services/api';
import { Job, JobStatus, Template } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import Modal from '../components/ui/Modal';
import { Play, FileText, Copy, Check, Terminal, Download, Maximize2 } from 'lucide-react';
import Anser from 'anser';

const JobsPage = () => {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState<string>("");
  const [isLogModalOpen, setIsLogModalOpen] = useState(false);
  const [selectedJobId, setSelectedJobId] = useState<number | null>(null);
  const [selectedJobName, setSelectedJobName] = useState<string>("");
  const [logs, setLogs] = useState<string>("");
  const [copied, setCopied] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const logContainerRef = useRef<HTMLDivElement>(null);

  const loadData = () => {
    Promise.all([api.getJobs(), api.getTemplates()])
      .then(([jobsData, templatesData]) => {
        setJobs(jobsData || []);
        // Check if templatesData structure matches or if it's paginated
        setTemplates(templatesData.items || templatesData || []);
      })
      .catch(err => console.error(err));
  };

  useEffect(() => {
    loadData();
    // Poll for updates every 5 seconds
    const interval = setInterval(loadData, 5000);
    return () => clearInterval(interval);
  }, []);

  const handleLaunch = async () => {
    if (!selectedTemplate) return;
    const template = templates.find(t => t.id.toString() === selectedTemplate);
    if (!template) return;

    try {
      await api.launchJob({
        unified_job_template_id: template.unified_job_template_id || template.id, // Fallback if not populated 
        name: template.name
      });
      loadData();
    } catch (error) {
      console.error("Launch failed", error);
      alert("Failed to launch job");
    }
  };

  const viewLogs = async (runId: string, jobName: string, jobId: number) => {
    setSelectedJobId(jobId);
    setSelectedJobName(jobName);
    setLogs("Loading logs...");
    setCopied(false);
    setIsLogModalOpen(true);
    try {
      // Full playbook output lives in the object store; fetch the reassembled
      // log. Fall back to event stdout snippets for older runs (or lifecycle-only
      // output) that predate object-store logging.
      let fullLog = "";
      try {
        fullLog = await api.getJobLogs(runId);
      } catch {
        fullLog = "";
      }
      if (!fullLog || !fullLog.trim()) {
        const events = await api.getJobEvents(runId);
        fullLog = events.map((e: any) => e.stdout_snippet).filter(Boolean).join("\n");
      }
      setLogs(fullLog || "No logs available.");
      // Auto-scroll to bottom after logs load
      setTimeout(() => {
        if (logContainerRef.current) {
          logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
        }
      }, 100);
    } catch (error) {
      setLogs("Failed to load logs.");
      console.error(error);
    }
  };

  const copyLogs = async () => {
    // Strip HTML/ANSI codes for plain text copy
    const plainText = logs.replace(/\x1b\[[0-9;]*m/g, '');
    await navigator.clipboard.writeText(plainText);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const downloadLogs = () => {
    const plainText = logs.replace(/\x1b\[[0-9;]*m/g, '');
    const blob = new Blob([plainText], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `job-${selectedJobId}-logs.txt`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  const formattedLogs = Anser.ansiToHtml(logs, { use_classes: false });

  const getStatusBadge = (status: string) => {
    // Map backend status strings to badges
    switch (status) {
      case 'successful': return <Badge variant="success">Successful</Badge>;
      case 'failed': return <Badge variant="error">Failed</Badge>;
      case 'running': return <Badge variant="info">Running</Badge>;
      case 'pending': return <Badge variant="neutral">Pending</Badge>;
      default: return <Badge variant="neutral">{status}</Badge>;
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <h1 className="text-2xl font-bold text-gray-900">Jobs</h1>
        <div className="flex gap-2 w-full sm:w-auto">
          <select
            className="border-gray-300 rounded-md shadow-sm focus:ring-brand-500 focus:border-brand-500 sm:text-sm px-3 py-2 border w-full sm:w-64"
            value={selectedTemplate}
            onChange={(e) => setSelectedTemplate(e.target.value)}
          >
            <option value="">Select a Template...</option>
            {templates.map((t: Template) => (
              <option key={t.id} value={t.id}>{t.name}</option>
            ))}
          </select>
          <Button onClick={handleLaunch} disabled={!selectedTemplate} icon={<Play size={16} />}>
            Launch
          </Button>
        </div>
      </div>

      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
          <table className="min-w-full divide-y divide-gray-200">
            <thead className="bg-gray-50">
              <tr>
                <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">ID</th>
                <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Name</th>
                <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
                <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Started</th>
                <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">User</th>
                <th scope="col" className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Actions</th>
              </tr>
            </thead>
            <tbody className="bg-white divide-y divide-gray-200">
              {jobs.map((job) => (
                <tr key={job.id} className="hover:bg-gray-50 transition-colors">
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">#{job.id}</td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">{job.name}</td>
                  <td className="px-6 py-4 whitespace-nowrap">{getStatusBadge(job.status)}</td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                    {job.started_at ? new Date(job.started_at).toLocaleString() : '-'}
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">admin</td>
                  <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                    <button
                      onClick={() => viewLogs(job.current_run_id, job.name, job.id)}
                      className="text-brand-600 hover:text-brand-900 flex items-center justify-end w-full gap-1"
                    >
                      <Terminal size={16} /> Logs
                    </button>
                  </td>
                </tr>
              ))}
              {jobs.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-6 py-4 text-center text-sm text-gray-500">No jobs found.</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </Card>

      {/* Custom Terminal Modal */}
      {isLogModalOpen && (
        <div className="fixed inset-0 z-50 overflow-y-auto">
          <div className="flex min-h-screen items-center justify-center p-4 text-center sm:p-0">
            {/* Backdrop */}
            <div
              className="fixed inset-0 transition-opacity bg-black/80 backdrop-blur-sm"
              onClick={() => { setIsLogModalOpen(false); setIsFullscreen(false); }}
              aria-hidden="true"
            ></div>

            {/* Window Container */}
            <div className={`inline-block w-full align-bottom bg-[#1e1e1e] rounded-lg text-left overflow-hidden shadow-2xl transform transition-all sm:my-8 sm:align-middle ${isFullscreen ? 'w-full h-screen m-0 rounded-none' : 'max-w-5xl'}`}>

              {/* Window Header / Title Bar */}
              <div className="flex items-center justify-between bg-[#2d2d2d] px-4 py-3 border-b border-[#1e1e1e]">
                <div className="flex items-center gap-4">
                  <div className="flex gap-2">
                    <button onClick={() => { setIsLogModalOpen(false); setIsFullscreen(false); }} className="w-3 h-3 rounded-full bg-[#ff5f56] hover:bg-[#ff5f56]/80 transition-colors" />
                    <button onClick={() => setIsFullscreen(!isFullscreen)} className="w-3 h-3 rounded-full bg-[#ffbd2e] hover:bg-[#ffbd2e]/80 transition-colors" />
                    <button onClick={() => { }} className="w-3 h-3 rounded-full bg-[#27c93f] hover:bg-[#27c93f]/80 transition-colors" />
                  </div>
                  <div className="flex items-center gap-2 text-gray-400 text-sm font-medium font-sans border-l border-gray-700 pl-4 ml-2">
                    <Terminal size={14} />
                    <span>{selectedJobName} — #{selectedJobId}</span>
                  </div>
                </div>

                <div className="flex items-center gap-1">
                  <button
                    onClick={copyLogs}
                    className="p-1.5 rounded hover:bg-[#3d3d3d] text-gray-400 hover:text-white transition-colors"
                    title="Copy logs"
                  >
                    {copied ? <Check size={16} className="text-green-400" /> : <Copy size={16} />}
                  </button>
                  <button
                    onClick={downloadLogs}
                    className="p-1.5 rounded hover:bg-[#3d3d3d] text-gray-400 hover:text-white transition-colors"
                    title="Download logs"
                  >
                    <Download size={16} />
                  </button>
                  <button
                    onClick={() => setIsFullscreen(!isFullscreen)}
                    className="p-1.5 rounded hover:bg-[#3d3d3d] text-gray-400 hover:text-white transition-colors"
                    title={isFullscreen ? "Exit fullscreen" : "Fullscreen"}
                  >
                    <Maximize2 size={16} />
                  </button>
                </div>
              </div>

              {/* Terminal Content */}
              <div
                ref={logContainerRef}
                className={`font-mono text-sm ${isFullscreen ? 'h-[calc(100vh-50px)]' : 'h-[600px]'} overflow-y-auto whitespace-pre-wrap text-[#d4d4d4] p-6 leading-relaxed selection:bg-brand-900 selection:text-white scroll-smooth`}
                style={{
                  fontFamily: 'Consolas, Monaco, "Andale Mono", "Ubuntu Mono", monospace',
                  backgroundColor: '#1e1e1e'
                }}
                dangerouslySetInnerHTML={{ __html: formattedLogs }}
              />

              {/* Status Bar */}
              <div className="bg-[#007acc] text-white px-3 py-1 text-xs flex justify-between items-center font-sans">
                <span>{logs ? logs.split('\n').length : 0} lines</span>
                <span>UTF-8</span>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default JobsPage;