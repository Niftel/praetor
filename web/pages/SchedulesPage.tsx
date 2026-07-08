import React, { useState, useEffect } from 'react';
import { api, unwrap } from '../services/api';
import { Schedule, Template, Workflow, EventTrigger, WebhookTrigger } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import Modal from '../components/ui/Modal';
import { Input, Select } from '../components/ui/Input';
import { Calendar, Plus, Power, Loader, Trash2, Zap, Webhook, Copy, Pencil } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';

type TargetType = 'job' | 'workflow';

const EVENT_LABEL: Record<string, string> = {
  job_succeeded: 'a job succeeds',
  job_failed: 'a job fails',
  job_finished: 'a job finishes (success or fail)',
};

const SchedulesPage = () => {
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [eventTriggers, setEventTriggers] = useState<EventTrigger[]>([]);
  const [webhookTriggers, setWebhookTriggers] = useState<WebhookTrigger[]>([]);
  const [loading, setLoading] = useState(true);

  const [showSchedule, setShowSchedule] = useState(false);
  const [sched, setSched] = useState({ name: '', targetType: 'job' as TargetType, target: 0, rrule: 'FREQ=DAILY;INTERVAL=1' });

  const [showEvent, setShowEvent] = useState(false);
  const [editingEvtId, setEditingEvtId] = useState<number | null>(null);
  const [evt, setEvt] = useState({ name: '', event_type: 'job_finished', source: 0, targetType: 'workflow' as TargetType, target: 0 });

  const fetchData = async () => {
    try {
      setLoading(true);
      const [s, t, wf, et, wh] = await Promise.all([
        api.getSchedules().catch(() => []),
        api.getTemplates().catch(() => ({})),
        api.getWorkflows().catch(() => []),
        api.getEventTriggers().catch(() => []),
        api.getWebhookTriggers().catch(() => []),
      ]);
      setSchedules(s || []);
      setTemplates(unwrap(t));
      setWorkflows(wf || []);
      setEventTriggers(et || []);
      setWebhookTriggers(wh || []);
    } catch (err) {
      console.error('Failed to load triggers', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchData(); }, []);

  const templateUjt = (t: Template) => (t as any).unified_job_template_id || t.id;
  const templateNameByUjt = (ujt?: number | null) => templates.find(t => templateUjt(t) === ujt)?.name || (ujt ? `template ${ujt}` : '—');
  const workflowName = (id?: number | null) => workflows.find(w => w.id === id)?.name || (id ? `workflow ${id}` : '—');

  const scheduleTarget = (s: Schedule) =>
    s.workflow_template_id ? `Workflow: ${workflowName(s.workflow_template_id)}` : `Template: ${templateNameByUjt(s.unified_job_template_id)}`;

  // --- Schedules ---
  const toggleSchedule = async (id: number) => {
    const s = schedules.find(x => x.id === id);
    if (!s) return;
    try {
      await api.updateSchedule(id, { ...s, enabled: !s.enabled });
      setSchedules(schedules.map(x => x.id === id ? { ...x, enabled: !x.enabled } : x));
    } catch (err) { console.error(err); }
  };
  const createSchedule = async () => {
    if (!sched.name || !sched.target) return;
    const body: any = { name: sched.name, rrule: sched.rrule };
    if (sched.targetType === 'workflow') body.workflow_template_id = sched.target;
    else body.unified_job_template_id = sched.target;
    try {
      await api.createSchedule(body);
      setShowSchedule(false);
      setSched({ name: '', targetType: 'job', target: 0, rrule: 'FREQ=DAILY;INTERVAL=1' });
      fetchData();
    } catch (err) { console.error(err); toast.error('Failed to create schedule'); }
  };
  const deleteSchedule = async (id: number) => {
    if (!(await confirmDialog('Delete this schedule?'))) return;
    try { await api.deleteSchedule(id); fetchData(); } catch (err) { console.error(err); }
  };

  // --- Event triggers ---
  const openEventCreate = () => {
    setEditingEvtId(null);
    setEvt({ name: '', event_type: 'job_finished', source: 0, targetType: 'workflow', target: 0 });
    setShowEvent(true);
  };
  const openEventEdit = (t: EventTrigger) => {
    setEditingEvtId(t.id);
    setEvt({
      name: t.name, event_type: t.event_type, source: t.source_ujt_id || 0,
      targetType: t.workflow_template_id ? 'workflow' : 'job',
      target: t.workflow_template_id || t.unified_job_template_id || 0,
    });
    setShowEvent(true);
  };
  const saveEventTrigger = async () => {
    if (!evt.name || !evt.target) return;
    // Org is derived from the target so there's no separate org picker.
    const org = evt.targetType === 'workflow'
      ? workflows.find(w => w.id === evt.target)?.organization_id
      : (templates.find(t => templateUjt(t) === evt.target) as any)?.organization_id;
    const body: any = { name: evt.name, event_type: evt.event_type, organization_id: org || 1, enabled: true };
    if (evt.source) body.source_ujt_id = evt.source;
    if (evt.targetType === 'workflow') body.workflow_template_id = evt.target;
    else body.unified_job_template_id = evt.target;
    try {
      if (editingEvtId) {
        // Preserve the current enabled state on edit.
        body.enabled = eventTriggers.find(e => e.id === editingEvtId)?.enabled ?? true;
        await api.updateEventTrigger(editingEvtId, body);
      } else {
        await api.createEventTrigger(body);
      }
      setShowEvent(false); setEditingEvtId(null);
      setEvt({ name: '', event_type: 'job_finished', source: 0, targetType: 'workflow', target: 0 });
      fetchData();
    } catch (err) { console.error(err); toast.error(`Failed to ${editingEvtId ? 'update' : 'create'} event trigger`); }
  };
  const toggleEventTrigger = async (t: EventTrigger) => {
    const body: any = { name: t.name, event_type: t.event_type, organization_id: t.organization_id, enabled: !t.enabled };
    if (t.source_ujt_id) body.source_ujt_id = t.source_ujt_id;
    if (t.workflow_template_id) body.workflow_template_id = t.workflow_template_id;
    else body.unified_job_template_id = t.unified_job_template_id;
    try {
      await api.updateEventTrigger(t.id, body);
      setEventTriggers(list => list.map(x => x.id === t.id ? { ...x, enabled: !x.enabled } : x));
    } catch (err) { console.error(err); }
  };
  const deleteEventTrigger = async (id: number) => {
    if (!(await confirmDialog('Delete this event trigger?'))) return;
    try { await api.deleteEventTrigger(id); fetchData(); } catch (err) { console.error(err); }
  };
  const eventTriggerTarget = (t: EventTrigger) =>
    t.workflow_template_id ? `Workflow: ${workflowName(t.workflow_template_id)}` : `Template: ${templateNameByUjt(t.unified_job_template_id)}`;

  if (loading) {
    return <PageSpinner />;
  }

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Schedules &amp; Triggers</h1>
        <p className="text-sm text-gray-500 mt-1">Launch a workflow or job template on a time schedule, when a job finishes, or from an inbound webhook.</p>
      </div>

      {/* Time schedules */}
      <section>
        <div className="flex justify-between items-center mb-3">
          <h2 className="text-lg font-semibold text-gray-800 flex items-center gap-2"><Calendar size={18} className="text-purple-600" /> Schedules <span className="text-sm font-normal text-gray-400">(time)</span></h2>
          <Button icon={<Plus size={16} />} onClick={() => setShowSchedule(true)}>New Schedule</Button>
        </div>
        <Card>
          <div className="divide-y divide-gray-100">
            {schedules.map(s => (
              <div key={s.id} className="p-4 flex items-center justify-between hover:bg-gray-50">
                <div>
                  <h3 className="text-base font-medium text-gray-900">{s.name}</h3>
                  <div className="flex items-center gap-2 mt-1 text-sm text-gray-500">
                    <span>{scheduleTarget(s)}</span>
                    <span className="text-gray-300">•</span>
                    <span>Next: {s.next_run ? new Date(s.next_run).toLocaleString() : '—'}</span>
                  </div>
                  <code className="text-xs bg-gray-100 px-2 py-1 rounded mt-2 inline-block text-gray-600">{s.rrule}</code>
                </div>
                <div className="flex items-center gap-3">
                  <Badge variant={s.enabled ? 'success' : 'neutral'}>{s.enabled ? 'Active' : 'Disabled'}</Badge>
                  <button onClick={() => toggleSchedule(s.id)} className={`p-2 rounded-full ${s.enabled ? 'text-brand-600 hover:bg-brand-50' : 'text-gray-400 hover:bg-gray-100'}`} title="Toggle"><Power size={18} /></button>
                  <button onClick={() => deleteSchedule(s.id)} className="p-2 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50" title="Delete"><Trash2 size={18} /></button>
                </div>
              </div>
            ))}
            {schedules.length === 0 && <p className="p-6 text-gray-500 text-center text-sm">No schedules yet.</p>}
          </div>
        </Card>
      </section>

      {/* Event triggers */}
      <section>
        <div className="flex justify-between items-center mb-3">
          <h2 className="text-lg font-semibold text-gray-800 flex items-center gap-2"><Zap size={18} className="text-amber-500" /> Event triggers <span className="text-sm font-normal text-gray-400">(on job outcome)</span></h2>
          <Button icon={<Plus size={16} />} onClick={openEventCreate}>New Event Trigger</Button>
        </div>
        <Card>
          <div className="divide-y divide-gray-100">
            {eventTriggers.map(t => (
              <div key={t.id} className="p-4 flex items-center justify-between hover:bg-gray-50">
                <div>
                  <h3 className="text-base font-medium text-gray-900">{t.name}</h3>
                  <div className="text-sm text-gray-500 mt-1">
                    When <span className="font-medium text-gray-700">{EVENT_LABEL[t.event_type] || t.event_type}</span>
                    {t.source_ujt_id ? <> for <span className="font-medium text-gray-700">{templateNameByUjt(t.source_ujt_id)}</span></> : <> (any template)</>}
                    {' '}→ launch <span className="font-medium text-gray-700">{eventTriggerTarget(t)}</span>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Badge variant={t.enabled ? 'success' : 'neutral'}>{t.enabled ? 'Active' : 'Disabled'}</Badge>
                  <button onClick={() => toggleEventTrigger(t)} className={`p-2 rounded-full ${t.enabled ? 'text-brand-600 hover:bg-brand-50' : 'text-gray-400 hover:bg-gray-100'}`} title="Enable/disable"><Power size={18} /></button>
                  <button onClick={() => openEventEdit(t)} className="p-2 rounded-full text-gray-400 hover:text-brand-600 hover:bg-brand-50" title="Edit"><Pencil size={18} /></button>
                  <button onClick={() => deleteEventTrigger(t.id)} className="p-2 rounded-full text-gray-400 hover:text-red-600 hover:bg-red-50" title="Delete"><Trash2 size={18} /></button>
                </div>
              </div>
            ))}
            {eventTriggers.length === 0 && <p className="p-6 text-gray-500 text-center text-sm">No event triggers yet. Chain automation on job outcomes.</p>}
          </div>
        </Card>
      </section>

      {/* Webhook triggers (read-only surface) */}
      <section>
        <h2 className="text-lg font-semibold text-gray-800 flex items-center gap-2 mb-3"><Webhook size={18} className="text-cyan-600" /> Webhook triggers <span className="text-sm font-normal text-gray-400">(inbound)</span></h2>
        <Card>
          <div className="divide-y divide-gray-100">
            {webhookTriggers.map((t, i) => (
              <div key={i} className="p-4 flex items-center justify-between hover:bg-gray-50">
                <div className="min-w-0">
                  <h3 className="text-base font-medium text-gray-900">{t.name} <span className="text-xs text-gray-400">({t.kind === 'workflow' ? 'workflow' : t.kind === 'execution_pack' ? 'execution pack (build on push)' : 'template'} · {t.service})</span></h3>
                  <code className="text-xs bg-gray-100 px-2 py-1 rounded mt-1 inline-block text-gray-600 truncate max-w-full">POST {t.url}</code>
                </div>
                <button onClick={() => navigator.clipboard?.writeText(`${window.location.origin}${t.url}`)} className="p-2 rounded-full text-gray-400 hover:text-brand-600 hover:bg-brand-50 shrink-0" title="Copy URL"><Copy size={18} /></button>
              </div>
            ))}
            {webhookTriggers.length === 0 && <p className="p-6 text-gray-500 text-center text-sm">No webhook triggers. Enable one on a workflow or job template to have a remote event launch it.</p>}
          </div>
        </Card>
      </section>

      {/* Create schedule modal */}
      <Modal isOpen={showSchedule} onClose={() => setShowSchedule(false)} title="New Schedule">
        <div className="space-y-4">
          <Input label="Name" value={sched.name} onChange={e => setSched({ ...sched, name: e.target.value })} placeholder="Nightly deploy" />
          <div className="grid grid-cols-2 gap-3">
            <Select label="Launch" value={sched.targetType} onChange={e => setSched({ ...sched, targetType: e.target.value as TargetType, target: 0 })}>
              <option value="job">Job template</option>
              <option value="workflow">Workflow</option>
            </Select>
            <Select label={sched.targetType === 'workflow' ? 'Workflow' : 'Template'} value={sched.target} onChange={e => setSched({ ...sched, target: Number(e.target.value) })}>
              <option value={0}>Select…</option>
              {sched.targetType === 'workflow'
                ? workflows.map(w => <option key={w.id} value={w.id}>{w.name}</option>)
                : templates.map(t => <option key={t.id} value={templateUjt(t)}>{t.name}</option>)}
            </Select>
          </div>
          <Input label="RRule" className="font-mono text-sm" value={sched.rrule} onChange={e => setSched({ ...sched, rrule: e.target.value })} placeholder="FREQ=DAILY;INTERVAL=1" />
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowSchedule(false)}>Cancel</Button>
            <Button onClick={createSchedule}>Create</Button>
          </div>
        </div>
      </Modal>

      {/* Create event trigger modal */}
      <Modal isOpen={showEvent} onClose={() => setShowEvent(false)} title={editingEvtId ? 'Edit Event Trigger' : 'New Event Trigger'}>
        <div className="space-y-4">
          <Input label="Name" value={evt.name} onChange={e => setEvt({ ...evt, name: e.target.value })} placeholder="On deploy failure, run rollback" />
          <div className="grid grid-cols-2 gap-3">
            <Select label="When" value={evt.event_type} onChange={e => setEvt({ ...evt, event_type: e.target.value })}>
              <option value="job_finished">A job finishes</option>
              <option value="job_succeeded">A job succeeds</option>
              <option value="job_failed">A job fails</option>
            </Select>
            <Select label="For template (optional)" value={evt.source} onChange={e => setEvt({ ...evt, source: Number(e.target.value) })}>
              <option value={0}>Any template</option>
              {templates.map(t => <option key={t.id} value={templateUjt(t)}>{t.name}</option>)}
            </Select>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Select label="Then launch" value={evt.targetType} onChange={e => setEvt({ ...evt, targetType: e.target.value as TargetType, target: 0 })}>
              <option value="workflow">Workflow</option>
              <option value="job">Job template</option>
            </Select>
            <Select label={evt.targetType === 'workflow' ? 'Workflow' : 'Template'} value={evt.target} onChange={e => setEvt({ ...evt, target: Number(e.target.value) })}>
              <option value={0}>Select…</option>
              {evt.targetType === 'workflow'
                ? workflows.map(w => <option key={w.id} value={w.id}>{w.name}</option>)
                : templates.map(t => <option key={t.id} value={templateUjt(t)}>{t.name}</option>)}
            </Select>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowEvent(false)}>Cancel</Button>
            <Button onClick={saveEventTrigger}>{editingEvtId ? 'Save changes' : 'Create'}</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default SchedulesPage;
