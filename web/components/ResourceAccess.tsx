import React, { useEffect, useState } from 'react';
import { api } from '../services/api';
import Button from './ui/Button';
import { Plus, X, Users as UsersIcon, User as UserIcon } from 'lucide-react';

interface AccessRole {
  object_role_id: number;
  role_definition_id: number;
  role: string;                // RoleDefinition name, already human-readable
  managed: boolean;
  users: { id: number; username: string; first_name?: string; last_name?: string }[];
  teams: { id: number; name: string }[];
}

interface RoleDefinition {
  id: number;
  name: string;
  description?: string;
  managed: boolean;
}

// RoleDefinition names are already display-ready ("Inventory Admin"); kept for callers
// that still want a formatter.
export const roleLabel = (name: string) => name;

interface Props {
  contentType: string;   // 'organization' | 'inventory' | 'project' | 'job_template' | 'credential' | 'team'
  objectId: number;
  canManage?: boolean;
}

const ResourceAccess: React.FC<Props> = ({ contentType, objectId, canManage = true }) => {
  const [roles, setRoles] = useState<AccessRole[]>([]);
  const [assignable, setAssignable] = useState<RoleDefinition[]>([]);
  const [loading, setLoading] = useState(true);
  const [users, setUsers] = useState<any[]>([]);
  const [teams, setTeams] = useState<any[]>([]);
  const [adding, setAdding] = useState(false);
  const [principalType, setPrincipalType] = useState<'user' | 'team'>('user');
  const [principalId, setPrincipalId] = useState<number | ''>('');
  const [defId, setDefId] = useState<number | ''>('');
  const [error, setError] = useState('');

  const load = () => {
    setLoading(true);
    api.getResourceAccess(contentType, objectId)
      .then(d => setRoles(d || []))
      .catch(() => setRoles([]))
      .finally(() => setLoading(false));
  };
  useEffect(() => { load(); /* eslint-disable-next-line */ }, [contentType, objectId]);
  useEffect(() => {
    api.getUsers().then(r => setUsers(r?.items || r || [])).catch(() => { });
    api.getTeams().then(r => setTeams(r?.items || r || [])).catch(() => { });
    api.getAssignableRoles(contentType).then(d => setAssignable(d || [])).catch(() => setAssignable([]));
  }, [contentType]);

  // One row per principal (user/team), collecting the RoleDefinitions they hold here.
  const principals = (() => {
    const m = new Map<string, { kind: 'user' | 'team'; id: number; name: string; roles: { defId: number; role: string }[] }>();
    for (const r of roles) {
      for (const u of r.users) {
        const k = `user-${u.id}`;
        if (!m.has(k)) m.set(k, { kind: 'user', id: u.id, name: u.username, roles: [] });
        m.get(k)!.roles.push({ defId: r.role_definition_id, role: r.role });
      }
      for (const t of r.teams) {
        const k = `team-${t.id}`;
        if (!m.has(k)) m.set(k, { kind: 'team', id: t.id, name: t.name, roles: [] });
        m.get(k)!.roles.push({ defId: r.role_definition_id, role: r.role });
      }
    }
    return [...m.values()];
  })();

  const grant = async () => {
    setError('');
    if (principalId === '' || defId === '') { setError('Pick a user/team and a role.'); return; }
    const body: any = { content_type: contentType, object_id: objectId, role_definition_id: Number(defId) };
    if (principalType === 'user') body.user_id = Number(principalId); else body.team_id = Number(principalId);
    try {
      await api.grantAccess(body);
      setAdding(false); setPrincipalId(''); setDefId(''); load();
    } catch (e: any) { setError(e.message || 'Failed to grant access. You need admin on this resource.'); }
  };
  const revoke = async (g: { kind: 'user' | 'team'; id: number; defId: number }) => {
    const body: any = { content_type: contentType, object_id: objectId, role_definition_id: g.defId };
    if (g.kind === 'user') body.user_id = g.id; else body.team_id = g.id;
    try { await api.revokeAccess(body); load(); } catch { /* ignore */ }
  };

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-mut">Users and teams with a role on this {contentType.replace('_', ' ')}.</p>
        {canManage && (
          <Button size="sm" icon={<Plus size={14} />} onClick={() => { setAdding(a => !a); setError(''); }}>Add access</Button>
        )}
      </div>

      {adding && (
        <div className="bg-panel2 border border-line rounded-md p-3 space-y-2">
          <div className="flex flex-wrap gap-2 items-center">
            <select value={principalType} onChange={e => { setPrincipalType(e.target.value as any); setPrincipalId(''); }} className="border border-line2 rounded px-2 py-1.5 text-sm">
              <option value="user">User</option>
              <option value="team">Team</option>
            </select>
            <select value={principalId} onChange={e => setPrincipalId(e.target.value === '' ? '' : Number(e.target.value))} className="border border-line2 rounded px-2 py-1.5 text-sm flex-1 min-w-[140px]">
              <option value="">{principalType === 'user' ? 'Select user…' : 'Select team…'}</option>
              {(principalType === 'user' ? users : teams).map(p => (
                <option key={p.id} value={p.id}>{principalType === 'user' ? p.username : p.name}</option>
              ))}
            </select>
            <select value={defId} onChange={e => setDefId(e.target.value === '' ? '' : Number(e.target.value))} className="border border-line2 rounded px-2 py-1.5 text-sm">
              <option value="">Select role…</option>
              {assignable.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
            </select>
            <Button size="sm" onClick={grant}>Grant</Button>
            <Button size="sm" variant="secondary" onClick={() => setAdding(false)}>Cancel</Button>
          </div>
          {error && <p className="text-sm text-err">{error}</p>}
        </div>
      )}

      <table className="min-w-full divide-y divide-line">
        <thead className="bg-panel2">
          <tr>
            <th className="px-4 py-2 text-left text-xs font-medium text-mut uppercase">Name</th>
            <th className="px-4 py-2 text-left text-xs font-medium text-mut uppercase">Type</th>
            <th className="px-4 py-2 text-left text-xs font-medium text-mut uppercase">Roles</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {principals.map(p => (
            <tr key={`${p.kind}-${p.id}`} className="hover:bg-white/[0.03]">
              <td className="px-4 py-2.5 text-sm font-medium text-ink">
                <span className="flex items-center gap-2">
                  {p.kind === 'user' ? <UserIcon size={14} className="text-dim" /> : <UsersIcon size={14} className="text-run" />}
                  {p.name}
                </span>
              </td>
              <td className="px-4 py-2.5 text-sm text-mut capitalize align-top">{p.kind}</td>
              <td className="px-4 py-2.5">
                <div className="flex flex-wrap gap-1.5">
                  {p.roles.map(role => (
                    <span key={role.defId}
                      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium ${/Admin$/.test(role.role) ? 'bg-changed/15 text-changed' : /Member$/.test(role.role) ? 'bg-run/15 text-run' : 'bg-white/5 text-ink2'}`}>
                      {role.role}
                      {canManage && (
                        <button onClick={() => revoke({ kind: p.kind, id: p.id, defId: role.defId })} className="ml-0.5 -mr-0.5 hover:text-err" title="Remove role">
                          <X size={11} />
                        </button>
                      )}
                    </span>
                  ))}
                </div>
              </td>
            </tr>
          ))}
          {principals.length === 0 && !loading && (
            <tr><td colSpan={3} className="px-4 py-6 text-center text-sm text-dim">No one has explicit access yet.</td></tr>
          )}
        </tbody>
      </table>
    </div>
  );
};

export default ResourceAccess;
