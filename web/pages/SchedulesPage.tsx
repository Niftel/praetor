import React, { useState, useEffect, useMemo, useRef } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Schedule, Template, Workflow, EventTrigger, WebhookTrigger } from '../types';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Input, Select } from '../components/ui/Input';
import {
  Clock, Zap, Webhook, Plus, Trash2, Copy, Pencil, ArrowLeft,
  FolderPlus, Folder, ChevronDown, Check, ArrowRight, GripVertical,
} from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';

type TargetType = 'job' | 'workflow';
type FilterKind = 'all' | 'sched' | 'event' | 'hook';

const EVENT_LABEL: Record<string, string> = { job_succeeded: 'succeeds', job_failed: 'fails', job_finished: 'finishes' };
const ROOT = '__root';

// ── RRULE humanizer ─────────────────────────────────────────────────────────────
const DAY_FULL: Record<string, string> = { MO: 'Monday', TU: 'Tuesday', WE: 'Wednesday', TH: 'Thursday', FR: 'Friday', SA: 'Saturday', SU: 'Sunday' };
const DAY_SHORT: Record<string, string> = { MO: 'Mon', TU: 'Tue', WE: 'Wed', TH: 'Thu', FR: 'Fri', SA: 'Sat', SU: 'Sun' };
const pad = (n: number | string) => String(n).padStart(2, '0');

function humanizeRRule(rrule: string, nextRun?: string | null): { text: string; fallback: boolean } {
  if (!rrule) return { text: '—', fallback: true };
  const parts: Record<string, string> = {};
  for (const seg of rrule.replace(/^RRULE:/i, '').split(';')) { const [k, v] = seg.split('='); if (k) parts[k.toUpperCase()] = v; }
  const freq = parts.FREQ;
  const interval = Number(parts.INTERVAL || 1);
  const byday = parts.BYDAY ? parts.BYDAY.split(',') : [];
  let base = '';
  switch (freq) {
    case 'MINUTELY': base = interval === 1 ? 'every minute' : `every ${interval} minutes`; break;
    case 'HOURLY': base = interval === 1 ? 'every hour' : `every ${interval} hours`; break;
    case 'DAILY': base = interval === 1 ? 'every day' : `every ${interval} days`; break;
    case 'MONTHLY': base = interval === 1 ? 'every month' : `every ${interval} months`; break;
    case 'WEEKLY':
      if (byday.length) {
        const weekdays = ['MO', 'TU', 'WE', 'TH', 'FR'];
        if (byday.length === 5 && weekdays.every(d => byday.includes(d))) base = 'weekdays';
        else if (byday.length === 2 && byday.includes('SA') && byday.includes('SU')) base = 'weekends';
        else if (byday.length === 1) base = `every ${DAY_FULL[byday[0]] || byday[0]}`;
        else base = byday.map(d => DAY_SHORT[d] || d).join(', ');
      } else base = interval === 1 ? 'every week' : `every ${interval} weeks`;
      break;
    default: return { text: rrule, fallback: true };
  }
  let time = '';
  if (parts.BYHOUR != null) time = `${pad(parts.BYHOUR)}:${pad(parts.BYMINUTE || 0)}`;
  else if (nextRun && (freq === 'DAILY' || freq === 'WEEKLY')) {
    try { time = new Date(nextRun).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false }); } catch { /* ignore */ }
  }
  return { text: time ? `${base} at ${time}` : base, fallback: false };
}

const relNext = (iso?: string | null) => {
  if (!iso) return '—';
  const d = new Date(iso); const now = new Date();
  const t = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
  if (d.toDateString() === now.toDateString()) return `today ${t}`;
  const tm = new Date(now); tm.setDate(now.getDate() + 1);
  if (d.toDateString() === tm.toDateString()) return `tomorrow ${t}`;
  return `${d.toLocaleDateString([], { weekday: 'short' })} ${t}`;
};

// ── Client-side folder + order overlay (nested), persisted per org ──────────────
interface Folder { id: string; name: string; parentId?: string | null; }
const foldersKey = (org: number) => `praetor.schedFolders.${org}`;
const assignKey = (org: number) => `praetor.schedAssign.${org}`;
const orderKey = (org: number) => `praetor.schedOrder.${org}`;
const loadLS = <T,>(k: string, fallback: T): T => { try { const v = localStorage.getItem(k); return v ? JSON.parse(v) : fallback; } catch { return fallback; } };
const saveLS = (k: string, v: unknown) => { try { localStorage.setItem(k, JSON.stringify(v)); } catch { /* ignore */ } };

