import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Template, Project, Inventory, Credential, PaginatedResponse, SurveyQuestion } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Plus, Edit2, Play, Trash2, Loader } from 'lucide-react';

const TemplatesPage = () => {
  const [templates, setTemplates] = useState<Template[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [inventories, setInventories] = useState<Inventory[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [executionPacks, setExecutionPacks] = useState<any[]>([]);
  const [orgs, setOrgs] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingTemplate, setEditingTemplate] = useState<Template | null>(null);

  // Form State
  const [formData, setFormData] = useState<Partial<Template>>({});
  const [varsText, setVarsText] = useState('');
  const [survey, setSurvey] = useState<SurveyQuestion[]>([]);
  // Notifications (edit mode): org targets + this template's attachments.
  const [notifTargets, setNotifTargets] = useState<any[]>([]);
  const [notifAttached, setNotifAttached] = useState<any[]>([]);
  const [newNotif, setNewNotif] = useState({ name: '', notification_type: 'webhook', url: '' });

  // Launch dialog
  const [launchTpl, setLaunchTpl] = useState<Template | null>(null);
  const [launchVars, setLaunchVars] = useState('');
  const [launchLimit, setLaunchLimit] = useState('');
  const [launchAnswers, setLaunchAnswers] = useState<Record<string, string>>({});
  const [launchMsg, setLaunchMsg] = useState('');

  const blankQuestion = (): SurveyQuestion => ({ variable: '', question_name: '', type: 'text', required: false, default: '' });
  const updateQ = (i: number, patch: Partial<SurveyQuestion>) =>
    setSurvey(prev => prev.map((q, j) => (j === i ? { ...q, ...patch } : q)));

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        const [templatesData, projectsData, inventoriesData, credentialsData, packsData, orgsData] = await Promise.all([
          api.getTemplates(),
          api.getProjects(),
          api.getInventories(),
          api.getCredentials(),
          api.getExecutionPacks(),
          api.getOrganizations().catch(() => [])
        ]);
        // Handle paginated responses
        setTemplates(templatesData.items || templatesData || []);
        setProjects(projectsData.items || projectsData || []);
        setInventories(inventoriesData.items || inventoriesData || []);
        setCredentials(credentialsData || []);
        setExecutionPacks(packsData || []);
        setOrgs(orgsData?.items || orgsData || []);
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
    setVarsText('');
    setSurvey([]);
    setIsModalOpen(true);
  };

  const openEditModal = (template: Template) => {
    setEditingTemplate(template);
    setFormData(template);
    setVarsText(
      template.extra_vars && Object.keys(template.extra_vars).length
        ? JSON.stringify(template.extra_vars, null, 2)
        : ''
    );
    setSurvey(template.survey_spec?.spec || []);
    setNewNotif({ name: '', notification_type: 'webhook', url: '' });
    if (template.organization_id) {
      api.getNotificationTemplates(template.organization_id).then(d => setNotifTargets(d || [])).catch(() => setNotifTargets([]));
      api.getTemplateNotifications(template.id).then(d => setNotifAttached(d || [])).catch(() => setNotifAttached([]));
    }
    setIsModalOpen(true);
  };

  const isAttached = (ntId: number, event: string) =>
    notifAttached.some(a => a.notification_template_id === ntId && a.event === event);

  const toggleNotif = async (ntId: number, event: string) => {
    if (!editingTemplate) return;
    if (isAttached(ntId, event)) await api.detachTemplateNotification(editingTemplate.id, ntId, event);
    else await api.attachTemplateNotification(editingTemplate.id, { notification_template_id: ntId, event });
    api.getTemplateNotifications(editingTemplate.id).then(d => setNotifAttached(d || [])).catch(() => {});
  };

  const addNotifTarget = async () => {
    if (!editingTemplate || !newNotif.name.trim() || !newNotif.url.trim()) return;
    await api.createNotificationTemplate({ organization_id: editingTemplate.organization_id, ...newNotif });
    setNewNotif({ name: '', notification_type: 'webhook', url: '' });
    api.getNotificationTemplates(editingTemplate.organization_id).then(d => setNotifTargets(d || [])).catch(() => {});
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    let extra_vars: any = {};
    if (varsText.trim()) {
      try { extra_vars = JSON.parse(varsText); }
      catch { alert('Variables must be valid JSON'); return; }
    }
    if (!editingTemplate && !formData.organization_id) { alert('Select an organization'); return; }
    const payload = {
      ...formData,
      extra_vars,
      survey_spec: { spec: survey.filter(q => q.variable.trim()) },
    };
    try {
      if (editingTemplate) {
        const updated = await api.updateTemplate(editingTemplate.id, payload);
        setTemplates(templates.map(t => t.id === editingTemplate.id ? updated : t));
      } else {
        const newTemplate = await api.createTemplate(payload);
        setTemplates([...templates, newTemplate]);
      }
      setIsModalOpen(false);
    } catch (err) {
      console.error('Failed to save template', err);
    }
  };

  const openLaunch = (t: Template) => {
    setLaunchTpl(t);
    setLaunchVars('');
    setLaunchLimit(t.limit || '');
    // Seed survey answers from each question's default.
    const seed: Record<string, string> = {};
    (t.survey_spec?.spec || []).forEach(q => { seed[q.variable] = q.default || ''; });
    setLaunchAnswers(seed);
    setLaunchMsg('');
  };

  const handleLaunch = async () => {
    if (!launchTpl) return;
    const payload: any = {
      unified_job_template_id: launchTpl.unified_job_template_id,
      name: launchTpl.name,
    };
    if (launchTpl.survey_enabled) {
      // Survey answers are submitted as extra_vars; the API validates them
      // against the spec (required/defaults).
      const answers: Record<string, any> = {};
      for (const q of launchTpl.survey_spec?.spec || []) {
        const raw = launchAnswers[q.variable];
        if (raw === undefined || raw === '') continue;
        answers[q.variable] = q.type === 'integer' ? Number(raw) : raw;
      }
      payload.extra_vars = answers;
    } else if (launchTpl.ask_variables_on_launch && launchVars.trim()) {
      try { payload.extra_vars = JSON.parse(launchVars); }
      catch { setLaunchMsg('Variables must be valid JSON'); return; }
    }
    if (launchTpl.ask_limit_on_launch && launchLimit.trim()) {
      payload.limit = launchLimit.trim();
    }
    try {
      await api.launchJob(payload);
      setLaunchTpl(null);
    } catch (err) {
      setLaunchMsg('Launch failed');
      console.error(err);
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
                    <button onClick={() => openLaunch(template)} className="text-green-600 hover:text-green-900" title="Launch">
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
            <label className="block text-sm font-medium text-gray-700">Organization</label>
            <select
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2 disabled:bg-gray-100"
              value={formData.organization_id || ''}
              disabled={!!editingTemplate}
              onChange={e => setFormData({ ...formData, organization_id: Number(e.target.value) })}
            >
              <option value="">Select organization…</option>
              {orgs.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}
            </select>
          </div>
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
          <div>
            <label className="block text-sm font-medium text-gray-700">Execution Pack</label>
            <p className="text-xs text-gray-500 mb-1">The self-contained Python+Ansible runtime this job runs in (pushed to the host). Default = the standard pack.</p>
            <select
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2"
              value={formData.execution_pack_id || ''}
              onChange={e => setFormData({ ...formData, execution_pack_id: e.target.value ? Number(e.target.value) : undefined })}
            >
              <option value="">Default</option>
              {executionPacks.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">Inline Playbook (YAML)</label>
            <p className="text-xs text-gray-500 mb-1">Paste a playbook to run directly — use this when you have no project / source control. If set, it overrides the Project and Playbook path above.</p>
            <textarea
              rows={8}
              placeholder={'- hosts: all\n  gather_facts: false\n  tasks:\n    - name: Ping\n      ansible.builtin.ping:'}
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2 font-mono text-sm"
              value={formData.playbook_content || ''}
              onChange={e => setFormData({ ...formData, playbook_content: e.target.value || undefined })}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">Default Variables (JSON)</label>
            <textarea
              rows={4}
              placeholder={'{\n  "key": "value"\n}'}
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2 font-mono text-sm"
              value={varsText}
              onChange={e => setVarsText(e.target.value)}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">Limit (default host pattern)</label>
            <input
              type="text"
              placeholder="e.g. web* or host1:host2"
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500 border p-2"
              value={formData.limit || ''}
              onChange={e => setFormData({ ...formData, limit: e.target.value })}
            />
          </div>
          <div className="border-t pt-3">
            <p className="text-sm font-medium text-gray-700 mb-2">Prompt on launch</p>
            <label className="flex items-center gap-2 text-sm text-gray-700">
              <input
                type="checkbox"
                checked={!!formData.ask_variables_on_launch}
                onChange={e => setFormData({ ...formData, ask_variables_on_launch: e.target.checked })}
              />
              Ask for variables when launching
            </label>
            <label className="flex items-center gap-2 text-sm text-gray-700 mt-1">
              <input
                type="checkbox"
                checked={!!formData.ask_limit_on_launch}
                onChange={e => setFormData({ ...formData, ask_limit_on_launch: e.target.checked })}
              />
              Ask for limit when launching
            </label>
            <label className="flex items-center gap-2 text-sm text-gray-700 mt-3 pt-2 border-t">
              <input
                type="checkbox"
                checked={!!formData.use_fact_cache}
                onChange={e => setFormData({ ...formData, use_fact_cache: e.target.checked })}
              />
              Use fact cache (persist &amp; reuse gathered facts across runs)
            </label>
          </div>
          <div className="border-t pt-3">
            <label className="flex items-center gap-2 text-sm font-medium text-gray-700 mb-2">
              <input
                type="checkbox"
                checked={!!formData.survey_enabled}
                onChange={e => setFormData({ ...formData, survey_enabled: e.target.checked })}
              />
              Enable survey
            </label>
            {formData.survey_enabled && (
              <div className="space-y-3">
                {survey.map((q, i) => (
                  <div key={i} className="border rounded p-2 space-y-2 bg-gray-50">
                    <div className="grid grid-cols-2 gap-2">
                      <input placeholder="variable (e.g. app_version)" className="border p-1 rounded text-sm font-mono"
                        value={q.variable} onChange={e => updateQ(i, { variable: e.target.value })} />
                      <input placeholder="Question label" className="border p-1 rounded text-sm"
                        value={q.question_name} onChange={e => updateQ(i, { question_name: e.target.value })} />
                    </div>
                    <div className="grid grid-cols-3 gap-2 items-center">
                      <select className="border p-1 rounded text-sm" value={q.type}
                        onChange={e => updateQ(i, { type: e.target.value as SurveyQuestion['type'] })}>
                        <option value="text">Text</option>
                        <option value="textarea">Textarea</option>
                        <option value="password">Password</option>
                        <option value="integer">Integer</option>
                        <option value="multiplechoice">Multiple choice</option>
                      </select>
                      <input placeholder="default" className="border p-1 rounded text-sm"
                        value={q.default || ''} onChange={e => updateQ(i, { default: e.target.value })} />
                      <label className="flex items-center gap-1 text-xs text-gray-600">
                        <input type="checkbox" checked={q.required} onChange={e => updateQ(i, { required: e.target.checked })} />
                        Required
                      </label>
                    </div>
                    {q.type === 'multiplechoice' && (
                      <textarea rows={2} placeholder="one choice per line" className="w-full border p-1 rounded text-sm"
                        value={q.choices || ''} onChange={e => updateQ(i, { choices: e.target.value })} />
                    )}
                    <button type="button" className="text-xs text-red-600 hover:underline"
                      onClick={() => setSurvey(survey.filter((_, j) => j !== i))}>Remove question</button>
                  </div>
                ))}
                <Button type="button" variant="secondary" onClick={() => setSurvey([...survey, blankQuestion()])}>+ Add question</Button>
              </div>
            )}
          </div>
          {editingTemplate && (
            <div className="border-t pt-3">
              <p className="text-sm font-medium text-gray-700 mb-2">Notifications</p>
              {notifTargets.length === 0 && (
                <p className="text-xs text-gray-500 mb-2">No notification targets in this organization yet — add one below.</p>
              )}
              {notifTargets.map(nt => (
                <div key={nt.id} className="flex items-center gap-3 text-sm py-1">
                  <span className="flex-1 truncate">{nt.name} <span className="text-xs text-gray-400">({nt.notification_type})</span></span>
                  {['success', 'error', 'started'].map(ev => (
                    <label key={ev} className="flex items-center gap-1 text-xs text-gray-600">
                      <input type="checkbox" checked={isAttached(nt.id, ev)} onChange={() => toggleNotif(nt.id, ev)} />
                      {ev}
                    </label>
                  ))}
                </div>
              ))}
              <div className="flex gap-2 mt-2">
                <input placeholder="name" className="border p-1 rounded text-sm w-1/4"
                  value={newNotif.name} onChange={e => setNewNotif({ ...newNotif, name: e.target.value })} />
                <select className="border p-1 rounded text-sm"
                  value={newNotif.notification_type} onChange={e => setNewNotif({ ...newNotif, notification_type: e.target.value })}>
                  <option value="webhook">Webhook</option>
                  <option value="slack">Slack</option>
                </select>
                <input placeholder="URL" className="border p-1 rounded text-sm flex-1"
                  value={newNotif.url} onChange={e => setNewNotif({ ...newNotif, url: e.target.value })} />
                <Button type="button" variant="secondary" onClick={addNotifTarget}>Add</Button>
              </div>
            </div>
          )}
          <div className="border-t pt-3">
            <label className="flex items-center gap-2 text-sm font-medium text-gray-700 mb-2">
              <input
                type="checkbox"
                checked={!!formData.webhook_enabled}
                onChange={e => setFormData({ ...formData, webhook_enabled: e.target.checked })}
              />
              Enable webhook trigger
            </label>
            {formData.webhook_enabled && (
              <div className="space-y-2">
                <select className="border p-1 rounded text-sm"
                  value={formData.webhook_service || 'generic'}
                  onChange={e => setFormData({ ...formData, webhook_service: e.target.value })}>
                  <option value="github">GitHub</option>
                  <option value="gitlab">GitLab</option>
                  <option value="generic">Generic</option>
                </select>
                {editingTemplate && formData.webhook_key ? (
                  <div className="text-xs text-gray-600 space-y-1">
                    <div>URL: <code className="bg-gray-100 px-1 break-all">{window.location.origin}/api/v1/webhooks/job-templates/{editingTemplate.id}/{formData.webhook_service || 'generic'}</code></div>
                    <div>Secret: <code className="bg-gray-100 px-1 break-all">{formData.webhook_key}</code></div>
                    <p className="text-gray-400">Configure this URL + secret in your Git provider (GitHub HMAC, GitLab token, or generic X-Praetor-Token).</p>
                  </div>
                ) : (
                  <p className="text-xs text-gray-400">Save the template to generate the webhook URL and secret.</p>
                )}
              </div>
            )}
          </div>
          <div className="mt-5 flex justify-end gap-3">
            <Button type="button" variant="secondary" onClick={() => setIsModalOpen(false)}>Cancel</Button>
            <Button type="submit">Save Template</Button>
          </div>
        </form>
      </Modal>

      <Modal
        isOpen={!!launchTpl}
        onClose={() => setLaunchTpl(null)}
        title={launchTpl ? `Launch: ${launchTpl.name}` : 'Launch'}
        size="md"
      >
        {launchTpl && (
          <div className="space-y-4">
            {!launchTpl.survey_enabled && !launchTpl.ask_variables_on_launch && !launchTpl.ask_limit_on_launch && (
              <p className="text-sm text-gray-500">This template runs with its saved configuration.</p>
            )}
            {launchTpl.survey_enabled && (launchTpl.survey_spec?.spec || []).map((q, i) => (
              <div key={i}>
                <label className="block text-sm font-medium text-gray-700">
                  {q.question_name || q.variable}{q.required && <span className="text-red-500"> *</span>}
                </label>
                {q.type === 'textarea' ? (
                  <textarea rows={3} className="mt-1 block w-full rounded-md border-gray-300 border p-2 text-sm"
                    value={launchAnswers[q.variable] || ''}
                    onChange={e => setLaunchAnswers({ ...launchAnswers, [q.variable]: e.target.value })} />
                ) : q.type === 'multiplechoice' ? (
                  <select className="mt-1 block w-full rounded-md border-gray-300 border p-2 text-sm"
                    value={launchAnswers[q.variable] || ''}
                    onChange={e => setLaunchAnswers({ ...launchAnswers, [q.variable]: e.target.value })}>
                    <option value="">Select…</option>
                    {(q.choices || '').split('\n').map(c => c.trim()).filter(Boolean).map(c => <option key={c} value={c}>{c}</option>)}
                  </select>
                ) : (
                  <input
                    type={q.type === 'password' ? 'password' : q.type === 'integer' ? 'number' : 'text'}
                    className="mt-1 block w-full rounded-md border-gray-300 border p-2 text-sm"
                    value={launchAnswers[q.variable] || ''}
                    onChange={e => setLaunchAnswers({ ...launchAnswers, [q.variable]: e.target.value })} />
                )}
              </div>
            ))}
            {!launchTpl.survey_enabled && launchTpl.ask_variables_on_launch && (
              <div>
                <label className="block text-sm font-medium text-gray-700">Variables (JSON)</label>
                <textarea
                  rows={4}
                  placeholder={'{\n  "key": "value"\n}'}
                  className="mt-1 block w-full rounded-md border-gray-300 border p-2 font-mono text-sm"
                  value={launchVars}
                  onChange={e => setLaunchVars(e.target.value)}
                />
              </div>
            )}
            {launchTpl.ask_limit_on_launch && (
              <div>
                <label className="block text-sm font-medium text-gray-700">Limit</label>
                <input
                  type="text"
                  placeholder="host pattern"
                  className="mt-1 block w-full rounded-md border-gray-300 border p-2"
                  value={launchLimit}
                  onChange={e => setLaunchLimit(e.target.value)}
                />
              </div>
            )}
            {launchMsg && <p className="text-sm text-red-600">{launchMsg}</p>}
            <div className="mt-5 flex justify-end gap-3">
              <Button type="button" variant="secondary" onClick={() => setLaunchTpl(null)}>Cancel</Button>
              <Button type="button" onClick={handleLaunch}>Launch</Button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default TemplatesPage;