import React, { useEffect, useState } from 'react';
import { api } from '../services/api';
import Button from './ui/Button';
import Badge from './ui/Badge';
import { Plus, Trash2, Users as UsersIcon, User as UserIcon } from 'lucide-react';

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

  const grants = roles.flatMap(r => [
    ...r.users.map(u => ({ kind: 'user' as const, id: u.id, name: u.username, roleId: r.role_id, roleField: r.role_field })),
    ...r.teams.map(t => ({ kind: 'team' as const, id: t.id, name: t.name, roleId: r.role_id, roleField: r.role_field })),
  ]);

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
            <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Role</th>
            {canManage && <th className="px-4 py-2"></th>}
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100">
          {grants.map((g, i) => (
            <tr key={`${g.kind}-${g.id}-${g.roleId}-${i}`} className="hover:bg-gray-50">
              <td className="px-4 py-2 text-sm font-medium text-gray-900 flex items-center gap-2">
                {g.kind === 'user' ? <UserIcon size={14} className="text-gray-400" /> : <UsersIcon size={14} className="text-blue-500" />}
                {g.name}
              </td>
              <td className="px-4 py-2 text-sm text-gray-500 capitalize">{g.kind}</td>
              <td className="px-4 py-2 text-sm"><Badge variant={g.roleField === 'admin_role' ? 'warning' : g.roleField === 'member_role' ? 'info' : 'neutral'}>{roleLabel(g.roleField)}</Badge></td>
              {canManage && (
                <td className="px-4 py-2 text-right">
                  <Button variant="ghost" size="sm" icon={<Trash2 size={14} />} onClick={() => revoke(g)} />
                </td>
              )}
            </tr>
          ))}
          {grants.length === 0 && !loading && (
            <tr><td colSpan={canManage ? 4 : 3} className="px-4 py-6 text-center text-sm text-gray-400">No one has explicit access yet.</td></tr>
          )}
        </tbody>
      </table>
    </div>
  );
};

export default ResourceAccess;
