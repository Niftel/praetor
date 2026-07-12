import React, { useState, useEffect } from 'react';
import { api, unwrap } from '../services/api';
import { Team, User } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Input, Textarea, Select } from '../components/ui/Input';
import { Users, Plus, Trash2, Loader } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';

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
      api.getOrganizations().then(o => setOrgs(unwrap(o))).catch(() => setOrgs([]));
      const teamsResponse = await api.getTeams();
      const teamItems: Team[] = unwrap(teamsResponse);

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
      <PageSpinner />
    );
  }

  return (
    <div className="space-y-6 p-8 max-w-[1160px] mx-auto">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-semibold tracking-tight text-ink">Teams</h1>
        <Button icon={<Plus size={16} />} onClick={openModal}>Create Team</Button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {teams.map(team => (
          <Card key={team.id} className="relative">
            <button
              onClick={() => handleDelete(team.id)}
              className="absolute top-4 right-4 text-dim hover:text-err"
            >
              <Trash2 size={16} />
            </button>
            <div className="flex items-center gap-3 mb-4">
              <div className="p-3 bg-grp/10 text-grp rounded-lg">
                <Users size={24} />
              </div>
              <div>
                <h3 className="font-semibold text-ink">{team.name}</h3>
                <p className="text-sm text-mut">{team.members?.length || 0} members</p>
              </div>
            </div>
            {team.description && (
              <p className="text-sm text-ink2 mb-4">{team.description}</p>
            )}
            <div className="flex -space-x-2 overflow-hidden">
              {team.members?.slice(0, 5).map(member => (
                <div
                  key={member.id}
                  className="inline-block h-8 w-8 rounded-full bg-acc/15 text-acc flex items-center justify-center text-xs font-medium ring-2 ring-bg"
                  title={member.username}
                >
                  {member.username.charAt(0).toUpperCase()}
                </div>
              ))}
              {(team.members?.length || 0) > 5 && (
                <div className="inline-block h-8 w-8 rounded-full bg-white/5 text-ink2 flex items-center justify-center text-xs font-medium ring-2 ring-bg">
                  +{team.members!.length - 5}
                </div>
              )}
            </div>
          </Card>
        ))}
        {teams.length === 0 && (
          <Card className="col-span-full text-center py-12 text-mut">
            No teams found. Click "Create Team" to add one.
          </Card>
        )}
      </div>

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title="Create Team">
        <div className="space-y-4">
          <Select
            label="Organization"
            value={formData.organization_id}
            onChange={e => setFormData({ ...formData, organization_id: Number(e.target.value) })}
          >
            <option value="">Select organization…</option>
            {orgs.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}
          </Select>
          <Input
            label="Name"
            type="text"
            value={formData.name}
            onChange={e => setFormData({ ...formData, name: e.target.value })}
            placeholder="DevOps Team"
          />
          <Textarea
            label="Description"
            value={formData.description}
            onChange={e => setFormData({ ...formData, description: e.target.value })}
            placeholder="Optional description"
            rows={3}
          />
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