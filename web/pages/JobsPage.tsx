import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../services/api';
import { Job, Template } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import { Play, Terminal, ChevronRight, X } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';

const JobsPage = () => {
  const navigate = useNavigate();
  const [jobs, setJobs] = useState<Job[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState<string>('');

  const loadData = () => {
    Promise.all([api.getJobs(), api.getTemplates()])
      .then(([jobsData, templatesData]) => {
        setJobs(jobsData || []);
        setTemplates(templatesData.items || templatesData || []);
      })
      .catch(err => console.error(err));
  };

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 5000);
    return () => clearInterval(interval);
  }, []);

  const handleLaunch = async () => {
    if (!selectedTemplate) return;
    const template = templates.find(t => t.id.toString() === selectedTemplate);
    if (!template) return;
    try {
      await api.launchJob({
        unified_job_template_id: template.unified_job_template_id || template.id,
        name: template.name,
      });
      loadData();
    } catch (error) {
      console.error('Launch failed', error);
      toast.error('Failed to launch job');
    }
  };

  const openJob = (job: Job) => navigate(`/jobs/${job.id}`, { state: { job } });

  const isActive = (s: string) => ['pending', 'queued', 'running', 'waiting'].includes(s);
  const handleCancel = async (e: React.MouseEvent, job: Job) => {
    e.stopPropagation();
    if (!(await confirmDialog(`Cancel job "${job.name}"?`))) return;
    try {
      await api.cancelJob(job.id);
      loadData();
    } catch {
      toast.error('Failed to cancel job');
    }
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'successful': return <Badge variant="success">Successful</Badge>;
      case 'failed': return <Badge variant="error">Failed</Badge>;
      case 'running': return <Badge variant="info">Running</Badge>;
      case 'pending': return <Badge variant="warning">Pending</Badge>;
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
                <tr key={job.id} className="hover:bg-gray-50 transition-colors cursor-pointer" onClick={() => openJob(job)}>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">#{job.id}</td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">{job.name}</td>
                  <td className="px-6 py-4 whitespace-nowrap">{getStatusBadge(job.status)}</td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">
                    {job.started_at ? new Date(job.started_at).toLocaleString() : '-'}
                  </td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">admin</td>
                  <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                    <div className="inline-flex items-center justify-end gap-3">
                      {isActive(job.status) && (
                        <button
                          onClick={(e) => handleCancel(e, job)}
                          className="text-red-600 hover:text-red-800 inline-flex items-center gap-1"
                          title="Cancel this job"
                        >
                          <X size={15} /> Cancel
                        </button>
                      )}
                      <span className="text-brand-600 hover:text-brand-900 inline-flex items-center gap-1">
                        <Terminal size={16} /> View <ChevronRight size={14} />
                      </span>
                    </div>
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
    </div>
  );
};

export default JobsPage;
