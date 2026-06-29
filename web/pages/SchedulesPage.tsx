import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Schedule, Template } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import Modal from '../components/ui/Modal';
import { Calendar, Plus, Power, Loader, Trash2 } from 'lucide-react';

const SchedulesPage = () => {
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [formData, setFormData] = useState({ name: '', unified_job_template_id: 0, rrule: 'FREQ=DAILY;INTERVAL=1' });

  const fetchData = async () => {
    try {
      setLoading(true);
      const [schedulesData, templatesData] = await Promise.all([
        api.getSchedules(),
        api.getTemplates()
      ]);
      setSchedules(schedulesData || []);
      setTemplates(templatesData?.items || templatesData || []);
    } catch (err) {
      console.error('Failed to load schedules', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  const toggleSchedule = async (id: number) => {
    const schedule = schedules.find(s => s.id === id);
    if (!schedule) return;
    try {
      await api.updateSchedule(id, { ...schedule, enabled: !schedule.enabled });
      setSchedules(schedules.map(s => s.id === id ? { ...s, enabled: !s.enabled } : s));
    } catch (err) {
      console.error('Failed to toggle schedule', err);
    }
  };

  const handleCreate = async () => {
    if (!formData.name || !formData.unified_job_template_id) return;
    try {
      await api.createSchedule(formData);
      setShowModal(false);
      setFormData({ name: '', unified_job_template_id: 0, rrule: 'FREQ=DAILY;INTERVAL=1' });
      fetchData();
    } catch (err) {
      console.error('Failed to create schedule', err);
      alert('Failed to create schedule');
    }
  };

  const handleDelete = async (id: number) => {
    if (!confirm('Delete this schedule?')) return;
    try {
      await api.deleteSchedule(id);
      fetchData();
    } catch (err) {
      console.error('Failed to delete schedule', err);
    }
  };

  const getTemplateName = (templateId: number) => {
    const template = templates.find(t => t.id === templateId || t.unified_job_template_id === templateId);
    return template?.name || 'Unknown Template';
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
        <h1 className="text-2xl font-bold text-gray-900">Schedules</h1>
        <Button icon={<Plus size={16} />} onClick={() => setShowModal(true)}>Create Schedule</Button>
      </div>

      <Card>
        <div className="divide-y divide-gray-100">
          {schedules.map(schedule => (
            <div key={schedule.id} className="p-6 flex items-center justify-between hover:bg-gray-50 transition-colors">
              <div className="flex items-start gap-4">
                <div className="p-3 bg-purple-50 text-purple-600 rounded-lg">
                  <Calendar size={24} />
                </div>
                <div>
                  <h3 className="text-lg font-medium text-gray-900">{schedule.name}</h3>
                  <div className="flex items-center gap-2 mt-1">
                    <span className="text-sm text-gray-500">Template: <span className="font-medium text-gray-700">{getTemplateName(schedule.unified_job_template_id)}</span></span>
                    <span className="text-gray-300">•</span>
                    <span className="text-sm text-gray-500">Next Run: {schedule.next_run ? new Date(schedule.next_run).toLocaleString() : 'Not scheduled'}</span>
                  </div>
                  <code className="text-xs bg-gray-100 px-2 py-1 rounded mt-2 inline-block text-gray-600">{schedule.rrule}</code>
                </div>
              </div>

              <div className="flex items-center gap-4">
                <Badge variant={schedule.enabled ? 'success' : 'neutral'}>
                  {schedule.enabled ? 'Active' : 'Disabled'}
                </Badge>
                <button
                  onClick={() => toggleSchedule(schedule.id)}
                  className={`p-2 rounded-full transition-colors ${schedule.enabled ? 'text-brand-600 hover:bg-brand-50' : 'text-gray-400 hover:bg-gray-100'}`}
                  title="Toggle Schedule"
                >
                  <Power size={20} />
                </button>
                <button
                  onClick={() => handleDelete(schedule.id)}
                  className="p-2 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50"
                  title="Delete"
                >
                  <Trash2 size={20} />
                </button>
              </div>
            </div>
          ))}
          {schedules.length === 0 && (
            <p className="p-6 text-gray-500 text-center">No schedules found. Click "Create Schedule" to add one.</p>
          )}
        </div>
      </Card>

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title="Create Schedule">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Name</label>
            <input
              type="text"
              className="w-full border border-gray-300 rounded-md p-2"
              value={formData.name}
              onChange={e => setFormData({ ...formData, name: e.target.value })}
              placeholder="Daily Backup"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Template</label>
            <select
              className="w-full border border-gray-300 rounded-md p-2"
              value={formData.unified_job_template_id}
              onChange={e => setFormData({ ...formData, unified_job_template_id: Number(e.target.value) })}
            >
              <option value={0}>Select Template</option>
              {templates.map(t => (
                <option key={t.id} value={t.unified_job_template_id || t.id}>{t.name}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">RRule</label>
            <input
              type="text"
              className="w-full border border-gray-300 rounded-md p-2 font-mono text-sm"
              value={formData.rrule}
              onChange={e => setFormData({ ...formData, rrule: e.target.value })}
              placeholder="FREQ=DAILY;INTERVAL=1"
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button onClick={handleCreate}>Create</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default SchedulesPage;