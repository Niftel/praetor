import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Organization, User, Team, Role } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Badge from '../components/ui/Badge';
import Modal from '../components/ui/Modal';
import ResourceAccess from '../components/ResourceAccess';
import { Building2, Users, ShieldCheck, UserPlus, Trash2, Loader, Eye, Key, Package, Plus } from 'lucide-react';

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
            const items = response?.items || response || [];
            setOrganizations(items);
        } catch (err) {
            console.error('Failed to load organizations', err);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchOrganizations();
        api.getUsers().then(res => setAllUsers(res?.items || res || [])).catch(() => { });
        api.getCredentials().then(res => setAllCredentials(res?.items || res || [])).catch(() => { });
        api.getCredentialTypes().then((types: any[]) => {
            const galaxy = (types || []).find(t => /galaxy/i.test(t.name));
            if (galaxy) setGalaxyTypeId(galaxy.id);
        }).catch(() => { });
    }, []);

    const handleCreate = async () => {
        if (!formData.name) return;
        try {
            await api.createOrganization(formData);
            setShowModal(false);
            setFormData({ name: '', description: '' });
            fetchOrganizations();
        } catch (err) {
            console.error('Failed to create organization', err);
            alert('Failed to create organization');
        }
    };

    const handleDelete = async (id: number) => {
        if (!confirm('Delete this organization? This will remove all associated data.')) return;
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
            alert('Failed to attach Galaxy credential');
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
            alert('Failed to add member');
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
            alert('Failed to add admin');
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
                <h1 className="text-2xl font-bold text-gray-900">Organizations</h1>
                <Button icon={<Building2 size={16} />} onClick={() => setShowModal(true)}>Add Organization</Button>
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                {/* Organization List */}
                <div className="lg:col-span-1">
                    <Card>
                        <h2 className="text-lg font-semibold mb-4">All Organizations</h2>
                        <div className="space-y-2">
                            {organizations.map(org => (
                                <div
                                    key={org.id}
                                    className={`p-3 rounded-lg cursor-pointer transition-colors ${selectedOrg?.id === org.id ? 'bg-brand-50 border border-brand-200' : 'hover:bg-gray-50 border border-transparent'
                                        }`}
                                    onClick={() => loadOrgDetails(org)}
                                >
                                    <div className="flex items-center justify-between">
                                        <div className="flex items-center gap-3">
                                            <div className="p-2 bg-indigo-50 text-indigo-600 rounded-lg">
                                                <Building2 size={20} />
                                            </div>
                                            <div>
                                                <div className="font-medium text-gray-900">{org.name}</div>
                                                {org.description && <div className="text-sm text-gray-500 truncate max-w-[200px]">{org.description}</div>}
                                            </div>
                                        </div>
                                        <button onClick={(e) => { e.stopPropagation(); handleDelete(org.id); }} className="text-gray-400 hover:text-red-600">
                                            <Trash2 size={16} />
                                        </button>
                                    </div>
                                </div>
                            ))}
                            {organizations.length === 0 && (
                                <div className="text-center py-8 text-gray-500">No organizations found</div>
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
                                    <div className="p-3 bg-indigo-100 text-indigo-600 rounded-xl">
                                        <Building2 size={28} />
                                    </div>
                                    <div>
                                        <h2 className="text-xl font-bold text-gray-900">{selectedOrg.name}</h2>
                                        <p className="text-gray-500">{selectedOrg.description || 'No description'}</p>
                                    </div>
                                </div>
                                <div className="grid grid-cols-3 gap-4 text-center">
                                    <div className="p-4 bg-gray-50 rounded-lg">
                                        <div className="text-2xl font-bold text-gray-900">{orgMembers.length}</div>
                                        <div className="text-sm text-gray-500">Members</div>
                                    </div>
                                    <div className="p-4 bg-gray-50 rounded-lg">
                                        <div className="text-2xl font-bold text-gray-900">{orgAdmins.length}</div>
                                        <div className="text-sm text-gray-500">Admins</div>
                                    </div>
                                    <div className="p-4 bg-gray-50 rounded-lg">
                                        <div className="text-2xl font-bold text-gray-900">{orgTeams.length}</div>
                                        <div className="text-sm text-gray-500">Teams</div>
                                    </div>
                                </div>
                            </Card>

                            {/* Admins Section */}
                            <Card>
                                <div className="flex items-center justify-between mb-4">
                                    <h3 className="text-lg font-semibold flex items-center gap-2">
                                        <ShieldCheck size={20} className="text-amber-600" />
                                        Administrators
                                    </h3>
                                    <Button variant="secondary" size="sm" icon={<UserPlus size={14} />} onClick={() => setShowAddAdminModal(true)}>
                                        Add Admin
                                    </Button>
                                </div>
                                <div className="space-y-2">
                                    {orgAdmins.map(user => (
                                        <div key={user.id} className="flex items-center justify-between p-2 rounded hover:bg-gray-50">
                                            <div className="flex items-center gap-3">
                                                <div className="h-8 w-8 rounded-full bg-amber-100 flex items-center justify-center text-amber-600 font-medium">
                                                    {user.username.charAt(0).toUpperCase()}
                                                </div>
                                                <span className="text-gray-900">{user.username}</span>
                                            </div>
                                            <Badge variant="warning">Admin</Badge>
                                        </div>
                                    ))}
                                    {orgAdmins.length === 0 && <div className="text-gray-500 text-center py-4">No admins assigned</div>}
                                </div>
                            </Card>

                            {/* Members Section */}
                            <Card>
                                <div className="flex items-center justify-between mb-4">
                                    <h3 className="text-lg font-semibold flex items-center gap-2">
                                        <Users size={20} className="text-blue-600" />
                                        Members
                                    </h3>
                                    <Button variant="secondary" size="sm" icon={<UserPlus size={14} />} onClick={() => setShowAddMemberModal(true)}>
                                        Add Member
                                    </Button>
                                </div>
                                <div className="space-y-2">
                                    {orgMembers.map(user => (
                                        <div key={user.id} className="flex items-center justify-between p-2 rounded hover:bg-gray-50">
                                            <div className="flex items-center gap-3">
                                                <div className="h-8 w-8 rounded-full bg-blue-100 flex items-center justify-center text-blue-600 font-medium">
                                                    {user.username.charAt(0).toUpperCase()}
                                                </div>
                                                <span className="text-gray-900">{user.username}</span>
                                            </div>
                                            <button onClick={() => handleRemoveMember(user.id)} className="text-gray-400 hover:text-red-600">
                                                <Trash2 size={16} />
                                            </button>
                                        </div>
                                    ))}
                                    {orgMembers.length === 0 && <div className="text-gray-500 text-center py-4">No members. Add users to grant them access.</div>}
                                </div>
                            </Card>

                            {/* Access — all roles on this org (admin, member, and the delegated
                                project/inventory/credential/etc. admins), with who holds them */}
                            <Card>
                                <h3 className="text-lg font-semibold flex items-center gap-2 mb-4">
                                    <Key size={20} className="text-purple-600" />
                                    Access
                                </h3>
                                <ResourceAccess contentType="organization" objectId={selectedOrg.id} />
                            </Card>

                            {/* Teams Section */}
                            <Card>
                                <h3 className="text-lg font-semibold flex items-center gap-2 mb-4">
                                    <Users size={20} className="text-green-600" />
                                    Teams
                                </h3>
                                <div className="space-y-2">
                                    {orgTeams.map(team => (
                                        <div key={team.id} className="flex items-center justify-between p-2 rounded hover:bg-gray-50">
                                            <div className="flex items-center gap-3">
                                                <div className="h-8 w-8 rounded-full bg-green-100 flex items-center justify-center text-green-600 font-medium">
                                                    {team.name.charAt(0).toUpperCase()}
                                                </div>
                                                <span className="text-gray-900">{team.name}</span>
                                            </div>
                                            <Badge variant="neutral">{team.description || 'No description'}</Badge>
                                        </div>
                                    ))}
                                    {orgTeams.length === 0 && <div className="text-gray-500 text-center py-4">No teams in this organization</div>}
                                </div>
                            </Card>

                            {/* Galaxy / Automation Hub Credentials */}
                            <Card>
                                <div className="flex items-center justify-between mb-4">
                                    <h3 className="text-lg font-semibold flex items-center gap-2">
                                        <Package size={20} className="text-rose-600" />
                                        Galaxy Credentials
                                    </h3>
                                    <Button variant="secondary" size="sm" icon={<Plus size={14} />} onClick={() => { setSelectedGalaxyCredId(0); setShowAddGalaxyModal(true); }}>
                                        Attach
                                    </Button>
                                </div>
                                <p className="text-xs text-gray-500 mb-3">Private Ansible Galaxy / Automation Hub servers used to install this org's project requirements (in order).</p>
                                <div className="space-y-2">
                                    {orgGalaxyCreds.map(gc => (
                                        <div key={gc.id} className="flex items-center justify-between p-2 rounded hover:bg-gray-50">
                                            <div className="flex items-center gap-3">
                                                <div className="h-8 w-8 rounded-full bg-rose-100 flex items-center justify-center text-rose-600">
                                                    <Package size={16} />
                                                </div>
                                                <span className="text-gray-900">{gc.name}</span>
                                            </div>
                                            <button onClick={() => handleRemoveGalaxyCred(gc.credential_id)} className="text-gray-400 hover:text-red-600">
                                                <Trash2 size={16} />
                                            </button>
                                        </div>
                                    ))}
                                    {orgGalaxyCreds.length === 0 && <div className="text-gray-500 text-center py-4">None attached — the public galaxy.ansible.com is used.</div>}
                                </div>
                            </Card>
                        </div>
                    ) : (
                        <Card className="h-full flex items-center justify-center py-16">
                            <div className="text-center text-gray-500">
                                <Eye size={48} className="mx-auto mb-4 text-gray-300" />
                                <p>Select an organization to view details</p>
                            </div>
                        </Card>
                    )}
                </div>
            </div>

            {/* Create Organization Modal */}
            <Modal isOpen={showModal} onClose={() => setShowModal(false)} title="Create Organization">
                <div className="space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">Name</label>
                        <input
                            type="text"
                            className="w-full border border-gray-300 rounded-md p-2"
                            value={formData.name}
                            onChange={e => setFormData({ ...formData, name: e.target.value })}
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">Description</label>
                        <textarea
                            className="w-full border border-gray-300 rounded-md p-2"
                            rows={3}
                            value={formData.description}
                            onChange={e => setFormData({ ...formData, description: e.target.value })}
                        />
                    </div>
                    <div className="flex justify-end gap-2">
                        <Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button>
                        <Button onClick={handleCreate}>Create</Button>
                    </div>
                </div>
            </Modal>

            {/* Add Member Modal */}
            <Modal isOpen={showAddMemberModal} onClose={() => setShowAddMemberModal(false)} title="Add Member">
                <div className="space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">Select User</label>
                        <select
                            className="w-full border border-gray-300 rounded-md p-2"
                            value={selectedUserId}
                            onChange={e => setSelectedUserId(Number(e.target.value))}
                        >
                            <option value={0}>-- Select a user --</option>
                            {allUsers.filter(u => !orgMembers.find(m => m.id === u.id)).map(user => (
                                <option key={user.id} value={user.id}>{user.username}</option>
                            ))}
                        </select>
                    </div>
                    <div className="flex justify-end gap-2">
                        <Button variant="secondary" onClick={() => setShowAddMemberModal(false)}>Cancel</Button>
                        <Button onClick={handleAddMember}>Add</Button>
                    </div>
                </div>
            </Modal>

            {/* Add Admin Modal */}
            <Modal isOpen={showAddAdminModal} onClose={() => setShowAddAdminModal(false)} title="Add Administrator">
                <div className="space-y-4">
                    <div>
                        <label className="block text-sm font-medium text-gray-700 mb-1">Select User</label>
                        <select
                            className="w-full border border-gray-300 rounded-md p-2"
                            value={selectedUserId}
                            onChange={e => setSelectedUserId(Number(e.target.value))}
                        >
                            <option value={0}>-- Select a user --</option>
                            {allUsers.filter(u => !orgAdmins.find(a => a.id === u.id)).map(user => (
                                <option key={user.id} value={user.id}>{user.username}</option>
                            ))}
                        </select>
                    </div>
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
                        <label className="block text-sm font-medium text-gray-700 mb-1">Galaxy / Automation Hub Credential</label>
                        <select
                            className="w-full border border-gray-300 rounded-md p-2"
                            value={selectedGalaxyCredId}
                            onChange={e => setSelectedGalaxyCredId(Number(e.target.value))}
                        >
                            <option value={0}>-- Select a credential --</option>
                            {availableGalaxyCreds().map(c => (
                                <option key={c.id} value={c.id}>{c.name}</option>
                            ))}
                        </select>
                        {availableGalaxyCreds().length === 0 && (
                            <p className="text-xs text-gray-500 mt-2">
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
        </div>
    );
};

export default OrganizationsPage;
