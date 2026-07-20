import React, { useState, useEffect } from 'react';
import { api, unwrap } from '../services/api';
import { Team, User } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Input, Textarea, Select } from '../components/ui/Input';
import { Users, Plus, Trash2 } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { EmptyState, FormActions, FormErrorSummary, FormSection, LoadingState, Page, PageHeader, useDirtyFormGuard } from '../components/ui';

interface TeamWithMembers extends Team {
  members?: User[];
}

const TeamsPage = () => {
  const [teams, setTeams] = useState<TeamWithMembers[]>([]);
  const [orgs, setOrgs] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [formData, setFormData] = useState<{ name: string; description: string; organization_id: number | '' }>({ name: '', description: '', organization_id: '' });
  const [creating, setCreating] = useState(false);
  const [formErrors, setFormErrors] = useState<string[]>([]);

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

  const openModal = () => { setFormData({ name: '', description: '', organization_id: orgs[0]?.id ?? '' }); setFormErrors([]); setShowModal(true); };

  const handleCreate = async () => {
    if (creating) return;
    const errors: string[] = [];
    if (!formData.name.trim()) errors.push('Name is required.');
    if (formData.organization_id === '') errors.push('Organization is required.');
    setFormErrors(errors);
    if (errors.length) return;
    setCreating(true);
    try {
      await api.createTeam(formData);
      setShowModal(false);
      setFormData({ name: '', description: '', organization_id: '' });
      fetchTeams();
    } catch { setFormErrors(['Praetor could not create this team. No changes were saved.']); }
    finally { setCreating(false); }
  };
  const dirty = showModal && Boolean(formData.name || formData.description);
  const canDiscard = useDirtyFormGuard(dirty);
  const closeForm = async () => { if (creating || !(await canDiscard())) return; setShowModal(false); setFormErrors([]); };

  const handleDelete = async (id: number) => {
    if (!(await confirmDialog('Delete this team?'))) return;
    try {
      await api.deleteTeam(id);
      fetchTeams();
    } catch (err) {
      console.error('Failed to delete team', err);
    }
  };

  if (loading) return <Page><LoadingState label="Loading teams" /></Page>;

  return (
    <Page width="wide">
      <PageHeader title="Teams" description="Groups of users that receive delegated access to Praetor resources." actions={<Button icon={<Plus size={16} />} onClick={openModal}>Create team</Button>} />

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
        {teams.length === 0 && <div className="col-span-full"><EmptyState title="No teams yet" description="Create a team to delegate resource access to a group of users." action={<Button icon={<Plus size={15} />} onClick={openModal}>Create team</Button>} /></div>}
      </div>

      <Modal isOpen={showModal} onClose={() => { void closeForm(); }} title="Create team">
        <form onSubmit={event => { event.preventDefault(); void handleCreate(); }} className="space-y-4">
          <FormErrorSummary errors={formErrors} />
          <FormSection title="Team details">
          <Select
            label="Organization"
            value={formData.organization_id}
            error={formErrors.includes('Organization is required.') ? 'Choose an organization.' : undefined}
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
            error={formErrors.includes('Name is required.') ? 'Enter a team name.' : undefined}
          />
          <Textarea
            label="Description"
            value={formData.description}
            onChange={e => setFormData({ ...formData, description: e.target.value })}
            placeholder="Optional description"
            rows={3}
          />
          </FormSection>
          <FormActions onCancel={() => { void closeForm(); }} submitting={creating} submitLabel="Create team" />
        </form>
      </Modal>
    </Page>
  );
};

export default TeamsPage;
