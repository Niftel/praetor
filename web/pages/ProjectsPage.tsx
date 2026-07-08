import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Project } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import { RefreshCw, GitBranch, Plus, Trash2, Loader } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';

const ProjectsPage = () => {
  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);
  const [newUrl, setNewUrl] = useState('');
  const [newName, setNewName] = useState('');
  const [syncing, setSyncing] = useState<number | null>(null);

  const fetchProjects = async () => {
    try {
      const data = await api.getProjects();
      setProjects(data?.items || data || []);
    } catch (err) {
      console.error('Failed to load projects', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchProjects();
  }, []);

  const handleSync = async (id: number) => {
    setSyncing(id);
    try {
      await api.syncProject(id);
      fetchProjects();
    } catch (err) {
      console.error('Sync failed', err);
    } finally {
      setSyncing(null);
    }
  };

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newUrl || !newName) return;
    try {
      await api.createProject({
        name: newName,
        scm_url: newUrl,
        scm_type: 'git',
        organization_id: 1
      });
      setNewUrl('');
      setNewName('');
      fetchProjects();
    } catch (err: any) {
      console.error('Failed to create project', err);
      toast.error(err.message || 'Failed to create project');
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader className="animate-spin text-brand-600" size={32} />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-gray-900">Projects</h1>

      <Card title="Add New Project" className="mb-6">
        <form onSubmit={handleAdd} className="flex flex-col md:flex-row gap-4 items-end">
          <div className="flex-1 w-full">
            <label className="block text-sm font-medium text-gray-700 mb-1">Name</label>
            <input
              className="w-full rounded-md border border-gray-300 p-2 focus:ring-brand-500 focus:border-brand-500"
              placeholder="e.g. Core Infrastructure"
              value={newName}
              onChange={e => setNewName(e.target.value)}
            />
          </div>
          <div className="flex-[2] w-full">
            <label className="block text-sm font-medium text-gray-700 mb-1">SCM URL</label>
            <div className="relative rounded-md shadow-sm">
              <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                <GitBranch className="h-4 w-4 text-gray-400" />
              </div>
              <input
                className="w-full rounded-md border border-gray-300 pl-10 p-2 focus:ring-brand-500 focus:border-brand-500"
                placeholder="https://github.com/..."
                value={newUrl}
                onChange={e => setNewUrl(e.target.value)}
              />
            </div>
          </div>
          <Button type="submit" icon={<Plus size={16} />}>Add</Button>
        </form>
      </Card>

      <Card>
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Name</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">SCM URL</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Branch</th>
              <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Actions</th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {projects.map((project) => (
              <tr key={project.id}>
                <td className="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900">{project.name}</td>
                <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500 font-mono">{project.scm_url}</td>
                <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{project.scm_branch || 'main'}</td>
                <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                  <button
                    onClick={() => handleSync(project.id)}
                    disabled={syncing === project.id}
                    className="text-brand-600 hover:text-brand-900 disabled:opacity-50"
                  >
                    <RefreshCw size={18} className={syncing === project.id ? 'animate-spin' : ''} />
                  </button>
                </td>
              </tr>
            ))}
            {projects.length === 0 && (
              <tr>
                <td colSpan={4} className="px-6 py-8 text-center text-gray-500">No projects found. Add one above.</td>
              </tr>
            )}
          </tbody>
        </table>
      </Card>
    </div>
  );
};

export default ProjectsPage;