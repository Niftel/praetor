import React, { useEffect, useState } from 'react';
import { api } from '../services/api';
import Button from './ui/Button';
import { Plus, X, Users as UsersIcon, User as UserIcon } from 'lucide-react';

interface AccessRole {
  role_id: number;
  role_field: string;
  users: { id: number; username: string; first_name?: string; last_name?: string }[];
  teams: { id: number; name: string }[];
}

// admin_role -> "Admin", project_admin_role -> "Project Admin", use_role -> "Use".
export const roleLabel = (f: string) =>
  f.replace(/_role$/, '').split('_').map(s => s.charAt(0).toUpperCase() + s.slice(1)).join(' ');

interface Props {
  contentType: string;   // 'organization' | 'inventory' | 'project' | 'job_template' | 'credential' | 'team'
  objectId: number;
  canManage?: boolean;
}

const ResourceAccess: React.FC<Props> = ({ contentType, objectId, canManage = true }) => {
  const [roles, setRoles] = useState<AccessRole[]>([]);
  const [loading, setLoading] = useState(true);
  const [users, setUsers] = useState<any[]>([]);
  const [teams, setTeams] = useState<any[]>([]);
  const [adding, setAdding] = useState(false);
  const [principalType, setPrincipalType] = useState<'user' | 'team'>('user');
  const [principalId, setPrincipalId] = useState<number | ''>('');
  const [roleId, setRoleId] = useState<number | ''>('');
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
  }, []);

  // One row per principal (user/team), collecting all the roles they hold here.
  const principals = (() => {
    const m = new Map<string, { kind: 'user' | 'team'; id: number; name: string; roles: { roleId: number; roleField: string }[] }>();
    for (const r of roles) {
      for (const u of r.users) {
        const k = `user-${u.id}`;
        if (!m.has(k)) m.set(k, { kind: 'user', id: u.id, name: u.username, roles: [] });
        m.get(k)!.roles.push({ roleId: r.role_id, roleField: r.role_field });
      }
      for (const t of r.teams) {
        const k = `team-${t.id}`;
        if (!m.has(k)) m.set(k, { kind: 'team', id: t.id, name: t.name, roles: [] });
        m.get(k)!.roles.push({ roleId: r.role_id, roleField: r.role_field });
      }
    }
    return [...m.values()];
  })();

  const grant = async () => {
    setError('');
    if (principalId === '' || roleId === '') { setError('Pick a user/team and a role.'); return; }
    try {
      if (principalType === 'user') await api.addRoleUser(Number(roleId), Number(principalId));
      else await api.addRoleTeam(Number(roleId), Number(principalId));
      setAdding(false); setPrincipalId(''); setRoleId(''); load();
    } catch (e: any) { setError(e.message || 'Failed to grant access. You need admin on this resource.'); }
  };
  const revoke = async (g: { kind: 'user' | 'team'; id: number; roleId: number }) => {
    try {
      if (g.kind === 'user') await api.removeRoleUser(g.roleId, g.id);
      else await api.removeRoleTeam(g.roleId, g.id);
      load();
    } catch { /* ignore */ }
  };

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-gray-500">Users and teams with a role on this {contentType.replace('_', ' ')}.</p>
        {canManage && (
          <Button size="sm" icon={<Plus size={14} />} onClick={() => { setAdding(a => !a); setError(''); }}>Add access</Button>
        )}
      </div>

      {adding && (
        <div className="bg-gray-50 border border-gray-200 rounded-md p-3 space-y-2">
          <div className="flex flex-wrap gap-2 items-center">
            <select value={principalType} onChange={e => { setPrincipalType(e.target.value as any); setPrincipalId(''); }} className="border border-gray-300 rounded px-2 py-1.5 text-sm">
              <option value="user">User</option>
              <option value="team">Team</option>
            </select>
            <select value={principalId} onChange={e => setPrincipalId(e.target.value === '' ? '' : Number(e.target.value))} className="border border-gray-300 rounded px-2 py-1.5 text-sm flex-1 min-w-[140px]">
              <option value="">{principalType === 'user' ? 'Select user…' : 'Select team…'}</option>
              {(principalType === 'user' ? users : teams).map(p => (
                <option key={p.id} value={p.id}>{principalType === 'user' ? p.username : p.name}</option>
              ))}
            </select>
            <select value={roleId} onChange={e => setRoleId(e.target.value === '' ? '' : Number(e.target.value))} className="border border-gray-300 rounded px-2 py-1.5 text-sm">
              <option value="">Select role…</option>
              {roles.map(r => <option key={r.role_id} value={r.role_id}>{roleLabel(r.role_field)}</option>)}
            </select>
            <Button size="sm" onClick={grant}>Grant</Button>
            <Button size="sm" variant="secondary" onClick={() => setAdding(false)}>Cancel</Button>
          </div>
          {error && <p className="text-sm text-red-600">{error}</p>}
        </div>
      )}

      <table className="min-w-full divide-y divide-gray-200">
        <thead className="bg-gray-50">
          <tr>
            <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
            <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
            <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Roles</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100">
          {principals.map(p => (
            <tr key={`${p.kind}-${p.id}`} className="hover:bg-gray-50">
              <td className="px-4 py-2.5 text-sm font-medium text-gray-900">
                <span className="flex items-center gap-2">
                  {p.kind === 'user' ? <UserIcon size={14} className="text-gray-400" /> : <UsersIcon size={14} className="text-blue-500" />}
                  {p.name}
                </span>
              </td>
              <td className="px-4 py-2.5 text-sm text-gray-500 capitalize align-top">{p.kind}</td>
              <td className="px-4 py-2.5">
                <div className="flex flex-wrap gap-1.5">
                  {p.roles.map(role => (
                    <span key={role.roleId}
                      className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium ${role.roleField === 'admin_role' ? 'bg-amber-100 text-amber-800' : role.roleField === 'member_role' ? 'bg-blue-100 text-blue-800' : 'bg-gray-100 text-gray-700'}`}>
                      {roleLabel(role.roleField)}
                      {canManage && (
                        <button onClick={() => revoke({ kind: p.kind, id: p.id, roleId: role.roleId })} className="ml-0.5 -mr-0.5 hover:text-red-600" title="Remove role">
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
            <tr><td colSpan={3} className="px-4 py-6 text-center text-sm text-gray-400">No one has explicit access yet.</td></tr>
          )}
        </tbody>
      </table>
    </div>
  );
};

export default ResourceAccess;
