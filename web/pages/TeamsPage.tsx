import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Team, User } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Users, Plus, Trash2, Loader } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';

interface TeamWithMembers extends Team {
  members?: User[];
}

const TeamsPage = () => {
  const [teams, setTeams] = useState<TeamWithMembers[]>([]);
  const [orgs, setOrgs] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [formData, setFormData] = useState<{ name: string; description: string; organization_id: number | '' }>({ name: '', description: '', organization_id: '' });

  const fetchTeams = async () => {
    try {
      setLoading(true);
      api.getOrganizations().then(o => setOrgs(o?.items || o || [])).catch(() => setOrgs([]));
      const teamsResponse = await api.getTeams();
      const teamItems: Team[] = teamsResponse?.items || teamsResponse || [];

      const teamsWithMembers = await Promise.all(
        teamItems.map(async (team) => {
          try {
            const members = await api.getTeamMembers(team.id);
            return { ...team, members: members || [] };
          } catch {
            return { ...team, members: [] };
          }
        })
      );

      setTeams(teamsWithMembers);
    } catch (err) {
      console.error('Failed to load teams', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchTeams();
  }, []);

  const openModal = () => { setFormData({ name: '', description: '', organization_id: orgs[0]?.id ?? '' }); setShowModal(true); };

  const handleCreate = async () => {
    if (!formData.name || formData.organization_id === '') return;
    try {
      await api.createTeam(formData);
      setShowModal(false);
      setFormData({ name: '', description: '', organization_id: '' });
      fetchTeams();
    } catch (err) {
      console.error('Failed to create team', err);
      toast.error('Failed to create team');
    }
  };

  const handleDelete = async (id: number) => {
    if (!(await confirmDialog('Delete this team?'))) return;
    try {
      await api.deleteTeam(id);
      fetchTeams();
    } catch (err) {
      console.error('Failed to delete team', err);
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
        <h1 className="text-2xl font-bold text-gray-900">Teams</h1>
        <Button icon={<Plus size={16} />} onClick={openModal}>Create Team</Button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {teams.map(team => (
          <Card key={team.id} className="relative">
            <button
              onClick={() => handleDelete(team.id)}
              className="absolute top-4 right-4 text-gray-400 hover:text-red-600"
            >
              <Trash2 size={16} />
            </button>
            <div className="flex items-center gap-3 mb-4">
              <div className="p-3 bg-indigo-50 text-indigo-600 rounded-lg">
                <Users size={24} />
              </div>
              <div>
                <h3 className="font-semibold text-gray-900">{team.name}</h3>
                <p className="text-sm text-gray-500">{team.members?.length || 0} members</p>
              </div>
            </div>
            {team.description && (
              <p className="text-sm text-gray-600 mb-4">{team.description}</p>
            )}
            <div className="flex -space-x-2 overflow-hidden">
              {team.members?.slice(0, 5).map(member => (
                <div
                  key={member.id}
                  className="inline-block h-8 w-8 rounded-full bg-brand-100 text-brand-600 flex items-center justify-center text-xs font-medium ring-2 ring-white"
                  title={member.username}
                >
                  {member.username.charAt(0).toUpperCase()}
                </div>
              ))}
              {(team.members?.length || 0) > 5 && (
                <div className="inline-block h-8 w-8 rounded-full bg-gray-100 text-gray-600 flex items-center justify-center text-xs font-medium ring-2 ring-white">
                  +{team.members!.length - 5}
                </div>
              )}
            </div>
          </Card>
        ))}
        {teams.length === 0 && (
          <Card className="col-span-full text-center py-12 text-gray-500">
            No teams found. Click "Create Team" to add one.
          </Card>
        )}
      </div>

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title="Create Team">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Organization</label>
            <select
              className="w-full border border-gray-300 rounded-md p-2"
              value={formData.organization_id}
              onChange={e => setFormData({ ...formData, organization_id: Number(e.target.value) })}
            >
              <option value="">Select organization…</option>
              {orgs.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Name</label>
            <input
              type="text"
              className="w-full border border-gray-300 rounded-md p-2"
              value={formData.name}
              onChange={e => setFormData({ ...formData, name: e.target.value })}
              placeholder="DevOps Team"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Description</label>
            <textarea
              className="w-full border border-gray-300 rounded-md p-2"
              value={formData.description}
              onChange={e => setFormData({ ...formData, description: e.target.value })}
              placeholder="Optional description"
              rows={3}
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button onClick={handleCreate}>Create</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default TeamsPage;