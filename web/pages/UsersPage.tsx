import React, { useState, useEffect, useMemo } from 'react';
import { api, unwrap } from '../services/api';
import { User } from '../types';
import Badge from '../components/ui/Badge';
import Modal from '../components/ui/Modal';
import { Shield, Trash2, KeyRound, Building2, UserRound } from 'lucide-react';
import { confirmDialog } from '../components/ui/toast';
import { EmptyState, LoadingState, Page, PageHeader } from '../components/ui';

interface Org { id: number; name: string; }
interface OrgRoster { members: User[]; admins: User[]; }

const UsersPage = () => {
  const [users, setUsers] = useState<User[]>([]);
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [roster, setRoster] = useState<Record<number, OrgRoster>>({});
  const [loading, setLoading] = useState(true);
  const [accessUser, setAccessUser] = useState<User | null>(null);
  const [accessRows, setAccessRows] = useState<any[]>([]);

  const openAccess = (user: User) => {
    setAccessUser(user);
    setAccessRows([]);
    api.getUserAccess(user.id).then(d => setAccessRows(d || [])).catch(() => setAccessRows([]));
  };

  const fetchAll = async () => {
    try {
      setLoading(true);
      const [usersRes, orgsRes] = await Promise.all([api.getUsers(), api.getOrganizations()]);
      const allUsers = unwrap<User>(usersRes);
      const allOrgs = unwrap<Org>(orgsRes);
      setUsers(allUsers);
      setOrgs(allOrgs);
      const entries = await Promise.all(allOrgs.map(async o => {
        const [members, admins] = await Promise.all([
          api.getOrganizationUsers(o.id).catch(() => []),
          api.getOrganizationAdmins(o.id).catch(() => []),
        ]);
        return [o.id, { members: members || [], admins: admins || [] }] as const;
      }));
      setRoster(Object.fromEntries(entries));
    } catch (err) {
      console.error('Failed to load users', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { fetchAll(); }, []);

  // Build one group per org (members ∪ admins, deduped, admins flagged), plus an
  // "unassigned" group for users who belong to no organization.
  const groups = useMemo(() => {
    const assigned = new Set<number>();
    const orgGroups = orgs.map(o => {
      const r = roster[o.id] || { members: [], admins: [] };
      const adminIds = new Set(r.admins.map(u => u.id));
      const byId = new Map<number, User>();
      [...r.members, ...r.admins].forEach(u => { byId.set(u.id, u); assigned.add(u.id); });
      const list = [...byId.values()].sort((a, b) => a.username.localeCompare(b.username));
      return { org: o, users: list, adminIds };
    });
    const unassigned = users.filter(u => !assigned.has(u.id)).sort((a, b) => a.username.localeCompare(b.username));
    return { orgGroups, unassigned };
  }, [orgs, roster, users]);

  const handleDelete = async (id: number) => {
    if (!(await confirmDialog('Delete this user?', { destructive: true, confirmText: 'Delete' }))) return;
    try { await api.deleteUser(id); fetchAll(); } catch (err) { console.error('Failed to delete user', err); }
  };

  if (loading) return <Page><LoadingState label="Loading users" /></Page>;

  const UserRow = (user: User, isOrgAdmin: boolean) => (
    <div key={user.id} onClick={() => openAccess(user)}
      className="group flex items-center gap-3 px-4 py-2.5 hover:bg-white/[0.025] cursor-pointer">
      <div className="h-8 w-8 rounded-full bg-acc/15 grid place-items-center text-acc text-[13px] font-medium shrink-0">{user.username.charAt(0).toUpperCase()}</div>
      <div className="min-w-0">
        <div className="text-[13.5px] font-medium text-ink truncate">{user.username}{user.first_name ? <span className="text-mut font-normal"> · {user.first_name} {user.last_name}</span> : ''}</div>
        <div className="font-mono text-[11px] text-dim truncate">{user.email}</div>
      </div>
      <div className="ml-auto flex items-center gap-2.5 shrink-0">
        {user.is_superuser
          ? <Badge variant="warning"><Shield size={11} className="mr-1" />System admin</Badge>
          : isOrgAdmin ? <Badge variant="warning">Org admin</Badge> : <Badge variant="neutral">Member</Badge>}
        {user.is_active === false && <Badge variant="neutral">Inactive</Badge>}
        <div className="flex items-center gap-1" onClick={e => e.stopPropagation()}>
          <button onClick={() => openAccess(user)} className="p-1.5 rounded-md text-dim hover:text-acc hover:bg-white/5" title="View access"><KeyRound size={16} /></button>
          <button onClick={() => handleDelete(user.id)} className="p-1.5 rounded-md text-dim hover:text-err hover:bg-white/5" title="Delete"><Trash2 size={16} /></button>
        </div>
      </div>
    </div>
  );

  return (
    <Page>
      <PageHeader title="Users" description={`${users.length} total, grouped by organization membership.`} />

      {users.length > 0 && <div className="rounded-xl border border-line overflow-hidden divide-y divide-line">
        {groups.orgGroups.map(({ org, users: list, adminIds }) => (
          <React.Fragment key={org.id}>
            <div className="flex items-center gap-2.5 px-4 h-10 bg-panel2 sticky top-0 z-[1]">
              <Building2 size={14} className="text-grp" />
              <span className="text-[13px] font-semibold text-ink">{org.name}</span>
              <span className="font-mono text-[10.5px] text-dim">{list.length} member{list.length === 1 ? '' : 's'}</span>
            </div>
            {list.length ? list.map(u => UserRow(u, adminIds.has(u.id)))
              : <p className="px-4 py-4 text-[12.5px] text-dim">No members in this organization.</p>}
          </React.Fragment>
        ))}

        {groups.unassigned.length > 0 && (
          <React.Fragment>
            <div className="flex items-center gap-2.5 px-4 h-10 bg-panel2 sticky top-0 z-[1]">
              <UserRound size={14} className="text-dim" />
              <span className="text-[13px] font-semibold text-ink2">No organization</span>
              <span className="font-mono text-[10.5px] text-dim">{groups.unassigned.length}</span>
            </div>
            {groups.unassigned.map(u => UserRow(u, false))}
          </React.Fragment>
        )}

      </div>}
      {users.length === 0 && <EmptyState title="No users found" description="Users appear here after they authenticate or an administrator creates them." />}

      <Modal isOpen={!!accessUser} onClose={() => setAccessUser(null)} title={accessUser ? `Access — ${accessUser.username}` : ''} size="lg">
        {accessUser && (
          <div className="space-y-4">
            {accessUser.is_superuser && (
              <div className="text-sm bg-changed/10 border border-changed/30 rounded-md px-3 py-2 text-changed">
                <Shield size={14} className="inline mr-1" /> System Administrator — full access to everything.
              </div>
            )}
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-line">
                <thead className="bg-panel2"><tr>
                  <th className="px-4 py-2 text-left text-xs font-medium text-mut uppercase">Scope</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-mut uppercase">Resource</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-mut uppercase">Role</th>
                </tr></thead>
                <tbody className="divide-y divide-line">
                  {accessRows.map((r, i) => (
                    <tr key={i} className="hover:bg-white/[0.03]">
                      <td className="px-4 py-2 text-sm text-mut capitalize">{r.content_type ? r.content_type.replace('_', ' ') : 'System'}</td>
                      <td className="px-4 py-2 text-sm font-medium text-ink">{r.content_type ? (r.resource_name || `#${r.object_id}`) : '—'}</td>
                      <td className="px-4 py-2 text-sm">
                        <Badge variant={/Admin(istrator)?$/.test(r.role) ? 'warning' : /Member$/.test(r.role) ? 'info' : 'neutral'}>
                          {r.role}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                  {accessRows.length === 0 && <tr><td colSpan={3} className="px-4 py-6 text-center text-sm text-dim">No roles assigned. Grant access from a resource's Access tab.</td></tr>}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </Modal>
    </Page>
  );
};

export default UsersPage;
