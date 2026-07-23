import React, { useEffect, useMemo, useState } from 'react';
import { Bell, Plus, Trash2 } from 'lucide-react';
import { Link } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Team } from '../types';
import Button from './ui/Button';
import { toast } from './ui/toast';

export type NotificationPolicyEvent = {
  id: string;
  label: string;
  description: string;
  requiresTeam?: boolean;
};

type NotificationTarget = {
  id: number;
  name: string;
  notification_type: string;
};

type NotificationPolicy = {
  id: number;
  team_id?: number;
  team_name?: string;
  notification_template_id: number;
  notification_name: string;
  notification_type: string;
  event: string;
};

type Props = {
  organizationId: number;
  resourceType: 'job_template' | 'workflow_template' | 'inventory_source';
  resourceId: number;
  events: NotificationPolicyEvent[];
  canManage: boolean;
  compact?: boolean;
};

const control = 'h-8 min-w-0 rounded-md border border-line2 bg-panel px-2.5 font-mono text-[11.5px] text-ink2 outline-none focus:border-acc/60';

/** One policy editor shared by job templates, workflows, and inventory sources. */
export default function NotificationPolicyManager({ organizationId, resourceType, resourceId, events, canManage, compact = false }: Props) {
  const [targets, setTargets] = useState<NotificationTarget[]>([]);
  const [teams, setTeams] = useState<Team[]>([]);
  const [policies, setPolicies] = useState<NotificationPolicy[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [targetId, setTargetId] = useState('');
  const [eventId, setEventId] = useState(events[0]?.id ?? '');
  const [teamId, setTeamId] = useState('');

  const selectedEvent = events.find(event => event.id === eventId);
  const eventById = useMemo(() => new Map(events.map(event => [event.id, event])), [events]);

  const reload = async () => {
    const [targetRows, policyRows, teamRows] = await Promise.all([
      api.getNotificationTemplates(organizationId),
      api.getNotificationPolicies(resourceType, resourceId),
      events.some(event => event.requiresTeam) ? api.getOrganizationTeams(organizationId) : Promise.resolve([]),
    ]);
    const nextTargets = unwrap<NotificationTarget>(targetRows);
    setTargets(nextTargets);
    setPolicies(unwrap<NotificationPolicy>(policyRows));
    setTeams(unwrap<Team>(teamRows));
    setTargetId(current => current || (nextTargets[0] ? String(nextTargets[0].id) : ''));
  };

  useEffect(() => {
    let active = true;
    setLoading(true);
    reload().catch((error: Error) => {
      if (active) toast.error(error.message || 'Notification routes could not be loaded.');
    }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  // The event definitions are static constants at call sites.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [organizationId, resourceType, resourceId]);

  useEffect(() => {
    if (!selectedEvent?.requiresTeam) setTeamId('');
  }, [selectedEvent]);

  const attach = async () => {
    if (!targetId || !eventId || (selectedEvent?.requiresTeam && !teamId)) return;
    setSaving(true);
    try {
      await api.createNotificationPolicy({
        notification_template_id: Number(targetId), resource_type: resourceType,
        resource_id: resourceId, event: eventId,
        ...(selectedEvent?.requiresTeam ? { team_id: Number(teamId) } : {}),
      });
      await reload();
      toast.success('Notification route attached.');
    } catch (error: any) {
      toast.error(error.message || 'Notification route could not be attached.');
    } finally { setSaving(false); }
  };

  const detach = async (policy: NotificationPolicy) => {
    setSaving(true);
    try {
      await api.deleteNotificationPolicy(policy.id);
      setPolicies(current => current.filter(item => item.id !== policy.id));
      toast.success('Notification route detached.');
    } catch (error: any) {
      toast.error(error.message || 'Notification route could not be detached.');
    } finally { setSaving(false); }
  };

  return (
    <section aria-label="Notification routing" className={compact ? '' : 'mt-2'}>
      {!compact && (
        <div className="mb-3 flex items-start gap-2.5">
          <Bell size={15} className="mt-0.5 shrink-0 text-acc2" aria-hidden="true" />
          <div>
            <h3 className="text-[12.5px] font-medium text-ink2">Event routing</h3>
            <p className="mt-0.5 max-w-[66ch] text-[11px] leading-relaxed text-dim">Attach organization targets to automation events. Approval routes are isolated to the selected team.</p>
          </div>
        </div>
      )}

      {loading ? (
        <p role="status" className="py-3 font-mono text-[11px] text-dim">Loading notification routes…</p>
      ) : policies.length === 0 ? (
        <p className="py-2 font-mono text-[11px] text-dim">No notification routes attached.</p>
      ) : (
        <div className="divide-y divide-line border-y border-line">
          {policies.map(policy => (
            <div key={policy.id} className="flex min-h-10 items-center gap-3 py-2 text-[12px]">
              <span className="min-w-0 flex-1 truncate text-ink2">{policy.notification_name}</span>
              <span className="font-mono text-[10.5px] text-dim">{eventById.get(policy.event)?.label ?? policy.event}</span>
              {policy.team_name && <span className="rounded-md bg-white/[0.045] px-2 py-1 font-mono text-[10px] text-mut">{policy.team_name}</span>}
              {canManage && <button type="button" disabled={saving} onClick={() => detach(policy)} className="text-faint hover:text-err disabled:opacity-50" aria-label={`Detach ${policy.notification_name} from ${policy.event}`}><Trash2 size={13} /></button>}
            </div>
          ))}
        </div>
      )}

      {canManage && (targets.length === 0 ? (
        <p className="mt-3 text-[11px] text-mut">Create and test a target in <Link className="text-acc2 hover:text-acc hover:underline" to="/settings/notifications">Settings → Notifications</Link> before attaching a route.</p>
      ) : (
        <div className="mt-3 flex flex-wrap items-end gap-2">
          <label className="min-w-[150px] flex-1 text-[10px] font-medium text-mut">Target
            <select value={targetId} onChange={event => setTargetId(event.target.value)} className={`${control} mt-1 w-full`}>
              {targets.map(target => <option key={target.id} value={target.id}>{target.name} · {target.notification_type}</option>)}
            </select>
          </label>
          <label className="min-w-[130px] text-[10px] font-medium text-mut">Event
            <select value={eventId} onChange={event => setEventId(event.target.value)} className={`${control} mt-1 w-full`}>
              {events.map(event => <option key={event.id} value={event.id}>{event.label}</option>)}
            </select>
          </label>
          {selectedEvent?.requiresTeam && (
            <label className="min-w-[140px] text-[10px] font-medium text-mut">Approval team
              <select value={teamId} onChange={event => setTeamId(event.target.value)} className={`${control} mt-1 w-full`} required>
                <option value="">Choose team…</option>
                {teams.map(team => <option key={team.id} value={team.id}>{team.name}</option>)}
              </select>
            </label>
          )}
          <Button type="button" size="sm" variant="secondary" icon={<Plus size={13} />} disabled={saving || !targetId || !eventId || (!!selectedEvent?.requiresTeam && !teamId)} onClick={attach}>Attach</Button>
        </div>
      ))}
      {selectedEvent && canManage && <p className="mt-2 font-mono text-[10px] text-faint">{selectedEvent.description}</p>}
    </section>
  );
}
