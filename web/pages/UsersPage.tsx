import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { User } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import Modal from '../components/ui/Modal';
import { roleLabel } from '../components/ResourceAccess';
import { UserPlus, Shield, Trash2, Loader, KeyRound } from 'lucide-react';

const UsersPage = () => {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [formData, setFormData] = useState({ username: '', email: '', password: '', is_superuser: false });
  const [accessUser, setAccessUser] = useState<User | null>(null);
  const [accessRows, setAccessRows] = useState<any[]>([]);

  const openAccess = (user: User) => {
    setAccessUser(user);
    setAccessRows([]);
    api.getUserAccess(user.id).then(d => setAccessRows(d || [])).catch(() => setAccessRows([]));
  };

  const fetchUsers = async () => {
    try {
      setLoading(true);
      const response = await api.getUsers();
      const items = response?.items || response || [];
      setUsers(items);
    } catch (err) {
      console.error('Failed to load users', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchUsers();
  }, []);

  const handleCreate = async () => {
    if (!formData.username || !formData.email || !formData.password) return;
    try {
      await api.createUser(formData);
      setShowModal(false);
      setFormData({ username: '', email: '', password: '', is_superuser: false });
      fetchUsers();
    } catch (err) {
      console.error('Failed to create user', err);
      alert('Failed to create user');
    }
  };

  const handleDelete = async (id: number) => {
    if (!confirm('Delete this user?')) return;
    try {
      await api.deleteUser(id);
      fetchUsers();
    } catch (err) {
      console.error('Failed to delete user', err);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader className="animate-spin text-brand-600" size={32} />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-900">Users</h1>
        <Button icon={<UserPlus size={16} />} onClick={() => setShowModal(true)}>Add User</Button>
      </div>

      <Card>
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">User</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Email</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Role</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
              <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Actions</th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {users.map((user) => (
              <tr key={user.id} className="hover:bg-gray-50 cursor-pointer" onClick={() => openAccess(user)}>
                <td className="px-6 py-4 whitespace-nowrap">
                  <div className="flex items-center">
                    <div className="h-8 w-8 rounded-full bg-brand-100 flex items-center justify-center text-brand-600 font-medium">
                      {user.username.charAt(0).toUpperCase()}
                    </div>
                    <div className="ml-3">
                      <div className="text-sm font-medium text-gray-900">{user.username}</div>
                      {user.first_name && <div className="text-sm text-gray-500">{user.first_name} {user.last_name}</div>}
                    </div>
                  </div>
                </td>
                <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500">{user.email}</td>
                <td className="px-6 py-4 whitespace-nowrap">
                  {user.is_superuser ? (
                    <Badge variant="info"><Shield size={12} className="mr-1" />Admin</Badge>
                  ) : (
                    <Badge variant="neutral">User</Badge>
                  )}
                </td>
                <td className="px-6 py-4 whitespace-nowrap">
                  <Badge variant={user.is_active !== false ? 'success' : 'neutral'}>
                    {user.is_active !== false ? 'Active' : 'Inactive'}
                  </Badge>
                </td>
                <td className="px-6 py-4 whitespace-nowrap text-right" onClick={e => e.stopPropagation()}>
                  <button
                    onClick={() => openAccess(user)}
                    className="text-gray-400 hover:text-brand-600 mr-3"
                    title="View access"
                  >
                    <KeyRound size={18} />
                  </button>
                  <button
                    onClick={() => handleDelete(user.id)}
                    className="text-gray-400 hover:text-red-600"
                    title="Delete"
                  >
                    <Trash2 size={18} />
                  </button>
                </td>
              </tr>
            ))}
            {users.length === 0 && (
              <tr>
                <td colSpan={5} className="px-6 py-8 text-center text-gray-500">No users found.</td>
              </tr>
            )}
          </tbody>
        </table>
      </Card>

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title="Add User">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Username</label>
            <input
              type="text"
              className="w-full border border-gray-300 rounded-md p-2"
              value={formData.username}
              onChange={e => setFormData({ ...formData, username: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Email</label>
            <input
              type="email"
              className="w-full border border-gray-300 rounded-md p-2"
              value={formData.email}
              onChange={e => setFormData({ ...formData, email: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Password</label>
            <input
              type="password"
              className="w-full border border-gray-300 rounded-md p-2"
              value={formData.password}
              onChange={e => setFormData({ ...formData, password: e.target.value })}
            />
          </div>
          <div>
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={formData.is_superuser}
                onChange={e => setFormData({ ...formData, is_superuser: e.target.checked })}
              />
              <span className="text-sm text-gray-700">Admin user</span>
            </label>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button onClick={handleCreate}>Create</Button>
          </div>
        </div>
      </Modal>

      {/* Per-user access: the roles this user holds and where */}
      <Modal isOpen={!!accessUser} onClose={() => setAccessUser(null)} title={accessUser ? `Access — ${accessUser.username}` : ''} size="lg">
        {accessUser && (
          <div className="space-y-4">
            {accessUser.is_superuser && (
              <div className="text-sm bg-amber-50 border border-amber-200 rounded-md px-3 py-2 text-amber-800">
                <Shield size={14} className="inline mr-1" /> System Administrator — full access to everything.
              </div>
            )}
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Scope</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Resource</th>
                  <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Role</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {accessRows.map((r, i) => (
                  <tr key={i} className="hover:bg-gray-50">
                    <td className="px-4 py-2 text-sm text-gray-500 capitalize">{r.singleton_name ? 'System' : (r.content_type || '').replace('_', ' ')}</td>
                    <td className="px-4 py-2 text-sm font-medium text-gray-900">{r.singleton_name ? '—' : (r.resource_name || `#${r.object_id}`)}</td>
                    <td className="px-4 py-2 text-sm">
                      <Badge variant={r.singleton_name === 'system_administrator' ? 'warning' : r.role_field === 'admin_role' ? 'warning' : r.role_field === 'member_role' ? 'info' : 'neutral'}>
                        {r.singleton_name ? r.singleton_name.replace(/_/g, ' ') : roleLabel(r.role_field)}
                      </Badge>
                    </td>
                  </tr>
                ))}
                {accessRows.length === 0 && (
                  <tr><td colSpan={3} className="px-4 py-6 text-center text-sm text-gray-400">No roles assigned. Grant access from a resource's Access tab.</td></tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </Modal>
    </div>
  );
};

export default UsersPage;