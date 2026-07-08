import React, { useState, useEffect } from 'react';
import { api, unwrap } from '../services/api';
import { Project } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import { Input, Select } from '../components/ui/Input';
import { RefreshCw, Plus, Trash2, Loader } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';

const ProjectsPage = () => {
  const [projects, setProjects] = useState<Project[]>([]);
  const [orgs, setOrgs] = useState<{ id: number; name: string }[]>([]);
  const [loading, setLoading] = useState(true);
  const [newUrl, setNewUrl] = useState('');
  const [newName, setNewName] = useState('');
  const [newOrg, setNewOrg] = useState<number | ''>('');
  const [syncing, setSyncing] = useState<number | null>(null);

  const fetchProjects = async () => {
    try {
      const data = await api.getProjects();
      setProjects(unwrap(data));
    } catch (err) {
      console.error('Failed to load projects', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchProjects();
    // Load the orgs the user can see so the create form can target one they
    // administer (rather than a hardcoded org).
    api.getOrganizations().then(d => {
      const list = unwrap<{ id: number; name: string }>(d);
      setOrgs(list);
      if (list.length) setNewOrg(list[0].id);
    }).catch(() => setOrgs([]));
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
    if (!newOrg) { toast.error('Select an organization'); return; }
    try {
      await api.createProject({
        name: newName,
        scm_url: newUrl,
        scm_type: 'git',
        organization_id: newOrg
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
      <PageSpinner />
    );
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-gray-900">Projects</h1>

      <Card title="Add New Project" className="mb-6">
        <form onSubmit={handleAdd} className="flex flex-col md:flex-row gap-4 items-end">
          <Select
            wrapperClassName="flex-1 w-full"
            label="Organization"
            value={newOrg}
            onChange={e => setNewOrg(e.target.value ? Number(e.target.value) : '')}
          >
            <option value="">Select organization…</option>
            {orgs.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}
          </Select>
          <Input
            wrapperClassName="flex-1 w-full"
            label="Name"
            placeholder="e.g. Core Infrastructure"
            value={newName}
            onChange={e => setNewName(e.target.value)}
          />
          <Input
            wrapperClassName="flex-[2] w-full"
            label="SCM URL"
            placeholder="https://github.com/..."
            value={newUrl}
            onChange={e => setNewUrl(e.target.value)}
          />
          <Button type="submit" icon={<Plus size={16} />}>Add</Button>
        </form>
      </Card>

      <Card>
        <div className="overflow-x-auto">
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
        </div>
      </Card>
    </div>
  );
};

export default ProjectsPage;