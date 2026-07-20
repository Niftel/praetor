import React, { useState, useEffect, useMemo } from 'react';
import { useNavigate, useParams, Link } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Template, Project, Inventory, Credential, SurveyQuestion, Workflow, Job, WorkflowRunSummary } from '../types';
import { Input, Textarea, Select } from '../components/ui/Input';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Plus, Search, Check, Trash2, Play, ArrowLeft, GitFork, FileText, Pencil } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';
import WorkflowLaunchModal, { WorkflowLaunchOptions } from '../components/WorkflowLaunchModal';

type Editing = number | 'new' | null;

const Toggle: React.FC<{ on: boolean; onChange: (v: boolean) => void }> = ({ on, onChange }) => (
  <button type="button" onClick={() => onChange(!on)}
    className={`relative w-9 h-[21px] rounded-full shrink-0 transition-colors ${on ? 'bg-acc' : 'bg-line2'}`}>
    <span className={`absolute top-[2.5px] w-4 h-4 rounded-full transition-transform ${on ? 'translate-x-[15px] bg-[#06231e]' : 'translate-x-[2.5px] bg-[#c3c9d4]'}`} />
  </button>
);

const TogRow: React.FC<{ title: string; sub: string; on: boolean; onChange: (v: boolean) => void }> = ({ title, sub, on, onChange }) => (
  <div className="flex items-center gap-4 py-2.5">
    <div className="flex-1">
      <div className="text-[12.5px] text-ink2">{title}</div>
      <div className="font-mono text-[10.5px] text-dim mt-0.5">{sub}</div>
    </div>
    <Toggle on={on} onChange={onChange} />
  </div>
);

const Row: React.FC<{ label: string; hint?: string; top?: boolean; children: React.ReactNode }> = ({ label, hint, top, children }) => (
  <div className={`grid grid-cols-[158px_1fr] gap-6 py-2.5 ${top ? 'items-start' : 'items-center'}`}>
    <label className="text-[12.5px] text-ink2">{label}{hint && <span className="block font-mono text-[10px] text-dim mt-1 leading-snug">{hint}</span>}</label>
    {children}
  </div>
);

const uinp = 'w-full max-w-[320px] bg-transparent border-b border-line2 focus:border-acc hover:border-mut text-ink font-mono text-[13px] py-1.5 outline-none placeholder:text-faint';
const usel = 'min-w-[240px] max-w-[320px] bg-transparent border-b border-line2 focus:border-acc hover:border-mut text-ink font-mono text-[13px] py-1.5 outline-none';

const SLabel: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <div className="font-mono text-[10px] tracking-[0.16em] uppercase text-mut mb-1.5">{children}</div>
);

