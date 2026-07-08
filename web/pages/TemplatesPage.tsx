import React, { useState, useEffect } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Template, Project, Inventory, Credential, PaginatedResponse, SurveyQuestion } from '../types';
import Card from '../components/ui/Card';
import { Input, Textarea, Select } from '../components/ui/Input';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Plus, Edit2, Play, Trash2, Loader, ArrowLeft } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';

const TemplatesPage = () => {
  const { orgId: orgIdStr } = useParams();
  const orgId = Number(orgIdStr);
  const [orgName, setOrgName] = useState('');
  const [templates, setTemplates] = useState<Template[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [inventories, setInventories] = useState<Inventory[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [executionPacks, setExecutionPacks] = useState<any[]>([]);
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
  const [formMsg, setFormMsg] = useState('');

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
        // Scope everything to the org from the route.
        const byOrg = <T extends { organization_id?: number }>(arr: T[]) => arr.filter(x => x.organization_id === orgId);
        setTemplates(byOrg(unwrap<Template>(templatesData)));
        setProjects(byOrg(unwrap<Project>(projectsData)));
        setInventories(byOrg(unwrap<Inventory>(inventoriesData)));
        setCredentials(byOrg(unwrap<Credential>(credentialsData)));
        setExecutionPacks(packsData || []);
        setOrgName(unwrap<{ id: number; name: string }>(orgsData).find(o => o.id === orgId)?.name ?? `Org ${orgId}`);
      } catch (err) {
        console.error('Failed to load data', err);
      } finally {
        setLoading(false);
      }
    };
    fetchData();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orgId]);

  const openCreateModal = () => {
    setEditingTemplate(null);
    setFormData({ organization_id: orgId }); // org fixed by the route
    setVarsText('');
    setSurvey([]);
    setFormMsg('');
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
    setFormMsg('');
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
    setFormMsg('');
    let extra_vars: any = {};
    if (varsText.trim()) {
      try { extra_vars = JSON.parse(varsText); }
      catch { setFormMsg('Variables must be valid JSON'); return; }
    }
    if (!editingTemplate && !formData.organization_id) { setFormMsg('Select an organization'); return; }
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
    } catch (err: any) {
      // Surface the failure in the modal instead of failing silently.
      setFormMsg(err?.message || 'Failed to save template');
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
    const t = templates.find(t => t.id === id);
    if (!(await confirmDialog(`Delete template "${t?.name ?? id}"? This cannot be undone.`))) return;
    try {
      await api.deleteTemplate(id);
      setTemplates(templates.filter(t => t.id !== id));
    } catch (err: any) {
      toast.error(err?.message || 'Failed to delete template');
    }
  };

  if (loading) {
    return (
      <PageSpinner />
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-end">
        <div>
          <Link to="/templates" className="inline-flex items-center gap-1 text-sm text-gray-500 hover:text-brand-600">
            <ArrowLeft size={14} /> Organizations
          </Link>
          <h1 className="text-2xl font-bold text-gray-900 mt-1">{orgName} · Templates</h1>
        </div>
        <Button onClick={openCreateModal} icon={<Plus size={16} />}>
          Add Template
        </Button>
      </div>

      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
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
        </div>
      </Card>

      <Modal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={editingTemplate ? "Edit Template" : "New Job Template"}
        size="lg"
      >
        <form onSubmit={handleSubmit} className="space-y-4">
          <Input
            label="Name"
            type="text"
            required
            value={formData.name || ''}
            onChange={e => setFormData({ ...formData, name: e.target.value })}
          />
          <Input
            label="Description"
            type="text"
            value={formData.description || ''}
            onChange={e => setFormData({ ...formData, description: e.target.value })}
          />
          <div className="grid grid-cols-2 gap-4">
            <Select
              label="Project"
              value={formData.project_id || ''}
              onChange={e => setFormData({ ...formData, project_id: Number(e.target.value) })}
            >
              <option value="">Select Project</option>
              {projects.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
            </Select>
            <Select
              label="Inventory"
              value={formData.inventory_id || ''}
              onChange={e => setFormData({ ...formData, inventory_id: Number(e.target.value) })}
            >
              <option value="">Select Inventory</option>
              {inventories.map(i => <option key={i.id} value={i.id}>{i.name}</option>)}
            </Select>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <Input
              label="Playbook"
              type="text"
              placeholder="site.yml"
              value={formData.playbook || ''}
              onChange={e => setFormData({ ...formData, playbook: e.target.value })}
            />
            <Select
              label="Credential"
              value={formData.credential_id || ''}
              onChange={e => setFormData({ ...formData, credential_id: Number(e.target.value) })}
            >
              <option value="">Select Credential</option>
              {credentials.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
            </Select>
          </div>
          <Select
            label="Execution Pack"
            hint="The self-contained Python+Ansible runtime this job runs in (pushed to the host). Default = the standard pack."
            value={formData.execution_pack_id || ''}
            onChange={e => setFormData({ ...formData, execution_pack_id: e.target.value ? Number(e.target.value) : undefined })}
          >
            <option value="">Default</option>
            {executionPacks.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
          </Select>
          <Textarea
            label="Default Variables (JSON)"
            rows={4}
            placeholder={'{\n  "key": "value"\n}'}
            className="font-mono text-sm"
            value={varsText}
            onChange={e => setVarsText(e.target.value)}
          />
          <Input
            label="Limit (default host pattern)"
            type="text"
            placeholder="e.g. web* or host1:host2"
            value={formData.limit || ''}
            onChange={e => setFormData({ ...formData, limit: e.target.value })}
          />
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
            <label className="flex items-center gap-2 text-sm text-gray-700 mt-3">
              <input
                type="checkbox"
                checked={!!formData.allow_simultaneous}
                onChange={e => setFormData({ ...formData, allow_simultaneous: e.target.checked })}
              />
              Allow simultaneous runs (off = a launch is refused while a run is still active)
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
          {formMsg && <p className="mt-4 text-sm text-red-600">{formMsg}</p>}
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
            {launchTpl.survey_enabled && (launchTpl.survey_spec?.spec || []).map((q, i) => {
              const label = q.question_name || q.variable;
              if (q.type === 'textarea') return (
                <Textarea key={i} label={label} required={q.required} rows={3}
                  value={launchAnswers[q.variable] || ''}
                  onChange={e => setLaunchAnswers({ ...launchAnswers, [q.variable]: e.target.value })} />
              );
              if (q.type === 'multiplechoice') return (
                <Select key={i} label={label} required={q.required}
                  value={launchAnswers[q.variable] || ''}
                  onChange={e => setLaunchAnswers({ ...launchAnswers, [q.variable]: e.target.value })}>
                  <option value="">Select…</option>
                  {(q.choices || '').split('\n').map(c => c.trim()).filter(Boolean).map(c => <option key={c} value={c}>{c}</option>)}
                </Select>
              );
              return (
                <Input key={i} label={label} required={q.required}
                  type={q.type === 'password' ? 'password' : q.type === 'integer' ? 'number' : 'text'}
                  value={launchAnswers[q.variable] || ''}
                  onChange={e => setLaunchAnswers({ ...launchAnswers, [q.variable]: e.target.value })} />
              );
            })}
            {!launchTpl.survey_enabled && launchTpl.ask_variables_on_launch && (
              <Textarea label="Variables (JSON)" rows={4} placeholder={'{\n  "key": "value"\n}'} className="font-mono text-sm"
                value={launchVars} onChange={e => setLaunchVars(e.target.value)} />
            )}
            {launchTpl.ask_limit_on_launch && (
              <Input label="Limit" type="text" placeholder="host pattern"
                value={launchLimit} onChange={e => setLaunchLimit(e.target.value)} />
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