// ── presentational pieces ───────────────────────────────────────────────────────
const Toggle: React.FC<{ on: boolean; onClick: () => void }> = ({ on, onClick }) => (
  <button onClick={onClick} title={on ? 'Disable' : 'Enable'} className={`relative w-[34px] h-5 rounded-full shrink-0 transition-colors ${on ? 'bg-acc' : 'bg-line2'}`}>
    <span className={`absolute top-[2.5px] h-[15px] w-[15px] rounded-full transition-all ${on ? 'left-[16.5px] bg-[#06231e]' : 'left-[2.5px] bg-[#c3c9d4]'}`} />
  </button>
);
const Stat: React.FC<{ kind: 'on' | 'off' | 'inbound' }> = ({ kind }) => {
  const map = { on: ['text-ok', 'bg-ok', 'Active'], off: ['text-dim', 'bg-faint', 'Off'], inbound: ['text-violet', 'bg-violet', 'Inbound'] } as const;
  const [text, dot, label] = map[kind];
  return <span className={`inline-flex items-center gap-2 font-mono text-[10.5px] w-[70px] ${text}`}><span className={`w-[6px] h-[6px] rounded-full ${dot}`} />{label}</span>;
};
const Tag: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <span className="text-[8.5px] uppercase tracking-[0.1em] text-dim border border-line rounded px-1.5 py-px">{children}</span>
);
const Dropline = () => (
  <div className="relative h-0 my-[3px] ml-1.5 rounded-full border-t-2 border-acc">
    <span className="absolute -left-1 -top-1 w-[7px] h-[7px] rounded-full bg-acc" />
  </div>
);

