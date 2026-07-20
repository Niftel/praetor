import React, { useState, useEffect } from 'react';
import { api, unwrap } from '../services/api';
import { Organization, User, Team, Role } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import Modal from '../components/ui/Modal';
import { Input, Textarea, Select } from '../components/ui/Input';
import ResourceAccess from '../components/ResourceAccess';
import { Building2, Users, ShieldCheck, UserPlus, Trash2, Key, Package, Plus } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { EmptyState, FormActions, FormErrorSummary, FormSection, LoadingState, Page, PageHeader } from '../components/ui';

const OrganizationsPage = () => {
    const [organizations, setOrganizations] = useState<Organization[]>([]);
    const [loading, setLoading] = useState(true);
    const [showModal, setShowModal] = useState(false);
    const [formData, setFormData] = useState({ name: '', description: '' });
    const [selectedOrg, setSelectedOrg] = useState<Organization | null>(null);
    const [orgMembers, setOrgMembers] = useState<User[]>([]);
    const [orgAdmins, setOrgAdmins] = useState<User[]>([]);
    const [orgTeams, setOrgTeams] = useState<Team[]>([]);
    const [orgRoles, setOrgRoles] = useState<Role[]>([]);
    const [allUsers, setAllUsers] = useState<User[]>([]);
    const [showAddMemberModal, setShowAddMemberModal] = useState(false);
    const [showAddAdminModal, setShowAddAdminModal] = useState(false);
    const [selectedUserId, setSelectedUserId] = useState<number>(0);
    const [creating, setCreating] = useState(false);
    const [formErrors, setFormErrors] = useState<string[]>([]);

    // Galaxy / Automation Hub credentials
    const [orgGalaxyCreds, setOrgGalaxyCreds] = useState<any[]>([]);
    const [allCredentials, setAllCredentials] = useState<any[]>([]);
    const [galaxyTypeId, setGalaxyTypeId] = useState<number>(0);
    const [showAddGalaxyModal, setShowAddGalaxyModal] = useState(false);
    const [selectedGalaxyCredId, setSelectedGalaxyCredId] = useState<number>(0);

    const fetchOrganizations = async () => {
        try {
            setLoading(true);
            const response = await api.getOrganizations();
            const items = unwrap(response);
            setOrganizations(items);
        } catch (err) {
            console.error('Failed to load organizations', err);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchOrganizations();
        api.getUsers().then(res => setAllUsers(unwrap(res))).catch(() => { });
        api.getCredentials().then(res => setAllCredentials(unwrap(res))).catch(() => { });
        api.getCredentialTypes().then((types: any[]) => {
            const galaxy = (types || []).find(t => /galaxy/i.test(t.name));
            if (galaxy) setGalaxyTypeId(galaxy.id);
        }).catch(() => { });
    }, []);

    const handleCreate = async () => {
        if (creating) return;
        if (!formData.name.trim()) { setFormErrors(['Name is required.']); return; }
        setCreating(true);
        setFormErrors([]);
        try {
            await api.createOrganization({ ...formData, name: formData.name.trim() });
            setShowModal(false);
            setFormData({ name: '', description: '' });
            fetchOrganizations();
        } catch { setFormErrors(['Praetor could not create this organization. No changes were saved.']); }
        finally { setCreating(false); }
    };

    const handleDelete = async (id: number) => {
        if (!(await confirmDialog('Delete this organization? This will remove all associated data.'))) return;
        try {
            await api.deleteOrganization(id);
            fetchOrganizations();
            if (selectedOrg?.id === id) setSelectedOrg(null);
        } catch (err) {
            console.error('Failed to delete organization', err);
        }
    };

    const loadOrgDetails = async (org: Organization) => {
        setSelectedOrg(org);
        try {
            const [members, admins, teams, roles, galaxy] = await Promise.all([
                api.getOrganizationUsers(org.id).catch(() => []),
                api.getOrganizationAdmins(org.id).catch(() => []),
                api.getOrganizationTeams(org.id).catch(() => []),
                api.getOrganizationRoles(org.id).catch(() => []),
                api.getOrgGalaxyCredentials(org.id).catch(() => []),
            ]);
            setOrgMembers(members || []);
            setOrgAdmins(admins || []);
            setOrgTeams(teams || []);
            setOrgRoles(roles || []);
            setOrgGalaxyCreds(galaxy || []);
        } catch (err) {
            console.error('Failed to load org details', err);
        }
    };

    const handleAddGalaxyCred = async () => {
        if (!selectedOrg || !selectedGalaxyCredId) return;
        try {
            await api.addOrgGalaxyCredential(selectedOrg.id, selectedGalaxyCredId);
            loadOrgDetails(selectedOrg);
            setShowAddGalaxyModal(false);
            setSelectedGalaxyCredId(0);
        } catch (err) {
            toast.error('Failed to attach Galaxy credential');
        }
    };

    const handleRemoveGalaxyCred = async (credId: number) => {
        if (!selectedOrg) return;
        try {
            await api.removeOrgGalaxyCredential(selectedOrg.id, credId);
            loadOrgDetails(selectedOrg);
        } catch (err) {
            console.error('Failed to remove Galaxy credential', err);
        }
    };

    // Galaxy-type credentials in this org not already attached.
    const availableGalaxyCreds = () =>
        allCredentials.filter(c =>
            c.credential_type_id === galaxyTypeId &&
            (c.organization_id === selectedOrg?.id) &&
            !orgGalaxyCreds.find(g => g.credential_id === c.id));

    const handleAddMember = async () => {
        if (!selectedOrg || !selectedUserId) return;
        try {
            await api.addOrganizationUser(selectedOrg.id, selectedUserId);
            loadOrgDetails(selectedOrg);
            setShowAddMemberModal(false);
            setSelectedUserId(0);
        } catch (err) {
            toast.error('Failed to add member');
        }
    };

    const handleRemoveMember = async (userId: number) => {
        if (!selectedOrg) return;
        try {
            await api.removeOrganizationUser(selectedOrg.id, userId);
            loadOrgDetails(selectedOrg);
        } catch (err) {
            console.error('Failed to remove member', err);
        }
    };

    const handleAddAdmin = async () => {
        if (!selectedOrg || !selectedUserId) return;
        try {
            await api.addOrganizationAdmin(selectedOrg.id, selectedUserId);
            loadOrgDetails(selectedOrg);
            setShowAddAdminModal(false);
            setSelectedUserId(0);
        } catch (err) {
            toast.error('Failed to add admin');
        }
    };

    if (loading) return <Page width="wide"><LoadingState label="Loading organizations" /></Page>;

    return (
        <Page width="wide">
            <PageHeader title="Organizations" description="Top-level boundaries for automation resources, membership, teams, and delegated access." actions={<Button icon={<Building2 size={16} />} onClick={() => { setFormErrors([]); setShowModal(true); }}>Add organization</Button>} />

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                {/* Organization List */}
                <div className="lg:col-span-1">
                    <Card>
                        <h2 className="text-lg font-semibold mb-4">All Organizations</h2>
                        <div className="space-y-2">
                            {organizations.map(org => (
                                <div
                                    key={org.id}
                                    className={`p-3 rounded-lg cursor-pointer transition-colors ${selectedOrg?.id === org.id ? 'bg-acc/10 border border-acc/30' : 'hover:bg-white/[0.03] border border-transparent'
                                        }`}
                                    onClick={() => loadOrgDetails(org)}
                                >
                                    <div className="flex items-center justify-between">
                                        <div className="flex items-center gap-3">
                                            <div className="p-2 bg-grp/10 text-grp rounded-lg">
                                                <Building2 size={20} />
                                            </div>
                                            <div>
                                                <div className="font-medium text-ink">{org.name}</div>
                                                {org.description && <div className="text-sm text-mut truncate max-w-[200px]">{org.description}</div>}
                                            </div>
                                        </div>
                                        <button onClick={(e) => { e.stopPropagation(); handleDelete(org.id); }} className="text-dim hover:text-err">
                                            <Trash2 size={16} />
                                        </button>
                                    </div>
                                </div>
                            ))}
                            {organizations.length === 0 && (
                                <EmptyState className="min-h-40" title="No organizations found" description="Create an organization to define the first automation boundary." />
                            )}
                        </div>
                    </Card>
                </div>

                {/* Organization Details */}
                <div className="lg:col-span-2">
                    {selectedOrg ? (
                        <div className="space-y-6">
                            <Card>
                                <div className="flex items-center gap-4 mb-6">
                                    <div className="p-3 bg-grp/15 text-grp rounded-xl">
                                        <Building2 size={28} />
                                    </div>
                                    <div>
                                        <h2 className="text-xl font-bold text-ink">{selectedOrg.name}</h2>
                                        <p className="text-mut">{selectedOrg.description || 'No description'}</p>
                                    </div>
                                </div>
                                <div className="grid grid-cols-3 gap-4 text-center">
                                    <div className="p-4 bg-panel2 rounded-lg">
                                        <div className="text-2xl font-semibold tracking-tight text-ink">{orgMembers.length}</div>
                                        <div className="text-sm text-mut">Members</div>
                                    </div>
                                    <div className="p-4 bg-panel2 rounded-lg">
                                        <div className="text-2xl font-semibold tracking-tight text-ink">{orgAdmins.length}</div>
                                        <div className="text-sm text-mut">Admins</div>
                                    </div>
                                    <div className="p-4 bg-panel2 rounded-lg">
                                        <div className="text-2xl font-semibold tracking-tight text-ink">{orgTeams.length}</div>
                                        <div className="text-sm text-mut">Teams</div>
                                    </div>
                                </div>
                            </Card>

                            {/* Admins Section */}
                            <Card>
                                <div className="flex items-center justify-between mb-4">
                                    <h3 className="text-lg font-semibold flex items-center gap-2">
                                        <ShieldCheck size={20} className="text-changed" />
                                        Administrators
                                    </h3>
                                    <Button variant="secondary" size="sm" icon={<UserPlus size={14} />} onClick={() => setShowAddAdminModal(true)}>
                                        Add Admin
                                    </Button>
                                </div>
                                <div className="space-y-2">
                                    {orgAdmins.map(user => (
                                        <div key={user.id} className="flex items-center justify-between p-2 rounded hover:bg-white/[0.03]">
                                            <div className="flex items-center gap-3">
                                                <div className="h-8 w-8 rounded-full bg-changed/15 flex items-center justify-center text-changed font-medium">
                                                    {user.username.charAt(0).toUpperCase()}
                                                </div>
                                                <span className="text-ink">{user.username}</span>
                                            </div>
                                            <Badge variant="warning">Admin</Badge>
                                        </div>
                                    ))}
                                    {orgAdmins.length === 0 && <div className="text-mut text-center py-4">No admins assigned</div>}
                                </div>
                            </Card>

                            {/* Members Section */}
                            <Card>
                                <div className="flex items-center justify-between mb-4">
                                    <h3 className="text-lg font-semibold flex items-center gap-2">
                                        <Users size={20} className="text-run" />
                                        Members
                                    </h3>
                                    <Button variant="secondary" size="sm" icon={<UserPlus size={14} />} onClick={() => setShowAddMemberModal(true)}>
                                        Add Member
                                    </Button>
                                </div>
                                <div className="space-y-2">
                                    {orgMembers.map(user => (
                                        <div key={user.id} className="flex items-center justify-between p-2 rounded hover:bg-white/[0.03]">
                                            <div className="flex items-center gap-3">
                                                <div className="h-8 w-8 rounded-full bg-run/15 flex items-center justify-center text-run font-medium">
                                                    {user.username.charAt(0).toUpperCase()}
                                                </div>
                                                <span className="text-ink">{user.username}</span>
                                            </div>
                                            <button onClick={() => handleRemoveMember(user.id)} className="text-dim hover:text-err">
                                                <Trash2 size={16} />
                                            </button>
                                        </div>
                                    ))}
                                    {orgMembers.length === 0 && <div className="text-mut text-center py-4">No members. Add users to grant them access.</div>}
                                </div>
                            </Card>

                            {/* Access — all roles on this org (admin, member, and the delegated
                                project/inventory/credential/etc. admins), with who holds them */}
                            <Card>
                                <h3 className="text-lg font-semibold flex items-center gap-2 mb-4">
                                    <Key size={20} className="text-violet" />
                                    Access
                                </h3>
                                <ResourceAccess contentType="organization" objectId={selectedOrg.id} />
                            </Card>

                            {/* Teams Section */}
                            <Card>
                                <h3 className="text-lg font-semibold flex items-center gap-2 mb-4">
                                    <Users size={20} className="text-ok" />
                                    Teams
                                </h3>
                                <div className="space-y-2">
                                    {orgTeams.map(team => (
                                        <div key={team.id} className="flex items-center justify-between p-2 rounded hover:bg-white/[0.03]">
                                            <div className="flex items-center gap-3">
                                                <div className="h-8 w-8 rounded-full bg-ok/15 flex items-center justify-center text-ok font-medium">
                                                    {team.name.charAt(0).toUpperCase()}
                                                </div>
                                                <span className="text-ink">{team.name}</span>
                                            </div>
                                            <Badge variant="neutral">{team.description || 'No description'}</Badge>
                                        </div>
                                    ))}
                                    {orgTeams.length === 0 && <div className="text-mut text-center py-4">No teams in this organization</div>}
                                </div>
                            </Card>

                            {/* Galaxy / Automation Hub Credentials */}
                            <Card>
                                <div className="flex items-center justify-between mb-4">
                                    <h3 className="text-lg font-semibold flex items-center gap-2">
                                        <Package size={20} className="text-err" />
                                        Galaxy Credentials
                                    </h3>
                                    <Button variant="secondary" size="sm" icon={<Plus size={14} />} onClick={() => { setSelectedGalaxyCredId(0); setShowAddGalaxyModal(true); }}>
                                        Attach
                                    </Button>
                                </div>
                                <p className="text-xs text-mut mb-3">Private Ansible Galaxy / Automation Hub servers used to install this org's project requirements (in order).</p>
                                <div className="space-y-2">
                                    {orgGalaxyCreds.map(gc => (
                                        <div key={gc.id} className="flex items-center justify-between p-2 rounded hover:bg-white/[0.03]">
                                            <div className="flex items-center gap-3">
                                                <div className="h-8 w-8 rounded-full bg-err/15 flex items-center justify-center text-err">
                                                    <Package size={16} />
                                                </div>
                                                <span className="text-ink">{gc.name}</span>
                                            </div>
                                            <button onClick={() => handleRemoveGalaxyCred(gc.credential_id)} className="text-dim hover:text-err">
                                                <Trash2 size={16} />
                                            </button>
                                        </div>
                                    ))}
                                    {orgGalaxyCreds.length === 0 && <div className="text-mut text-center py-4">None attached — the public galaxy.ansible.com is used.</div>}
                                </div>
                            </Card>
                        </div>
                    ) : (
                        <EmptyState title="Select an organization" description="Choose an organization to inspect its members, administrators, teams, access, and Galaxy credentials." />
                    )}
                </div>
            </div>

            {/* Create Organization Modal */}
            <Modal isOpen={showModal} onClose={() => { if (!creating) setShowModal(false); }} title="Create organization">
                <form onSubmit={event => { event.preventDefault(); void handleCreate(); }} className="space-y-4">
                    <FormErrorSummary errors={formErrors} />
                    <FormSection title="Organization details">
                    <Input
                        label="Name"
                        type="text"
                        value={formData.name}
                        onChange={e => setFormData({ ...formData, name: e.target.value })}
                        error={formErrors.includes('Name is required.') ? 'Enter an organization name.' : undefined}
                    />
                    <Textarea
                        label="Description"
                        rows={3}
                        value={formData.description}
                        onChange={e => setFormData({ ...formData, description: e.target.value })}
                    />
                    </FormSection>
                    <FormActions onCancel={() => setShowModal(false)} submitting={creating} submitLabel="Create organization" />
                </form>
            </Modal>

            {/* Add Member Modal */}
            <Modal isOpen={showAddMemberModal} onClose={() => setShowAddMemberModal(false)} title="Add Member">
                <div className="space-y-4">
                    <Select
                        label="Select User"
                        value={selectedUserId}
                        onChange={e => setSelectedUserId(Number(e.target.value))}
                    >
                        <option value={0}>-- Select a user --</option>
                        {allUsers.filter(u => !orgMembers.find(m => m.id === u.id)).map(user => (
                            <option key={user.id} value={user.id}>{user.username}</option>
                        ))}
                    </Select>
                    <div className="flex justify-end gap-2">
                        <Button variant="secondary" onClick={() => setShowAddMemberModal(false)}>Cancel</Button>
                        <Button onClick={handleAddMember}>Add</Button>
                    </div>
                </div>
            </Modal>

            {/* Add Admin Modal */}
            <Modal isOpen={showAddAdminModal} onClose={() => setShowAddAdminModal(false)} title="Add Administrator">
                <div className="space-y-4">
                    <Select
                        label="Select User"
                        value={selectedUserId}
                        onChange={e => setSelectedUserId(Number(e.target.value))}
                    >
                        <option value={0}>-- Select a user --</option>
                        {allUsers.filter(u => !orgAdmins.find(a => a.id === u.id)).map(user => (
                            <option key={user.id} value={user.id}>{user.username}</option>
                        ))}
                    </Select>
                    <div className="flex justify-end gap-2">
                        <Button variant="secondary" onClick={() => setShowAddAdminModal(false)}>Cancel</Button>
                        <Button onClick={handleAddAdmin}>Add</Button>
                    </div>
                </div>
            </Modal>

            {/* Attach Galaxy Credential Modal */}
            <Modal isOpen={showAddGalaxyModal} onClose={() => setShowAddGalaxyModal(false)} title="Attach Galaxy Credential">
                <div className="space-y-4">
                    <div>
                        <Select
                            label="Galaxy / Automation Hub Credential"
                            value={selectedGalaxyCredId}
                            onChange={e => setSelectedGalaxyCredId(Number(e.target.value))}
                        >
                            <option value={0}>-- Select a credential --</option>
                            {availableGalaxyCreds().map(c => (
                                <option key={c.id} value={c.id}>{c.name}</option>
                            ))}
                        </Select>
                        {availableGalaxyCreds().length === 0 && (
                            <p className="text-xs text-mut mt-2">
                                No unattached Galaxy credentials in this organization. Create one on the Credentials page using the
                                &ldquo;Ansible Galaxy/Automation Hub API Token&rdquo; type.
                            </p>
                        )}
                    </div>
                    <div className="flex justify-end gap-2">
                        <Button variant="secondary" onClick={() => setShowAddGalaxyModal(false)}>Cancel</Button>
                        <Button onClick={handleAddGalaxyCred} disabled={!selectedGalaxyCredId}>Attach</Button>
                    </div>
                </div>
            </Modal>
        </Page>
    );
};

export default OrganizationsPage;
