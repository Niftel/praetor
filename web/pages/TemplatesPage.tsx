import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Template, Project, Inventory, Credential, PaginatedResponse } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Plus, Edit2, Play, Trash2, Loader } from 'lucide-react';

const TemplatesPage = () => {
  const [templates, setTemplates] = useState<Template[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [inventories, setInventories] = useState<Inventory[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [loading, setLoading] = useState(true);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingTemplate, setEditingTemplate] = useState<Template | null>(null);

  // Form State
  const [formData, setFormData] = useState<Partial<Template>>({});

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        const [templatesData, projectsData, inventoriesData, credentialsData] = await Promise.all([
          api.getTemplates(),
          api.getProjects(),
          api.getInventories(),
          api.getCredentials()
        ]);
        // Handle paginated responses
        setTemplates(templatesData.items || templatesData || []);
        setProjects(projectsData.items || projectsData || []);
        setInventories(inventoriesData.items || inventoriesData || []);
        setCredentials(credentialsData || []);
      } catch (err) {
        console.error('Failed to load data', err);
      } finally {
        setLoading(false);
      }
    };
    fetchData();
  }, []);

  const openCreateModal = () => {
    setEditingTemplate(null);
    setFormData({});
    setIsModalOpen(true);
  };

  const openEditModal = (template: Template) => {
    setEditingTemplate(template);
    setFormData(template);
    setIsModalOpen(true);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      if (editingTemplate) {
        // Update existing - for now just update local state
        setTemplates(templates.map(t => t.id === editingTemplate.id ? { ...t, ...formData } as Template : t));
      } else {
        // Create new
        const newTemplate = await api.createTemplate(formData);
        setTemplates([...templates, newTemplate]);
      }
      setIsModalOpen(false);
    } catch (err) {
      console.error('Failed to save template', err);
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await api.deleteTemplate(id);
      setTemplates(templates.filter(t => t.id !== id));
    } catch (err) {
      console.error('Failed to delete template', err);
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
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-900">Templates</h1>
        <Button onClick={openCreateModal} icon={<Plus size={16} />}>
          Add Template
        </Button>
      </div>

      <Card className="overflow-hidden">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Name</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Project</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Inventory</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Playbook</th>
              <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Actions</th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {templates.map((template) => (
              <tr key={template.id} className="hover:bg-gray-50 transition-colors">
                <td className="px-6 py-4">
                  <div className="text-sm font-medium text-gray-900">{template.name}</div>
                  <div className="text-sm text-gray-500">{template.description}</div>
                </td>
                <td className="px-6 py-4 text-sm text-gray-500">
                  {projects.find(p => p.id === template.project_id)?.name || '-'}
                </td>
                <td className="px-6 py-4 text-sm text-gray-500">
                  {inventories.find(i => i.id === template.inventory_id)?.name || '-'}
                </td>
                <td className="px-6 py-4 text-sm text-gray-500 font-mono">
                  {template.playbook}
                </td>
                <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                  <div className="flex justify-end gap-2">
                    <button onClick={() => { }} className="text-green-600 hover:text-green-900" title="Launch">
                      <Play size={18} />
                    </button>
                    <button onClick={() => openEditModal(template)} className="text-blue-600 hover:text-blue-900" title="Edit">
                      <Edit2 size={18} />
                    </button>
                    <button onClick={() => handleDelete(template.id)} className="text-red-600 hover:text-red-900" title="Delete">
                      <Trash2 size={18} />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {templates.length === 0 && (
              <tr>
                <td colSpan={5} className="px-6 py-4 text-center text-gray-500">No templates found.</td>
              </tr>
            )}
          </tbody>
        </table>
      </Card>

      <Modal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={editingTemplate ? "Edit Template" : "New Job Template"}
        size="lg"
      >
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700">Name</label>
            <input
              type="text"
              required
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2"
              value={formData.name || ''}
              onChange={e => setFormData({ ...formData, name: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">Description</label>
            <input
              type="text"
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2"
              value={formData.description || ''}
              onChange={e => setFormData({ ...formData, description: e.target.value })}
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700">Project</label>
              <select
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2"
                value={formData.project_id || ''}
                onChange={e => setFormData({ ...formData, project_id: Number(e.target.value) })}
              >
                <option value="">Select Project</option>
                {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">Inventory</label>
              <select
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2"
                value={formData.inventory_id || ''}
                onChange={e => setFormData({ ...formData, inventory_id: Number(e.target.value) })}
              >
                <option value="">Select Inventory</option>
                {inventories.map(i => <option key={i.id} value={i.id}>{i.name}</option>)}
              </select>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700">Playbook</label>
              <input
                type="text"
                placeholder="site.yml"
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2"
                value={formData.playbook || ''}
                onChange={e => setFormData({ ...formData, playbook: e.target.value })}
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700">Credential</label>
              <select
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2"
                value={formData.credential_id || ''}
                onChange={e => setFormData({ ...formData, credential_id: Number(e.target.value) })}
              >
                <option value="">Select Credential</option>
                {credentials.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select>
            </div>
          </div>
          <div className="mt-5 flex justify-end gap-3">
            <Button type="button" variant="secondary" onClick={() => setIsModalOpen(false)}>Cancel</Button>
            <Button type="submit">Save Template</Button>
          </div>
        </form>
      </Modal>
    </div>
  );
};

export default TemplatesPage;