const SchedulesPage = () => {
  const { orgId: orgIdStr } = useParams();
  const orgId = Number(orgIdStr);
  const [orgName, setOrgName] = useState('');
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [eventTriggers, setEventTriggers] = useState<EventTrigger[]>([]);
  const [webhookTriggers, setWebhookTriggers] = useState<WebhookTrigger[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState<FilterKind>('all');

  const [showSchedule, setShowSchedule] = useState(false);
  const [sched, setSched] = useState({ name: '', targetType: 'job' as TargetType, target: 0, rrule: 'FREQ=DAILY;INTERVAL=1' });
  const [showEvent, setShowEvent] = useState(false);
  const [editingEvtId, setEditingEvtId] = useState<number | null>(null);
  const [evt, setEvt] = useState({ name: '', event_type: 'job_finished', source: 0, targetType: 'workflow' as TargetType, target: 0 });

  // Folder + order overlay
  const [folders, setFolders] = useState<Folder[]>([]);
  const [assign, setAssign] = useState<Record<number, string | null>>({});
  const [order, setOrder] = useState<Record<string, number[]>>({});
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [newFolderParent, setNewFolderParent] = useState<string | null | undefined>(undefined);
  const [newFolderName, setNewFolderName] = useState('');

  // Drag state (custom pointer DnD)
  const [drag, setDrag] = useState<{ id: number; name: string; cond: string } | null>(null);
  const [ghost, setGhost] = useState({ x: 0, y: 0 });
  const [dropAt, setDropAt] = useState<{ container: string; index: number } | null>(null);
  const dropAtRef = useRef<{ container: string; index: number } | null>(null);
  const setDrop = (v: { container: string; index: number } | null) => { dropAtRef.current = v; setDropAt(v); };

  useEffect(() => {
    setFolders(loadLS<Folder[]>(foldersKey(orgId), []));
    setAssign(loadLS<Record<number, string | null>>(assignKey(orgId), {}));
    setOrder(loadLS<Record<string, number[]>>(orderKey(orgId), {}));
  }, [orgId]);
  useEffect(() => { if (!loading) saveLS(foldersKey(orgId), folders); }, [folders, orgId, loading]);
  useEffect(() => { if (!loading) saveLS(assignKey(orgId), assign); }, [assign, orgId, loading]);
  useEffect(() => { if (!loading) saveLS(orderKey(orgId), order); }, [order, orgId, loading]);

  const fetchData = async () => {
    try {
      setLoading(true);
      const [s, t, wf, et, wh, orgs] = await Promise.all([
        api.getSchedules().catch(() => []), api.getTemplates().catch(() => ({})), api.getWorkflows().catch(() => []),
        api.getEventTriggers().catch(() => []), api.getWebhookTriggers().catch(() => []), api.getOrganizations().catch(() => []),
      ]);
      setSchedules(unwrap(s)); setTemplates(unwrap(t)); setWorkflows(unwrap(wf));
      setEventTriggers(unwrap(et)); setWebhookTriggers(unwrap(wh));
      setOrgName(unwrap<{ id: number; name: string }>(orgs).find(o => o.id === orgId)?.name ?? `Org ${orgId}`);
    } catch (err) { console.error('Failed to load triggers', err); }
    finally { setLoading(false); }
  };
  useEffect(() => { fetchData(); /* eslint-disable-next-line */ }, [orgId]);

  const templateUjt = (t: Template) => (t as any).unified_job_template_id || t.id;
  const templateNameByUjt = (ujt?: number | null) => templates.find(t => templateUjt(t) === ujt)?.name || (ujt ? `template ${ujt}` : '—');
  const workflowName = (id?: number | null) => workflows.find(w => w.id === id)?.name || (id ? `workflow ${id}` : '—');
  const scheduleOrgId = (s: Schedule): number | undefined => s.workflow_template_id ? workflows.find(w => w.id === s.workflow_template_id)?.organization_id : (templates.find(t => templateUjt(t) === s.unified_job_template_id) as any)?.organization_id;
  const webhookOrgId = (t: WebhookTrigger): number | undefined => t.kind === 'workflow' ? workflows.find(w => w.id === t.id)?.organization_id : t.kind === 'job_template' ? (templates.find(x => x.id === t.id) as any)?.organization_id : undefined;

  const orgTemplates = templates.filter(t => (t as any).organization_id === orgId);
  const orgWorkflows = workflows.filter(w => (w as any).organization_id === orgId);
  const shownSchedules = schedules.filter(s => scheduleOrgId(s) === orgId);
  const shownEventTriggers = eventTriggers.filter(t => t.organization_id === orgId);
  const shownWebhooks = webhookTriggers.filter(t => webhookOrgId(t) === orgId);
  const counts = { all: shownSchedules.length + shownEventTriggers.length + shownWebhooks.length, sched: shownSchedules.length, event: shownEventTriggers.length, hook: shownWebhooks.length };

  const scheduleTargetName = (s: Schedule) => s.workflow_template_id ? workflowName(s.workflow_template_id) : templateNameByUjt(s.unified_job_template_id);
  const scheduleTargetKind = (s: Schedule) => s.workflow_template_id ? 'workflow' : 'template';

  // Group by leaf folder container ('__root' when ungrouped/removed).
  const byContainer = useMemo(() => {
    const valid = new Set(folders.map(f => f.id));
    const m = new Map<string, Schedule[]>();
    const push = (k: string, s: Schedule) => { if (!m.has(k)) m.set(k, []); m.get(k)!.push(s); };
    for (const s of shownSchedules) {
      const fid = assign[s.id];
      push(fid && valid.has(fid) ? fid : ROOT, s);
    }
    return m;
  }, [shownSchedules, folders, assign]);

  const ordered = (container: string): Schedule[] => {
    const items = byContainer.get(container) || [];
    const ord = order[container] || [];
    const rank = (id: number) => { const i = ord.indexOf(id); return i === -1 ? 1e9 + id : i; };
    return [...items].sort((a, b) => rank(a.id) - rank(b.id));
  };
  const childFolders = (parentId: string | null) => folders.filter(f => (f.parentId ?? null) === parentId);
  const subtreeCount = (fid: string): number => (byContainer.get(fid)?.length || 0) + childFolders(fid).reduce((n, c) => n + subtreeCount(c.id), 0);

  // ── drag lifecycle ────────────────────────────────────────────────────────────
  const startDrag = (s: Schedule, e: React.MouseEvent) => {
    e.preventDefault(); e.stopPropagation();
    const h = humanizeRRule(s.rrule, s.next_run);
    setDrag({ id: s.id, name: s.name, cond: `${h.text}  →  ${scheduleTargetName(s)}` });
    setGhost({ x: e.clientX, y: e.clientY });
  };
  useEffect(() => {
    if (!drag) return;
    const move = (e: MouseEvent) => setGhost({ x: e.clientX, y: e.clientY });
    const up = () => {
      const t = dropAtRef.current;
      if (t) {
        const folderId = t.container === ROOT ? null : t.container;
        setAssign(a => ({ ...a, [drag.id]: folderId }));
        setOrder(o => {
          const next: Record<string, number[]> = {};
          for (const k of Object.keys(o)) next[k] = o[k].filter(x => x !== drag.id);
          const arr = (next[t.container] || ordered(t.container).map(s => s.id).filter(x => x !== drag.id)).slice();
          const idx = Math.min(t.index, arr.length);
          arr.splice(idx, 0, drag.id);
          next[t.container] = arr;
          return next;
        });
      }
      setDrag(null); setDrop(null);
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    const prev = document.body.style.userSelect; document.body.style.userSelect = 'none';
    return () => { window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', up); document.body.style.userSelect = prev; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [drag]);

  const onRowHover = (e: React.MouseEvent, container: string, index: number) => {
    if (!drag) return;
    const r = e.currentTarget.getBoundingClientRect();
    const before = e.clientY < r.top + r.height / 2;
    setDrop({ container, index: before ? index : index + 1 });
  };
  const onFolderHover = (fid: string) => { if (drag) setDrop({ container: fid, index: (byContainer.get(fid) || []).length }); };

  // ── mutations ─────────────────────────────────────────────────────────────────
  const toggleSchedule = async (id: number) => {
    const s = schedules.find(x => x.id === id); if (!s) return;
    try { await api.updateSchedule(id, { ...s, enabled: !s.enabled }); setSchedules(schedules.map(x => x.id === id ? { ...x, enabled: !x.enabled } : x)); }
    catch (err) { console.error(err); }
  };
  const createSchedule = async () => {
    if (!sched.name || !sched.target) return;
    const body: any = { name: sched.name, rrule: sched.rrule };
    if (sched.targetType === 'workflow') body.workflow_template_id = sched.target; else body.unified_job_template_id = sched.target;
    try { await api.createSchedule(body); setShowSchedule(false); setSched({ name: '', targetType: 'job', target: 0, rrule: 'FREQ=DAILY;INTERVAL=1' }); fetchData(); }
    catch (err) { console.error(err); toast.error('Failed to create schedule'); }
  };
  const deleteSchedule = async (id: number) => {
    if (!(await confirmDialog('Delete this schedule?', { destructive: true, confirmText: 'Delete' }))) return;
    try { await api.deleteSchedule(id); fetchData(); } catch (err) { console.error(err); }
  };
  const openEventCreate = () => { setEditingEvtId(null); setEvt({ name: '', event_type: 'job_finished', source: 0, targetType: 'workflow', target: 0 }); setShowEvent(true); };
  const openEventEdit = (t: EventTrigger) => {
    setEditingEvtId(t.id);
    setEvt({ name: t.name, event_type: t.event_type, source: t.source_ujt_id || 0, targetType: t.workflow_template_id ? 'workflow' : 'job', target: t.workflow_template_id || t.unified_job_template_id || 0 });
    setShowEvent(true);
  };
  const saveEventTrigger = async () => {
    if (!evt.name || !evt.target) return;
    const body: any = { name: evt.name, event_type: evt.event_type, organization_id: orgId, enabled: true };
    if (evt.source) body.source_ujt_id = evt.source;
    if (evt.targetType === 'workflow') body.workflow_template_id = evt.target; else body.unified_job_template_id = evt.target;
    try {
      if (editingEvtId) { body.enabled = eventTriggers.find(e => e.id === editingEvtId)?.enabled ?? true; await api.updateEventTrigger(editingEvtId, body); }
      else await api.createEventTrigger(body);
      setShowEvent(false); setEditingEvtId(null); setEvt({ name: '', event_type: 'job_finished', source: 0, targetType: 'workflow', target: 0 }); fetchData();
    } catch (err) { console.error(err); toast.error(`Failed to ${editingEvtId ? 'update' : 'create'} event trigger`); }
  };
  const toggleEventTrigger = async (t: EventTrigger) => {
    const body: any = { name: t.name, event_type: t.event_type, organization_id: t.organization_id, enabled: !t.enabled };
    if (t.source_ujt_id) body.source_ujt_id = t.source_ujt_id;
    if (t.workflow_template_id) body.workflow_template_id = t.workflow_template_id; else body.unified_job_template_id = t.unified_job_template_id;
    try { await api.updateEventTrigger(t.id, body); setEventTriggers(list => list.map(x => x.id === t.id ? { ...x, enabled: !x.enabled } : x)); } catch (err) { console.error(err); }
  };
  const deleteEventTrigger = async (id: number) => {
    if (!(await confirmDialog('Delete this event trigger?', { destructive: true, confirmText: 'Delete' }))) return;
    try { await api.deleteEventTrigger(id); fetchData(); } catch (err) { console.error(err); }
  };

  // Folder ops
  const addFolder = () => {
    const n = newFolderName.trim(); if (!n) { setNewFolderParent(undefined); return; }
    setFolders(f => [...f, { id: `f${Date.now()}${f.length}`, name: n, parentId: newFolderParent ?? null }]);
    setNewFolderName(''); setNewFolderParent(undefined);
  };
  const removeFolder = (id: string) => {
    const ids = new Set<string>(); const collect = (fid: string) => { ids.add(fid); childFolders(fid).forEach(c => collect(c.id)); }; collect(id);
    setFolders(f => f.filter(x => !ids.has(x.id)));
    setAssign(a => { const n = { ...a }; for (const k of Object.keys(n)) if (n[+k] && ids.has(n[+k]!)) n[+k] = null; return n; });
  };
  const toggleCollapse = (id: string) => setCollapsed(p => { const n = new Set(p); n.has(id) ? n.delete(id) : n.add(id); return n; });

  if (loading) return <PageSpinner />;

  // ── renderers ─────────────────────────────────────────────────────────────────
  const ScheduleRule = (s: Schedule, container: string, index: number) => {
    const h = humanizeRRule(s.rrule, s.next_run);
    return (
      <div key={s.id} onMouseMove={e => onRowHover(e, container, index)}
        className={`group relative flex items-center gap-4 pl-8 pr-2 py-3 border-b border-line last:border-0 hover:bg-white/[0.02] ${drag?.id === s.id ? 'opacity-30' : ''}`}>
        <button onMouseDown={e => startDrag(s, e)} title="Drag to reorder or move to a folder"
          className="absolute left-1.5 top-1/2 -translate-y-1/2 text-faint group-hover:text-dim cursor-grab active:cursor-grabbing p-0.5"><GripVertical size={14} /></button>
        <div className="min-w-0 flex-1">
          <div className="text-[13.5px] font-medium text-ink truncate">{s.name}</div>
          <div className="mt-1.5 flex items-center gap-2.5 flex-wrap font-mono text-[12px] text-mut">
            <span className={h.fallback ? 'text-dim' : ''}>{h.text}</span>
            <ArrowRight size={13} className="text-faint" />
            <span className="inline-flex items-center gap-1.5 text-ink font-medium">{scheduleTargetName(s)}<Tag>{scheduleTargetKind(s)}</Tag></span>
          </div>
        </div>
        <div className="flex items-center gap-3.5 shrink-0">
          <div className="font-mono text-[11px] text-dim text-right whitespace-nowrap">next <span className="text-mut">{relNext(s.next_run)}</span></div>
          <Stat kind={s.enabled ? 'on' : 'off'} />
          <Toggle on={!!s.enabled} onClick={() => toggleSchedule(s.id)} />
          <button onClick={() => deleteSchedule(s.id)} className="w-7 h-7 grid place-items-center rounded-md text-faint opacity-0 group-hover:opacity-100 hover:text-err hover:bg-white/5" title="Delete"><Trash2 size={15} /></button>
        </div>
      </div>
    );
  };

  const renderRules = (container: string) => {
    const list = ordered(container);
    const active = !!drag && dropAt?.container === container;
    return (
      <>
        {list.map((s, i) => (
          <React.Fragment key={s.id}>
            {active && dropAt!.index === i && <Dropline />}
            {ScheduleRule(s, container, i)}
          </React.Fragment>
        ))}
        {active && dropAt!.index >= list.length && <Dropline />}
      </>
    );
  };

  const FolderTree = (parentId: string | null): React.ReactNode => (
    childFolders(parentId).map(f => {
      const items = byContainer.get(f.id) || [];
      const subs = childFolders(f.id);
      const isCol = collapsed.has(f.id);
      const isDrop = drag && dropAt?.container === f.id;
      return (
        <div key={f.id} className="mt-1">
          <div onClick={() => toggleCollapse(f.id)} onMouseMove={() => onFolderHover(f.id)}
            className={`group/f flex items-center gap-2.5 h-9 px-2 rounded-lg cursor-pointer hover:bg-white/[0.025] ${isDrop ? 'bg-acc/10 shadow-[inset_0_0_0_1px_rgba(77,224,200,.42)]' : ''}`}>
            <ChevronDown size={12} className={`text-dim transition-transform ${isCol ? '-rotate-90' : ''}`} />
            <Folder size={15} className={isDrop ? 'text-acc2' : 'text-mut'} />
            <span className={`text-[12.5px] font-semibold ${isDrop ? 'text-acc2' : 'text-ink2'}`}>{f.name}</span>
            <span className="font-mono text-[10.5px] text-faint">{subs.length ? `${subs.length} folder${subs.length === 1 ? '' : 's'} · ` : ''}{subtreeCount(f.id)}</span>
            <div className="ml-auto flex items-center gap-1 opacity-0 group-hover/f:opacity-100">
              <button onClick={e => { e.stopPropagation(); setNewFolderParent(f.id); setNewFolderName(''); }} className="inline-flex items-center gap-1 font-mono text-[10.5px] text-dim hover:text-ink2 px-1.5 py-1 rounded"><Plus size={12} /> folder</button>
              <button onClick={e => { e.stopPropagation(); removeFolder(f.id); }} className="w-6 h-6 grid place-items-center rounded text-faint hover:text-err" title="Remove folder (schedules kept)"><Trash2 size={13} /></button>
            </div>
          </div>
          {!isCol && (
            <div className="ml-[15px] pl-[17px] border-l border-line">
              {FolderTree(f.id)}
              {newFolderParent === f.id && folderInput}
              {items.length || (drag && dropAt?.container === f.id) ? renderRules(f.id)
                : subs.length === 0 && <div className="pl-6 py-2.5 font-mono text-[11px] text-faint">drop schedules here</div>}
            </div>
          )}
        </div>
      );
    })
  );

  const folderInput = (
    <div className="flex items-center gap-2.5 h-9 px-2 my-1 rounded-lg bg-panel2 border border-line">
      <Folder size={15} className="text-grp" />
      <input autoFocus value={newFolderName} onChange={e => setNewFolderName(e.target.value)}
        onKeyDown={e => { if (e.key === 'Enter') addFolder(); if (e.key === 'Escape') setNewFolderParent(undefined); }}
        placeholder="Folder name" className="flex-1 bg-transparent outline-none text-[12.5px] text-ink placeholder:text-dim" />
      <button onClick={addFolder} className="text-acc hover:text-acc2"><Check size={15} /></button>
    </div>
  );

  const GroupHead: React.FC<{ color: string; icon: React.ReactNode; title: string; sub: string; children?: React.ReactNode }> = ({ color, icon, title, sub, children }) => (
    <div className="flex items-center gap-2.5 mt-7 mb-1 px-1">
      <span className={color}>{icon}</span>
      <span className="font-mono text-[10px] tracking-[0.15em] uppercase text-mut">{title}</span>
      <span className="font-mono text-[10px] text-faint">{sub}</span>
      {children && <div className="ml-auto flex items-center gap-2">{children}</div>}
    </div>
  );

  const showSched = filter === 'all' || filter === 'sched';
  const showEvt = filter === 'all' || filter === 'event';
  const showHook = filter === 'all' || filter === 'hook';
  const rootItems = ordered(ROOT);

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink overflow-auto scroll-tint">
      {/* floating drag ghost */}
      {drag && (
        <div style={{ position: 'fixed', left: ghost.x + 18, top: ghost.y - 16, transform: 'rotate(-1.4deg)' }}
          className="z-[60] w-[420px] max-w-[70vw] pointer-events-none rounded-[11px] border border-acc/60 bg-[#12171d] shadow-[0_18px_38px_rgba(0,0,0,.6)] pl-9 pr-4 py-3">
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-acc2"><GripVertical size={14} /></span>
          <div className="text-[13.5px] font-medium text-ink truncate">{drag.name}</div>
          <div className="mt-1 font-mono text-[12px] text-mut truncate">{drag.cond}</div>
        </div>
      )}

      <div className="max-w-[1040px] w-full mx-auto px-8 pt-7 pb-16">
        <div className="mb-6">
          <Link to="/schedules" className="inline-flex items-center gap-1.5 text-[12px] text-mut hover:text-acc"><ArrowLeft size={14} /> Organizations</Link>
          <h1 className="text-[22px] font-semibold tracking-tight mt-1.5">Schedules &amp; Triggers</h1>
          <p className="mt-2 text-[12.5px] text-mut max-w-[520px] leading-relaxed">Launch a template or workflow in <span className="text-ink2">{orgName}</span> — on a clock, when a job finishes, or from an inbound webhook.</p>
        </div>

        {/* filter chips */}
        <div className="flex items-center gap-1.5 mb-1 pb-3.5 border-b border-line">
          {([['all', 'All', ''], ['sched', 'Schedules', 'bg-run'], ['event', 'Events', 'bg-changed'], ['hook', 'Webhooks', 'bg-violet']] as const).map(([k, label, dot]) => (
            <button key={k} onClick={() => setFilter(k)}
              className={`inline-flex items-center gap-2 h-[30px] px-3 rounded-lg text-[12px] border ${filter === k ? 'text-ink bg-panel border-line' : 'text-mut border-transparent hover:text-ink'}`}>
              {dot && <span className={`w-[6px] h-[6px] rounded-full ${dot}`} />}{label}
              <span className={`font-mono text-[10.5px] ${filter === k ? 'text-mut' : 'text-dim'}`}>{counts[k]}</span>
            </button>
          ))}
        </div>

        {/* ── SCHEDULES ── */}
        {showSched && (
          <>
            <GroupHead color="text-run" icon={<Clock size={16} />} title="Time schedules" sub="RRULE · cron">
              <button onClick={() => { setNewFolderParent(null); setNewFolderName(''); }} className="inline-flex items-center gap-1.5 h-7 px-3 rounded-md text-[11.5px] text-dim hover:text-ink2 hover:bg-panel"><FolderPlus size={13} /> New folder</button>
              <button onClick={() => setShowSchedule(true)} className="inline-flex items-center gap-1.5 h-7 px-3 rounded-md text-[11.5px] font-medium text-ink2 border border-line hover:border-line2 hover:bg-panel"><Plus size={13} /> New schedule</button>
            </GroupHead>

            <div className="mt-1">
              {FolderTree(null)}
              {newFolderParent === null && folderInput}
              {(rootItems.length > 0 || (drag && dropAt?.container === ROOT)) && (
                <div onMouseMove={e => { if (drag && rootItems.length === 0) { e.preventDefault(); setDrop({ container: ROOT, index: 0 }); } }}
                  className="mt-1">
                  {folders.length > 0 && <div className="px-1 py-1.5 font-mono text-[9px] tracking-[0.14em] uppercase text-faint">ungrouped</div>}
                  {renderRules(ROOT)}
                </div>
              )}
              {shownSchedules.length === 0 && <p className="py-8 text-center text-[13px] text-dim">No schedules yet.</p>}
            </div>
            {folders.length > 0 && <p className="mt-3 font-mono text-[10px] text-faint">Folders are a local view overlay — drag by the handle to reorder or move between folders. They don't change what runs.</p>}
          </>
        )}

        {/* ── EVENTS ── */}
        {showEvt && (
          <>
            <GroupHead color="text-changed" icon={<Zap size={16} />} title="Event triggers" sub="on job outcome">
              <button onClick={openEventCreate} className="inline-flex items-center gap-1.5 h-7 px-3 rounded-md text-[11.5px] font-medium text-ink2 border border-line hover:border-line2 hover:bg-panel"><Plus size={13} /> New event trigger</button>
            </GroupHead>
            {shownEventTriggers.map(t => (
              <div key={t.id} className="group flex items-center gap-4 py-3.5 px-1 border-b border-line hover:bg-white/[0.02]">
                <span className="w-9 h-9 rounded-[10px] grid place-items-center border border-line bg-panel text-changed shrink-0"><Zap size={17} /></span>
                <div className="min-w-0 flex-1">
                  <div className="text-[13.5px] font-medium text-ink">{t.name}</div>
                  <div className="mt-1.5 flex items-center gap-2.5 flex-wrap font-mono text-[12px] text-mut">
                    <span>when <b className="text-ink2 font-medium">{t.source_ujt_id ? templateNameByUjt(t.source_ujt_id) : 'any job'}</b> <span className={t.event_type === 'job_failed' ? 'text-err' : t.event_type === 'job_succeeded' ? 'text-ok' : ''}>{EVENT_LABEL[t.event_type] || t.event_type}</span></span>
                    <ArrowRight size={13} className="text-faint" />
                    <span className="inline-flex items-center gap-1.5 text-ink font-medium">{t.workflow_template_id ? workflowName(t.workflow_template_id) : templateNameByUjt(t.unified_job_template_id)}<Tag>{t.workflow_template_id ? 'workflow' : 'template'}</Tag></span>
                  </div>
                </div>
                <div className="flex items-center gap-3.5 shrink-0">
                  <Stat kind={t.enabled ? 'on' : 'off'} />
                  <Toggle on={!!t.enabled} onClick={() => toggleEventTrigger(t)} />
                  <button onClick={() => openEventEdit(t)} className="w-7 h-7 grid place-items-center rounded-md text-faint opacity-0 group-hover:opacity-100 hover:text-acc hover:bg-white/5" title="Edit"><Pencil size={14} /></button>
                  <button onClick={() => deleteEventTrigger(t.id)} className="w-7 h-7 grid place-items-center rounded-md text-faint opacity-0 group-hover:opacity-100 hover:text-err hover:bg-white/5" title="Delete"><Trash2 size={15} /></button>
                </div>
              </div>
            ))}
            {shownEventTriggers.length === 0 && <p className="py-8 text-center text-[13px] text-dim">No event triggers. Chain automation on job outcomes.</p>}
          </>
        )}

        {/* ── WEBHOOKS ── */}
        {showHook && (
          <>
            <GroupHead color="text-violet" icon={<Webhook size={16} />} title="Webhook triggers" sub="inbound">
              <span className="font-mono text-[10.5px] text-dim">enabled on the template or workflow</span>
            </GroupHead>
            {shownWebhooks.map((t, i) => (
              <div key={i} className="group flex items-center gap-4 py-3.5 px-1 border-b border-line hover:bg-white/[0.02]">
                <span className="w-9 h-9 rounded-[10px] grid place-items-center border border-line bg-panel text-violet shrink-0"><Webhook size={17} /></span>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 text-[13.5px] font-medium text-ink">{t.name}<Tag>{t.service}</Tag></div>
                  <div className="mt-1.5 flex items-center gap-2.5 font-mono text-[12px] text-mut">
                    <span>on inbound <b className="text-ink2 font-medium">POST</b></span>
                    <ArrowRight size={13} className="text-faint" />
                    <span className="inline-flex items-center gap-1.5 text-ink font-medium">{t.name}<Tag>{t.kind === 'workflow' ? 'workflow' : t.kind === 'execution_pack' ? 'build pack' : 'template'}</Tag></span>
                  </div>
                </div>
                <div className="flex items-center gap-3.5 shrink-0">
                  <div className="font-mono text-[10.5px] text-dim max-w-[270px] truncate">POST {t.url}</div>
                  <Stat kind="inbound" />
                  <button onClick={() => { navigator.clipboard?.writeText(`${window.location.origin}${t.url}`); toast.info('URL copied'); }} className="w-7 h-7 grid place-items-center rounded-md text-faint hover:text-acc hover:bg-white/5" title="Copy URL"><Copy size={16} /></button>
                </div>
              </div>
            ))}
            {shownWebhooks.length === 0 && <p className="py-8 text-center text-[13px] text-dim">No webhook triggers. Enable one on a workflow or job template to have a remote event launch it.</p>}
          </>
        )}
      </div>

      {/* Schedule modal */}
      <Modal isOpen={showSchedule} onClose={() => setShowSchedule(false)} title="New schedule">
        <div className="space-y-4">
          <Input label="Name" value={sched.name} onChange={e => setSched({ ...sched, name: e.target.value })} placeholder="Nightly deploy" />
          <div className="grid grid-cols-2 gap-3">
            <Select label="Launch" value={sched.targetType} onChange={e => setSched({ ...sched, targetType: e.target.value as TargetType, target: 0 })}>
              <option value="job">Job template</option><option value="workflow">Workflow</option>
            </Select>
            <Select label={sched.targetType === 'workflow' ? 'Workflow' : 'Template'} value={sched.target} onChange={e => setSched({ ...sched, target: Number(e.target.value) })}>
              <option value={0}>Select…</option>
              {sched.targetType === 'workflow' ? orgWorkflows.map(w => <option key={w.id} value={w.id}>{w.name}</option>) : orgTemplates.map(t => <option key={t.id} value={templateUjt(t)}>{t.name}</option>)}
            </Select>
          </div>
          <Input label="RRule" className="font-mono text-sm" value={sched.rrule} onChange={e => setSched({ ...sched, rrule: e.target.value })} placeholder="FREQ=DAILY;INTERVAL=1" />
          {(() => { const h = humanizeRRule(sched.rrule); return !h.fallback && <p className="text-[12px] text-mut -mt-1">Runs <span className="text-ink2">{h.text}</span>.</p>; })()}
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setShowSchedule(false)}>Cancel</Button><Button onClick={createSchedule}>Create</Button></div>
        </div>
      </Modal>

      {/* Event trigger modal */}
      <Modal isOpen={showEvent} onClose={() => setShowEvent(false)} title={editingEvtId ? 'Edit event trigger' : 'New event trigger'}>
        <div className="space-y-4">
          <Input label="Name" value={evt.name} onChange={e => setEvt({ ...evt, name: e.target.value })} placeholder="On deploy failure, run rollback" />
          <div className="grid grid-cols-2 gap-3">
            <Select label="When" value={evt.event_type} onChange={e => setEvt({ ...evt, event_type: e.target.value })}>
              <option value="job_finished">A job finishes</option><option value="job_succeeded">A job succeeds</option><option value="job_failed">A job fails</option>
            </Select>
            <Select label="For template (optional)" value={evt.source} onChange={e => setEvt({ ...evt, source: Number(e.target.value) })}>
              <option value={0}>Any template</option>{orgTemplates.map(t => <option key={t.id} value={templateUjt(t)}>{t.name}</option>)}
            </Select>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Select label="Then launch" value={evt.targetType} onChange={e => setEvt({ ...evt, targetType: e.target.value as TargetType, target: 0 })}>
              <option value="workflow">Workflow</option><option value="job">Job template</option>
            </Select>
            <Select label={evt.targetType === 'workflow' ? 'Workflow' : 'Template'} value={evt.target} onChange={e => setEvt({ ...evt, target: Number(e.target.value) })}>
              <option value={0}>Select…</option>
              {evt.targetType === 'workflow' ? orgWorkflows.map(w => <option key={w.id} value={w.id}>{w.name}</option>) : orgTemplates.map(t => <option key={t.id} value={templateUjt(t)}>{t.name}</option>)}
            </Select>
          </div>
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setShowEvent(false)}>Cancel</Button><Button onClick={saveEventTrigger}>{editingEvtId ? 'Save changes' : 'Create'}</Button></div>
        </div>
      </Modal>
    </div>
  );
};

export default SchedulesPage;
