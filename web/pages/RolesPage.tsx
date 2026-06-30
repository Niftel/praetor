import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Role, User, Team } from '../types';
import Card from '../components/ui/Card';
import Badge from '../components/ui/Badge';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Key, Loader, Users, Building2, Layers, Eye, UserPlus, Trash2 } from 'lucide-react';

const RolesPage = () => {
  const [roles, setRoles] = useState<Role[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedRole, setSelectedRole] = useState<Role | null>(null);
  const [roleUsers, setRoleUsers] = useState<User[]>([]);
  const [roleTeams, setRoleTeams] = useState<Team[]>([]);
  const [allUsers, setAllUsers] = useState<User[]>([]);
  const [allTeams, setAllTeams] = useState<Team[]>([]);
  const [showAddUserModal, setShowAddUserModal] = useState(false);
  const [showAddTeamModal, setShowAddTeamModal] = useState(false);
  const [selectedUserId, setSelectedUserId] = useState<number>(0);
  const [selectedTeamId, setSelectedTeamId] = useState<number>(0);

  useEffect(() => {
    const fetchRoles = async () => {
      try {
        setLoading(true);
        const rolesData = await api.getRoles();
        setRoles(rolesData || []);
      } catch (err) {
        console.error('Failed to load roles', err);
      } finally {
        setLoading(false);
      }
    };
    fetchRoles();
    api.getUsers().then(res => setAllUsers(res?.items || res || [])).catch(() => { });
    api.getTeams().then(res => setAllTeams(res?.items || res || [])).catch(() => { });
  }, []);

  const loadRoleDetails = async (role: Role) => {
    setSelectedRole(role);
    try {
      const [users, teams] = await Promise.all([
        api.getRoleUsers(role.id).catch(() => []),
        api.getRoleTeams(role.id).catch(() => []),
      ]);
      setRoleUsers(users || []);
      setRoleTeams(teams || []);
    } catch (err) {
      console.error('Failed to load role details', err);
    }
  };

  const handleAddUser = async () => {
    if (!selectedRole || !selectedUserId) return;
    try {
      await api.addRoleUser(selectedRole.id, selectedUserId);
      loadRoleDetails(selectedRole);
      setShowAddUserModal(false);
      setSelectedUserId(0);
    } catch (err) {
      alert('Failed to add user to role');
    }
  };

  const handleRemoveUser = async (userId: number) => {
    if (!selectedRole) return;
    try {
      await api.removeRoleUser(selectedRole.id, userId);
      loadRoleDetails(selectedRole);
    } catch (err) {
      console.error('Failed to remove user', err);
    }
  };

  const handleAddTeam = async () => {
    if (!selectedRole || !selectedTeamId) return;
    try {
      await api.addRoleTeam(selectedRole.id, selectedTeamId);
      loadRoleDetails(selectedRole);
      setShowAddTeamModal(false);
      setSelectedTeamId(0);
    } catch (err) {
      alert('Failed to add team to role');
    }
  };

  const handleRemoveTeam = async (teamId: number) => {
    if (!selectedRole) return;
    try {
      await api.removeRoleTeam(selectedRole.id, teamId);
      loadRoleDetails(selectedRole);
    } catch (err) {
      console.error('Failed to remove team', err);
    }
  };

  const getRoleIcon = (role: Role) => {
    if (role.singleton_name) return <Key className="text-amber-600" size={20} />;
    switch (role.content_type) {
      case 'organization': return <Building2 className="text-indigo-600" size={20} />;
      case 'team': return <Users className="text-blue-600" size={20} />;
      case 'project': return <Layers className="text-green-600" size={20} />;
      default: return <Key className="text-gray-600" size={20} />;
    }
  };

  const getRoleVariant = (role: Role): 'success' | 'warning' | 'info' | 'neutral' => {
    if (role.singleton_name) return 'warning';
    if (role.role_field === 'admin_role') return 'warning';
    if (role.role_field === 'member_role') return 'info';
    if (role.role_field === 'read_role') return 'neutral';
    return 'success';
  };

  // Only system (singleton) roles are managed here; per-resource roles live on
  // each resource's page.
  const systemRoles = roles.filter(r => r.singleton_name);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader className="animate-spin text-brand-600" size={32} />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-900">Roles</h1>
        <p className="text-sm text-gray-500 mt-1">
          System-wide roles. Per-resource access (organization, project, inventory, credential
          admins, etc.) is granted on each resource's own page, not here.
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* System Roles list */}
        <div className="lg:col-span-1">
          <Card>
            <h2 className="text-lg font-semibold mb-4 flex items-center gap-2">
              <Key className="text-amber-600" size={20} />
              System Roles
            </h2>
            <div className="space-y-2">
              {systemRoles.map(role => (
                <div
                  key={role.id}
                  className={`p-3 rounded-lg cursor-pointer transition-colors ${selectedRole?.id === role.id ? 'bg-amber-50 border border-amber-200' : 'hover:bg-gray-50 border border-transparent'
                    }`}
                  onClick={() => loadRoleDetails(role)}
                >
                  <div className="font-medium text-gray-900">{role.name || role.singleton_name}</div>
                  <div className="text-sm text-gray-500">{role.description}</div>
                </div>
              ))}
              {systemRoles.length === 0 && (
                <div className="text-gray-500 text-sm">No system roles found</div>
              )}
            </div>
          </Card>
        </div>

        {/* Role Details */}
        <div className="lg:col-span-2">
          {selectedRole ? (
            <div className="space-y-6">
              <Card>
                <div className="flex items-center gap-4 mb-6">
                  <div className={`p-3 rounded-xl ${selectedRole.singleton_name ? 'bg-amber-100 text-amber-600' : 'bg-blue-100 text-blue-600'}`}>
                    {getRoleIcon(selectedRole)}
                  </div>
                  <div>
                    <h2 className="text-xl font-bold text-gray-900">{selectedRole.name || selectedRole.role_field}</h2>
                    <p className="text-gray-500">{selectedRole.description}</p>
                    <div className="flex gap-2 mt-2">
                      <Badge variant={getRoleVariant(selectedRole)}>{selectedRole.role_field}</Badge>
                      {selectedRole.content_type && (
                        <Badge variant="neutral">{selectedRole.content_type} #{selectedRole.object_id}</Badge>
                      )}
                      {selectedRole.singleton_name && (
                        <Badge variant="warning">System Role</Badge>
                      )}
                    </div>
                  </div>
                </div>
              </Card>

              {/* Users in Role */}
              <Card>
                <div className="flex items-center justify-between mb-4">
                  <h3 className="text-lg font-semibold flex items-center gap-2">
                    <Users size={20} className="text-blue-600" />
                    Users
                  </h3>
                  <Button variant="secondary" size="sm" icon={<UserPlus size={14} />} onClick={() => setShowAddUserModal(true)}>
                    Add User
                  </Button>
                </div>
                <div className="space-y-2">
                  {roleUsers.map(user => (
                    <div key={user.id} className="flex items-center justify-between p-2 rounded hover:bg-gray-50">
                      <div className="flex items-center gap-3">
                        <div className="h-8 w-8 rounded-full bg-blue-100 flex items-center justify-center text-blue-600 font-medium">
                          {user.username.charAt(0).toUpperCase()}
                        </div>
                        <div>
                          <span className="text-gray-900">{user.username}</span>
                          {user.is_superuser && <Badge variant="warning">Superuser</Badge>}
                        </div>
                      </div>
                      <button onClick={() => handleRemoveUser(user.id)} className="text-gray-400 hover:text-red-600">
                        <Trash2 size={16} />
                      </button>
                    </div>
                  ))}
                  {roleUsers.length === 0 && <div className="text-gray-500 text-center py-4">No users assigned to this role</div>}
                </div>
              </Card>

              {/* Teams with Role */}
              {!selectedRole.singleton_name && (
                <Card>
                  <div className="flex items-center justify-between mb-4">
                    <h3 className="text-lg font-semibold flex items-center gap-2">
                      <Users size={20} className="text-green-600" />
                      Teams
                    </h3>
                    <Button variant="secondary" size="sm" icon={<UserPlus size={14} />} onClick={() => setShowAddTeamModal(true)}>
                      Add Team
                    </Button>
                  </div>
                  <div className="space-y-2">
                    {roleTeams.map(team => (
                      <div key={team.id} className="flex items-center justify-between p-2 rounded hover:bg-gray-50">
                        <div className="flex items-center gap-3">
                          <div className="h-8 w-8 rounded-full bg-green-100 flex items-center justify-center text-green-600 font-medium">
                            {team.name.charAt(0).toUpperCase()}
                          </div>
                          <span className="text-gray-900">{team.name}</span>
                        </div>
                        <button onClick={() => handleRemoveTeam(team.id)} className="text-gray-400 hover:text-red-600">
                          <Trash2 size={16} />
                        </button>
                      </div>
                    ))}
                    {roleTeams.length === 0 && <div className="text-gray-500 text-center py-4">No teams assigned to this role</div>}
                  </div>
                </Card>
              )}
            </div>
          ) : (
            <Card className="h-full flex items-center justify-center py-16">
              <div className="text-center text-gray-500">
                <Eye size={48} className="mx-auto mb-4 text-gray-300" />
                <p>Select a role to view details and manage access</p>
              </div>
            </Card>
          )}
        </div>
      </div>

      {/* Add User Modal */}
      <Modal isOpen={showAddUserModal} onClose={() => setShowAddUserModal(false)} title="Add User to Role">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Select User</label>
            <select
              className="w-full border border-gray-300 rounded-md p-2"
              value={selectedUserId}
              onChange={e => setSelectedUserId(Number(e.target.value))}
            >
              <option value={0}>-- Select a user --</option>
              {allUsers.filter(u => !roleUsers.find(ru => ru.id === u.id)).map(user => (
                <option key={user.id} value={user.id}>{user.username}</option>
              ))}
            </select>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowAddUserModal(false)}>Cancel</Button>
            <Button onClick={handleAddUser}>Add</Button>
          </div>
        </div>
      </Modal>

      {/* Add Team Modal */}
      <Modal isOpen={showAddTeamModal} onClose={() => setShowAddTeamModal(false)} title="Add Team to Role">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Select Team</label>
            <select
              className="w-full border border-gray-300 rounded-md p-2"
              value={selectedTeamId}
              onChange={e => setSelectedTeamId(Number(e.target.value))}
            >
              <option value={0}>-- Select a team --</option>
              {allTeams.filter(t => !roleTeams.find(rt => rt.id === t.id)).map(team => (
                <option key={team.id} value={team.id}>{team.name}</option>
              ))}
            </select>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowAddTeamModal(false)}>Cancel</Button>
            <Button onClick={handleAddTeam}>Add</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default RolesPage;