const TemplatesPage = () => {
  const navigate = useNavigate();
  const { orgId: orgIdStr } = useParams();
  const orgId = Number(orgIdStr);
  const [orgName, setOrgName] = useState('');
  const [templates, setTemplates] = useState<Template[]>([]);
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [workflowRuns, setWorkflowRuns] = useState<WorkflowRunSummary[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [inventories, setInventories] = useState<Inventory[]>([]);
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [executionPacks, setExecutionPacks] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState('');
  const [catalogType, setCatalogType] = useState<'all' | 'job' | 'workflow'>('all');

  const [editing, setEditing] = useState<Editing>(null);
  const [formData, setFormData] = useState<Partial<Template>>({});
  const [varsText, setVarsText] = useState('');
  const [survey, setSurvey] = useState<SurveyQuestion[]>([]);
  const [formMsg, setFormMsg] = useState('');
  const [saving, setSaving] = useState(false);

  // Notifications
  const [notifTargets, setNotifTargets] = useState<any[]>([]);
  const [notifAttached, setNotifAttached] = useState<any[]>([]);
  const [notifTypes, setNotifTypes] = useState<any[]>([]);
  const [newNotif, setNewNotif] = useState<{ name: string; notification_type: string; config: Record<string, string> }>({ name: '', notification_type: 'webhook', config: {} });

  // Launch dialog
  const [launchTpl, setLaunchTpl] = useState<Template | null>(null);
  const [launchVars, setLaunchVars] = useState('');
  const [launchLimit, setLaunchLimit] = useState('');
  const [launchAnswers, setLaunchAnswers] = useState<Record<string, string>>({});
  const [launchMsg, setLaunchMsg] = useState('');
  const [launchWorkflow, setLaunchWorkflow] = useState<Workflow | null>(null);

  const blankQuestion = (): SurveyQuestion => ({ variable: '', question_name: '', type: 'text', required: false, default: '' });
  const updateQ = (i: number, patch: Partial<SurveyQuestion>) => setSurvey(prev => prev.map((q, j) => (j === i ? { ...q, ...patch } : q)));

  useEffect(() => {
    (async () => {
      try {
        setLoading(true);
        const [t, w, j, wr, p, i, c, packs, orgs] = await Promise.all([
          api.getTemplates(), api.getWorkflows(), api.getJobs(), api.getWorkflowJobs(), api.getProjects(), api.getInventories(), api.getCredentials(), api.getExecutionPacks(), api.getOrganizations().catch(() => []),
        ]);
        const byOrg = <T extends { organization_id?: number }>(arr: T[]) => arr.filter(x => x.organization_id === orgId);
        setTemplates(byOrg(unwrap<Template>(t)));
        setWorkflows(byOrg(unwrap<Workflow>(w)));
        setJobs(unwrap<Job>(j));
        setWorkflowRuns(byOrg(unwrap<WorkflowRunSummary>(wr)));
        setProjects(byOrg(unwrap<Project>(p)));
        setInventories(byOrg(unwrap<Inventory>(i)));
        setCredentials(byOrg(unwrap<Credential>(c)));
        setExecutionPacks(packs || []);
        setOrgName(unwrap<{ id: number; name: string }>(orgs).find(o => o.id === orgId)?.name ?? `Org ${orgId}`);
      } catch (err) { console.error('Failed to load data', err); }
      finally { setLoading(false); }
    })();
  }, [orgId]);

  const startNew = () => { setEditing('new'); setFormData({ organization_id: orgId }); setVarsText(''); setSurvey([]); setFormMsg(''); };

  const startEdit = (t: Template) => {
    setEditing(t.id);
    setFormData(t);
    setVarsText(t.extra_vars && Object.keys(t.extra_vars).length ? JSON.stringify(t.extra_vars, null, 2) : '');
    setSurvey(t.survey_spec?.spec || []);
    setFormMsg('');
    setNewNotif({ name: '', notification_type: 'webhook', config: {} });
    api.getNotificationTypes().then(d => setNotifTypes(d || [])).catch(() => setNotifTypes([]));
    if (t.organization_id) {
      api.getNotificationTemplates(t.organization_id).then(d => setNotifTargets(d || [])).catch(() => setNotifTargets([]));
      api.getTemplateNotifications(t.id).then(d => setNotifAttached(d || [])).catch(() => setNotifAttached([]));
    }
  };

  const isAttached = (ntId: number, event: string) => notifAttached.some(a => a.notification_template_id === ntId && a.event === event);
  const toggleNotif = async (ntId: number, event: string) => {
    if (typeof editing !== 'number') return;
    if (isAttached(ntId, event)) await api.detachTemplateNotification(editing, ntId, event);
    else await api.attachTemplateNotification(editing, { notification_template_id: ntId, event });
    api.getTemplateNotifications(editing).then(d => setNotifAttached(d || [])).catch(() => { });
  };
  const notifFields = (): { id: string; label: string; type: string }[] =>
    notifTypes.find(t => t.type === newNotif.notification_type)?.fields || [{ id: 'url', label: 'URL', type: 'text' }];
  const addNotifTarget = async () => {
    if (typeof editing !== 'number' || !newNotif.name.trim() || !formData.organization_id) return;
    if (notifFields().some(f => !(newNotif.config[f.id] || '').trim())) return;
    await api.createNotificationTemplate({ organization_id: formData.organization_id, name: newNotif.name, notification_type: newNotif.notification_type, config: newNotif.config });
    setNewNotif({ name: '', notification_type: 'webhook', config: {} });
    api.getNotificationTemplates(formData.organization_id).then(d => setNotifTargets(d || [])).catch(() => { });
  };

  const save = async () => {
    setFormMsg('');
    let extra_vars: any = {};
    if (varsText.trim()) { try { extra_vars = JSON.parse(varsText); } catch { setFormMsg('Variables must be valid JSON'); return; } }
    if (!formData.name?.trim()) { setFormMsg('Name is required'); return; }
    const payload = { ...formData, extra_vars, survey_spec: { spec: survey.filter(q => q.variable.trim()) } };
    setSaving(true);
    try {
      if (typeof editing === 'number') {
        const updated = await api.updateTemplate(editing, payload);
        setTemplates(ts => ts.map(t => (t.id === editing ? updated : t)));
        toast.success('Template saved');
      } else {
        const created = await api.createTemplate(payload);
        setTemplates(ts => [...ts, created]);
        setEditing(created.id);
        toast.success('Template created');
      }
    } catch (err: any) { setFormMsg(err?.message || 'Failed to save template'); }
    finally { setSaving(false); }
  };

  const remove = async (id: number) => {
    const t = templates.find(t => t.id === id);
    if (!(await confirmDialog(`Delete template "${t?.name ?? id}"?`, { destructive: true, confirmText: 'Delete' }))) return;
    try { await api.deleteTemplate(id); setTemplates(ts => ts.filter(t => t.id !== id)); if (editing === id) setEditing(null); }
    catch (err: any) { toast.error(err?.message || 'Failed to delete template'); }
  };

  const openLaunch = (t: Template) => {
    setLaunchTpl(t); setLaunchVars(''); setLaunchLimit(t.limit || '');
    const seed: Record<string, string> = {};
    (t.survey_spec?.spec || []).forEach(q => { seed[q.variable] = q.default || ''; });
    setLaunchAnswers(seed); setLaunchMsg('');
  };
  const doLaunch = async () => {
    if (!launchTpl) return;
    const payload: any = { unified_job_template_id: launchTpl.unified_job_template_id, name: launchTpl.name };
    if (launchTpl.survey_enabled) {
      const answers: Record<string, any> = {};
      for (const q of launchTpl.survey_spec?.spec || []) {
        const raw = launchAnswers[q.variable];
        if (raw === undefined || raw === '') continue;
        answers[q.variable] = q.type === 'integer' ? Number(raw) : raw;
      }
      payload.extra_vars = answers;
    } else if (launchTpl.ask_variables_on_launch && launchVars.trim()) {
      try { payload.extra_vars = JSON.parse(launchVars); } catch { setLaunchMsg('Variables must be valid JSON'); return; }
    }
    if (launchTpl.ask_limit_on_launch && launchLimit.trim()) payload.limit = launchLimit.trim();
    try { await api.launchJob(payload); setLaunchTpl(null); toast.success('Launched'); }
    catch (err) { setLaunchMsg('Launch failed'); console.error(err); }
  };

  const doLaunchWorkflow = async (options: WorkflowLaunchOptions, signal?: AbortSignal) => {
    if (!launchWorkflow) return;
    const response = await api.launchWorkflow(launchWorkflow.id, options, signal);
    setLaunchWorkflow(null);
    navigate(`/workflows/runs/${response.workflow_job_id}`);
  };

  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return q ? templates.filter(t => t.name.toLowerCase().includes(q) || (t.playbook || '').toLowerCase().includes(q)) : templates;
  }, [templates, filter]);

  const catalog = useMemo(() => {
    const q = filter.trim().toLowerCase();
    const latestJob = (template: Template) => jobs
      .filter(job => job.unified_job_template_id === (template.unified_job_template_id || template.id))
      .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())[0];
    const latestWorkflow = (workflow: Workflow) => workflowRuns
      .filter(run => run.workflow_template_id === workflow.id)
      .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())[0];
    return [
      ...templates.map(template => ({ key: `job-${template.id}`, kind: 'job' as const, id: template.id, name: template.name, description: template.playbook || 'No playbook selected', item: template, latest: latestJob(template) })),
      ...workflows.map(workflow => ({ key: `workflow-${workflow.id}`, kind: 'workflow' as const, id: workflow.id, name: workflow.name, description: workflow.nodes?.length ? `${workflow.nodes.length} workflow nodes` : 'Workflow template', item: workflow, latest: latestWorkflow(workflow) })),
    ].filter(item => (catalogType === 'all' || item.kind === catalogType) && (!q || item.name.toLowerCase().includes(q) || item.description.toLowerCase().includes(q)))
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [templates, workflows, jobs, workflowRuns, filter, catalogType]);

  const set = (patch: Partial<Template>) => setFormData(f => ({ ...f, ...patch }));

  if (loading) return <PageSpinner />;

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink">
      <div className="flex items-center gap-4 min-h-[68px] px-6 py-3 border-b border-line shrink-0">
        <Link to="/templates" className="w-7 h-7 grid place-items-center rounded-md border border-line2 text-mut hover:text-ink hover:border-white/20 transition-colors" title="All organizations"><ArrowLeft size={15} /></Link>
        <div className="min-w-0">
          <div className="flex items-baseline gap-3"><h1 className="text-[19px] font-semibold tracking-tight">Automation templates</h1><span className="font-mono text-[11px] text-dim">{templates.length + workflows.length} total</span></div>
          <p className="text-[11.5px] text-mut mt-0.5">{orgName} · reusable definitions for playbook and workflow execution</p>
        </div>
      </div>

      {editing === null ? (
        <div className="flex-1 min-h-0 flex flex-col">
          <div className="flex flex-wrap items-center gap-2 px-6 py-3 border-b border-line shrink-0">
            <label className="relative min-w-[260px] max-[700px]:w-full">
              <span className="sr-only">Search automation templates</span>
              <Search size={13} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-dim pointer-events-none" />
              <input value={filter} onChange={e => setFilter(e.target.value)} placeholder="Search templates by name or content" className="h-[30px] w-full pl-8 pr-3 rounded-md bg-panel border border-line2 text-xs text-ink placeholder:text-mut hover:border-white/20 focus:border-acc/60" />
            </label>
            <div className="flex items-center gap-1 ml-1" aria-label="Filter automation templates by type">
              {([
                ['all', 'All', templates.length + workflows.length],
                ['job', 'Job templates', templates.length],
                ['workflow', 'Workflows', workflows.length],
              ] as const).map(([key, label, count]) => (
                <button key={key} onClick={() => setCatalogType(key)} aria-pressed={catalogType === key} className={`h-[30px] px-2.5 rounded-md border font-mono text-[10.5px] transition-colors ${catalogType === key ? 'border-line2 bg-white/5 text-ink' : 'border-transparent text-mut hover:text-ink'}`}>{label} <span className="ml-1 text-dim tabular-nums">{count}</span></button>
              ))}
            </div>
            <div className="ml-auto flex items-center gap-2 max-[700px]:ml-0">
              <button onClick={() => navigate(`/workflows/org/${orgId}/builder`)} className="h-8 px-3 rounded-md text-[11px] font-medium text-mut hover:text-ink hover:bg-white/5 inline-flex items-center gap-1.5"><GitFork size={12} /> New workflow</button>
              <button onClick={startNew} className="h-8 px-3 rounded-md border border-line2 text-[11px] font-medium text-ink2 hover:text-ink hover:border-white/20 inline-flex items-center gap-1.5"><Plus size={13} /> New job template</button>
            </div>
          </div>

          <div className="grid grid-cols-[minmax(260px,1fr)_130px_160px_190px_170px] items-center h-[34px] px-6 border-b border-line font-mono text-[9.5px] tracking-[0.1em] uppercase text-dim max-[900px]:grid-cols-[minmax(220px,1fr)_130px_150px]">
            <span>Name</span><span>Type</span><span>Last status</span><span className="max-[900px]:hidden">Last run</span><span className="text-right">Actions</span>
          </div>
          <div className="flex-1 overflow-auto scroll-tint">
            {catalog.map(entry => {
              const latest = entry.latest as Job | WorkflowRunSummary | undefined;
              const latestDate = latest?.created_at ? new Date(latest.created_at).toLocaleString([], { dateStyle: 'short', timeStyle: 'short' }) : 'Never run';
              const status = latest?.status || 'never run';
              const isWorkflow = entry.kind === 'workflow';
              const TypeIcon = isWorkflow ? GitFork : FileText;
              return (
                <div key={entry.key} className="group grid grid-cols-[minmax(260px,1fr)_130px_160px_190px_170px] items-center min-h-[58px] px-6 border-b border-line hover:bg-white/[0.025] max-[900px]:grid-cols-[minmax(220px,1fr)_130px_150px]">
                  <button onClick={() => isWorkflow ? navigate(`/workflows/org/${orgId}/builder/${entry.id}`) : startEdit(entry.item as Template)} className="min-w-0 text-left pr-4 group">
                    <span className="block text-[13px] font-medium text-ink2 truncate group-hover:text-acc">{entry.name}</span>
                    <span className="block font-mono text-[10.5px] text-dim mt-0.5 truncate">{entry.description}</span>
                  </button>
                  <span className="inline-flex items-center gap-1.5 text-[11px] text-mut"><TypeIcon size={12} /> {isWorkflow ? 'Workflow' : 'Job template'}</span>
                  <span className={`inline-flex items-center gap-2 text-[11px] ${status === 'successful' ? 'text-ok' : status === 'failed' || status === 'error' ? 'text-err' : status === 'running' ? 'text-run' : 'text-mut'}`}><span className={`w-1.5 h-1.5 rounded-full ${status === 'successful' ? 'bg-ok' : status === 'failed' || status === 'error' ? 'bg-err' : status === 'running' ? 'bg-run' : 'bg-dim'}`} />{status}</span>
                  <span className="font-mono text-[11px] text-mut tabular-nums max-[900px]:hidden">{latestDate}</span>
                  <div className="flex justify-end gap-1.5">
                    <button onClick={() => isWorkflow ? navigate(`/workflows/org/${orgId}/builder/${entry.id}`) : startEdit(entry.item as Template)} className="h-8 px-2.5 rounded-md text-[11px] text-dim hover:text-ink hover:bg-white/5 inline-flex items-center gap-1.5"><Pencil size={12} /> Edit</button>
                    <button onClick={() => isWorkflow ? setLaunchWorkflow(entry.item as Workflow) : openLaunch(entry.item as Template)} className="h-8 px-3 rounded-md border border-line2 text-[11px] font-medium text-ink2 hover:text-acc hover:border-acc/40 hover:bg-acc/[0.05] inline-flex items-center gap-1.5"><Play size={12} /> Launch</button>
                  </div>
                </div>
              );
            })}
            {catalog.length === 0 && <div className="px-6 py-12 text-center text-sm text-mut">No automation templates match this search.</div>}
          </div>
        </div>
      ) : (
      <div className="grid grid-cols-[288px_1fr] flex-1 min-h-0 max-[820px]:grid-cols-1">
        {/* Catalog */}
        <div className="flex flex-col min-h-0 border-r border-line bg-tree max-[820px]:hidden">
          <div className="flex items-center gap-2.5 h-[46px] px-4 border-b border-line shrink-0">
            <Search size={14} className="text-dim shrink-0" />
            <input value={filter} onChange={e => setFilter(e.target.value)} placeholder="Filter templates" className="flex-1 bg-transparent border-none outline-none text-[12.5px] text-ink placeholder:text-dim" />
          </div>
          <div className="flex items-center h-[34px] px-4 mt-1.5 shrink-0">
            <span className="font-mono text-[9px] tracking-[0.16em] uppercase text-dim">Job templates</span>
            <button onClick={startNew} className="ml-auto text-dim hover:text-ink" title="New template"><Plus size={15} /></button>
          </div>
          <div className="flex-1 overflow-auto scroll-tint px-2.5 pb-6">
            {editing === 'new' && (
              <div className="p-2.5 rounded-lg bg-acc/[0.09] shadow-[inset_0_0_0_1px_rgba(77,224,200,0.5)]">
                <div className="flex items-center gap-2"><span className="text-[13px] font-medium text-ink">{formData.name || 'New template'}</span><span className="ml-auto font-mono text-[9px] text-acc uppercase tracking-[0.1em]">new</span></div>
              </div>
            )}
            {shown.map(t => {
              const sel = editing === t.id;
              const inv = inventories.find(i => i.id === t.inventory_id);
              return (
                <button key={t.id} onClick={() => startEdit(t)} className={`w-full text-left p-2.5 rounded-lg flex flex-col gap-1 ${sel ? 'bg-acc/[0.09]' : 'hover:bg-white/[0.028]'}`}>
                  <div className="flex items-center gap-2"><span className={`text-[13px] font-medium ${sel ? 'text-ink' : 'text-ink2'}`}>{t.name}</span></div>
                  <div className="font-mono text-[10.5px] text-dim truncate">{t.playbook || '—'}<span className="text-faint mx-1.5">›</span>{inv?.name || 'no inventory'}</div>
                </button>
              );
            })}
            {shown.length === 0 && editing !== 'new' && <p className="px-3 py-6 text-[12px] text-dim text-center">No templates.</p>}
          </div>
        </div>

        {/* Job template editor */}
          <div className="flex flex-col min-h-0 bg-bg">
            <div className="flex items-start gap-5 px-10 pt-5 pb-4 border-b border-line shrink-0 max-[820px]:px-5">
              <div className="flex-1 min-w-0">
                <input value={formData.name || ''} onChange={e => set({ name: e.target.value })} placeholder="Template name"
                  className="w-full text-[22px] font-semibold tracking-tight text-ink bg-transparent border-b border-transparent hover:border-line focus:border-acc pb-1 outline-none" />
                <input value={formData.description || ''} onChange={e => set({ description: e.target.value })} placeholder="Describe what this template does…"
                  className="w-full mt-2 text-[12.5px] text-mut bg-transparent border-b border-transparent hover:border-line focus:border-acc pb-0.5 outline-none" />
              </div>
              <div className="flex items-center gap-2.5 pt-1.5 shrink-0">
                {typeof editing === 'number' && <button onClick={() => openLaunch(templates.find(t => t.id === editing)!)} className="h-[34px] px-3.5 rounded-lg text-[12.5px] font-medium flex items-center gap-1.5 border border-line2 text-ink2 hover:border-white/25"><Play size={13} /> Launch</button>}
                <button onClick={() => setEditing(null)} className="h-[34px] px-3.5 rounded-lg text-[12.5px] font-medium border border-line2 text-ink2 hover:border-white/25">Cancel</button>
                <Button onClick={save} disabled={saving} icon={<Check size={14} />}>Save</Button>
              </div>
            </div>

            <div className="flex-1 overflow-auto scroll-tint px-10 py-6 max-[820px]:px-5">
              <div className="max-w-[640px]">
                {/* What it runs */}
                <div>
                  <SLabel>What it runs</SLabel>
                  <Row label="Project">
                    <select className={usel} value={formData.project_id || ''} onChange={e => set({ project_id: Number(e.target.value) })}>
                      <option value="" className="bg-panel">Select project</option>
                      {projects.map(p => <option key={p.id} value={p.id} className="bg-panel">{p.name}</option>)}
                    </select>
                  </Row>
                  <Row label="Playbook" hint="path within the project repo">
                    <input className={uinp} placeholder="site.yml" value={formData.playbook || ''} onChange={e => set({ playbook: e.target.value })} />
                  </Row>
                  <Row label="Inventory">
                    <select className={usel} value={formData.inventory_id || ''} onChange={e => set({ inventory_id: Number(e.target.value) })}>
                      <option value="" className="bg-panel">Select inventory</option>
                      {inventories.map(i => <option key={i.id} value={i.id} className="bg-panel">{i.name}</option>)}
                    </select>
                  </Row>
                  <Row label="Credential">
                    <select className={usel} value={formData.credential_id || ''} onChange={e => set({ credential_id: Number(e.target.value) })}>
                      <option value="" className="bg-panel">Select credential</option>
                      {credentials.map(c => <option key={c.id} value={c.id} className="bg-panel">{c.name}</option>)}
                    </select>
                  </Row>
                  <Row label="Execution pack" hint="runtime pushed to the host">
                    <select className={usel} value={formData.execution_pack_id || ''} onChange={e => set({ execution_pack_id: e.target.value ? Number(e.target.value) : undefined })}>
                      <option value="" className="bg-panel">Default pack</option>
                      {executionPacks.map(p => <option key={p.id} value={p.id} className="bg-panel">{p.name}</option>)}
                    </select>
                  </Row>
                </div>

                {/* Defaults */}
                <div className="mt-8 pt-6 border-t border-line">
                  <SLabel>Defaults</SLabel>
                  <Row label="Variables" hint="applied unless overridden at launch" top>
                    <textarea className="w-full max-w-[420px] rounded-lg border border-line bg-[#070809] p-3 font-mono text-[12.5px] leading-relaxed text-ink2 outline-none focus:border-acc/50" rows={4}
                      placeholder={'{\n  "app_env": "production"\n}'} value={varsText} onChange={e => setVarsText(e.target.value)} />
                  </Row>
                  <Row label="Limit" hint="default host pattern">
                    <input className={`${uinp} max-w-[150px]`} placeholder="web*" value={formData.limit || ''} onChange={e => set({ limit: e.target.value })} />
                  </Row>
                  <TogRow title="Use fact cache" sub="persist & reuse gathered facts across runs" on={!!formData.use_fact_cache} onChange={v => set({ use_fact_cache: v })} />
                  <TogRow title="Allow simultaneous runs" sub="off = a launch is refused while a run is active" on={!!formData.allow_simultaneous} onChange={v => set({ allow_simultaneous: v })} />
                </div>

                {/* Prompt on launch */}
                <div className="mt-8 pt-6 border-t border-line">
                  <SLabel>Prompt on launch</SLabel>
                  <TogRow title="Ask for variables" sub="let the operator pass extra_vars at launch" on={!!formData.ask_variables_on_launch} onChange={v => set({ ask_variables_on_launch: v })} />
                  <TogRow title="Ask for limit" sub="let the operator narrow the host pattern" on={!!formData.ask_limit_on_launch} onChange={v => set({ ask_limit_on_launch: v })} />
                  <TogRow title="Enable survey" sub="collect structured answers before the run" on={!!formData.survey_enabled} onChange={v => set({ survey_enabled: v })} />

                  {formData.survey_enabled && (
                    <div className="mt-3 space-y-3">
                      {survey.map((q, i) => (
                        <div key={i} className="rounded-xl border border-line bg-panel2 p-3.5">
                          <div className="grid grid-cols-2 gap-x-4 gap-y-3">
                            <SurveyField label="Variable"><input className={`${uinp} max-w-none`} placeholder="app_version" value={q.variable} onChange={e => updateQ(i, { variable: e.target.value })} /></SurveyField>
                            <SurveyField label="Question"><input className={`${uinp} max-w-none`} placeholder="Which release?" value={q.question_name} onChange={e => updateQ(i, { question_name: e.target.value })} /></SurveyField>
                            <SurveyField label="Type">
                              <select className={`${usel} min-w-0 max-w-none`} value={q.type} onChange={e => updateQ(i, { type: e.target.value as SurveyQuestion['type'] })}>
                                {['text', 'textarea', 'password', 'integer', 'multiplechoice'].map(t => <option key={t} value={t} className="bg-panel">{t}</option>)}
                              </select>
                            </SurveyField>
                            <SurveyField label="Default"><input className={`${uinp} max-w-none`} value={q.default || ''} onChange={e => updateQ(i, { default: e.target.value })} /></SurveyField>
                          </div>
                          {q.type === 'multiplechoice' && (
                            <textarea rows={2} placeholder="one choice per line" className="mt-3 w-full rounded-lg border border-line bg-[#070809] p-2 font-mono text-[12px] text-ink2 outline-none focus:border-acc/50" value={q.choices || ''} onChange={e => updateQ(i, { choices: e.target.value })} />
                          )}
                          <div className="flex items-center gap-5 mt-3 pt-3 border-t border-line">
                            <label className="flex items-center gap-2 font-mono text-[11px] text-mut cursor-pointer"><Toggle on={q.required} onChange={v => updateQ(i, { required: v })} /> Required</label>
                            <button type="button" className="ml-auto font-mono text-[11px] text-err hover:underline" onClick={() => setSurvey(survey.filter((_, j) => j !== i))}>remove</button>
                          </div>
                        </div>
                      ))}
                      <button type="button" className="flex items-center gap-2 font-mono text-[12px] text-dim hover:text-acc" onClick={() => setSurvey([...survey, blankQuestion()])}><Plus size={13} /> add question</button>
                    </div>
                  )}
                </div>

                {/* Notifications (edit only) */}
                {typeof editing === 'number' && (
                  <div className="mt-8 pt-6 border-t border-line">
                    <SLabel>Notifications</SLabel>
                    {notifTargets.length === 0 && <p className="font-mono text-[11px] text-dim mb-2">No notification targets in this org yet — add one below.</p>}
                    {notifTargets.map(nt => (
                      <div key={nt.id} className="flex items-center gap-3 py-1.5 text-[13px]">
                        <span className="flex-1 truncate">{nt.name} <span className="font-mono text-[11px] text-dim">({nt.notification_type})</span></span>
                        {['success', 'error', 'started'].map(ev => (
                          <label key={ev} className="flex items-center gap-1.5 font-mono text-[11px] text-mut cursor-pointer">
                            <input type="checkbox" className="accent-acc" checked={isAttached(nt.id, ev)} onChange={() => toggleNotif(nt.id, ev)} /> {ev}
                          </label>
                        ))}
                      </div>
                    ))}
                    <div className="flex flex-wrap gap-2 mt-3 items-center">
                      <input placeholder="name" className={`${uinp} max-w-[140px]`} value={newNotif.name} onChange={e => setNewNotif({ ...newNotif, name: e.target.value })} />
                      <select className={`${usel} min-w-0 max-w-[130px]`} value={newNotif.notification_type} onChange={e => setNewNotif({ ...newNotif, notification_type: e.target.value, config: {} })}>
                        {(notifTypes.length ? notifTypes.map(t => t.type) : ['webhook', 'slack']).map((tp: string) => <option key={tp} value={tp} className="bg-panel">{tp}</option>)}
                      </select>
                      {notifFields().map(f => (
                        <input key={f.id} placeholder={f.label} type={f.type === 'password' ? 'password' : 'text'} className={`${uinp} flex-1 min-w-[120px] max-w-none`}
                          value={newNotif.config[f.id] || ''} onChange={e => setNewNotif({ ...newNotif, config: { ...newNotif.config, [f.id]: e.target.value } })} />
                      ))}
                      <Button size="sm" variant="secondary" type="button" onClick={addNotifTarget}>Add</Button>
                    </div>
                  </div>
                )}

                {/* Webhook trigger */}
                <div className="mt-8 pt-6 border-t border-line">
                  <TogRow title="Enable webhook trigger" sub="launch this template from an inbound Git webhook" on={!!formData.webhook_enabled} onChange={v => set({ webhook_enabled: v })} />
                  {formData.webhook_enabled && (
                    <div className="mt-2 space-y-2">
                      <select className={`${usel} min-w-0 max-w-[160px]`} value={formData.webhook_service || 'generic'} onChange={e => set({ webhook_service: e.target.value })}>
                        <option value="github" className="bg-panel">GitHub</option>
                        <option value="gitlab" className="bg-panel">GitLab</option>
                        <option value="generic" className="bg-panel">Generic</option>
                      </select>
                      {typeof editing === 'number' && formData.webhook_key ? (
                        <div className="font-mono text-[11px] text-mut space-y-1 rounded-lg border border-line bg-panel2 p-3">
                          <div>URL: <span className="text-ink2 break-all">{window.location.origin}/api/v1/webhooks/job-templates/{editing}/{formData.webhook_service || 'generic'}</span></div>
                          <div>Secret: <span className="text-ink2 break-all">{formData.webhook_key}</span></div>
                        </div>
                      ) : <p className="font-mono text-[11px] text-dim">Save the template to generate the webhook URL and secret.</p>}
                    </div>
                  )}
                </div>

                {formMsg && <p className="mt-5 text-sm text-err">{formMsg}</p>}
                {typeof editing === 'number' && (
                  <div className="mt-8 pt-6 border-t border-line">
                    <button onClick={() => remove(editing)} className="flex items-center gap-2 text-[12.5px] text-err/90 hover:text-err"><Trash2 size={14} /> Delete template</button>
                  </div>
                )}
              </div>
            </div>
          </div>
      </div>
      )}

      {/* Launch modal */}
      <Modal isOpen={!!launchTpl} onClose={() => setLaunchTpl(null)} title={launchTpl ? `Launch: ${launchTpl.name}` : 'Launch'} size="md">
        {launchTpl && (
          <div className="space-y-4">
            {!launchTpl.survey_enabled && !launchTpl.ask_variables_on_launch && !launchTpl.ask_limit_on_launch && <p className="text-sm text-mut">This template runs with its saved configuration.</p>}
            {launchTpl.survey_enabled && (launchTpl.survey_spec?.spec || []).map((q, i) => {
              const label = q.question_name || q.variable;
              if (q.type === 'textarea') return <Textarea key={i} label={label} required={q.required} rows={3} value={launchAnswers[q.variable] || ''} onChange={e => setLaunchAnswers({ ...launchAnswers, [q.variable]: e.target.value })} />;
              if (q.type === 'multiplechoice') return (
                <Select key={i} label={label} required={q.required} value={launchAnswers[q.variable] || ''} onChange={e => setLaunchAnswers({ ...launchAnswers, [q.variable]: e.target.value })}>
                  <option value="">Select…</option>
                  {(q.choices || '').split('\n').map(c => c.trim()).filter(Boolean).map(c => <option key={c} value={c}>{c}</option>)}
                </Select>
              );
              return <Input key={i} label={label} required={q.required} type={q.type === 'password' ? 'password' : q.type === 'integer' ? 'number' : 'text'} value={launchAnswers[q.variable] || ''} onChange={e => setLaunchAnswers({ ...launchAnswers, [q.variable]: e.target.value })} />;
            })}
            {!launchTpl.survey_enabled && launchTpl.ask_variables_on_launch && <Textarea label="Variables (JSON)" rows={4} placeholder={'{\n  "key": "value"\n}'} className="font-mono text-sm" value={launchVars} onChange={e => setLaunchVars(e.target.value)} />}
            {launchTpl.ask_limit_on_launch && <Input label="Limit" placeholder="host pattern" value={launchLimit} onChange={e => setLaunchLimit(e.target.value)} />}
            {launchMsg && <p className="text-sm text-err">{launchMsg}</p>}
            <div className="mt-5 flex justify-end gap-3"><Button variant="secondary" onClick={() => setLaunchTpl(null)}>Cancel</Button><Button onClick={doLaunch} icon={<Play size={14} />}>Launch</Button></div>
          </div>
        )}
      </Modal>
      <WorkflowLaunchModal
        isOpen={!!launchWorkflow}
        workflowName={launchWorkflow?.name || 'Workflow'}
        organizationId={orgId}
        onClose={() => setLaunchWorkflow(null)}
        onLaunch={doLaunchWorkflow}
      />
    </div>
  );
};

const SurveyField: React.FC<{ label: string; children: React.ReactNode }> = ({ label, children }) => (
  <div>
    <div className="font-mono text-[9px] tracking-[0.12em] uppercase text-dim mb-1.5">{label}</div>
    {children}
  </div>
);

export default TemplatesPage;
