import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Inventory, Host, Group } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Server, Users, Settings, Plus, Trash, Loader, Play, Activity, Clock } from 'lucide-react';

const InventoriesPage = () => {
  const [inventories, setInventories] = useState<Inventory[]>([]);
  const [selectedInventoryId, setSelectedInventoryId] = useState<number | null>(null);
  const [activeTab, setActiveTab] = useState<'hosts' | 'groups' | 'sources'>('hosts');
  const [sources, setSources] = useState<any[]>([]);
  const [showSourceModal, setShowSourceModal] = useState(false);
  const [newSource, setNewSource] = useState<{ name: string; source_kind: string; source: string; credential_id: number | '' }>({ name: '', source_kind: 'inventory', source: '', credential_id: '' });
  const [credentials, setCredentials] = useState<any[]>([]);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedHostId, setSelectedHostId] = useState<number | null>(null);
  const [hostGroups, setHostGroups] = useState<number[]>([]); // Group IDs the selected host belongs to
  const [settingRunner, setSettingRunner] = useState(false);

  // Modal states
  const [showInventoryModal, setShowInventoryModal] = useState(false);
  const [showHostModal, setShowHostModal] = useState(false);
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [showImportModal, setShowImportModal] = useState(false);
  const [newInventoryName, setNewInventoryName] = useState('');
  const [newHostName, setNewHostName] = useState('');
  const [newGroupName, setNewGroupName] = useState('');
  const [importContent, setImportContent] = useState('');
  const [importFormat, setImportFormat] = useState<'ini' | 'yaml'>('ini');


  // Load inventories
  const fetchInventories = async () => {
    try {
      setLoading(true);
      const response = await api.getInventories();
      const items = response?.items || response || [];
      setInventories(items);
      if (items.length > 0 && !selectedInventoryId) {
        setSelectedInventoryId(items[0].id);
      }
    } catch (err) {
      setError('Failed to load inventories');
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchInventories();
    api.getCredentials().then(res => setCredentials(res?.items || res || [])).catch(() => { });
  }, []);

  // Load hosts and groups when selected inventory changes
  useEffect(() => {
    if (!selectedInventoryId) return;

    const fetchHostsAndGroups = async () => {
      try {
        const [hostsData, groupsData, sourcesData] = await Promise.all([
          api.getHosts(selectedInventoryId),
          api.getGroups(selectedInventoryId),
          api.getInventorySources(selectedInventoryId).catch(() => [])
        ]);
        setHosts(hostsData || []);
        setGroups(groupsData || []);
        setSources(sourcesData || []);
      } catch (err) {
        console.error('Failed to load hosts/groups', err);
      }
    };
    fetchHostsAndGroups();
  }, [selectedInventoryId]);

  const refreshSources = () => {
    if (selectedInventoryId) api.getInventorySources(selectedInventoryId).then(d => setSources(d || [])).catch(() => {});
  };
  const refreshHosts = () => {
    if (selectedInventoryId) {
      api.getHosts(selectedInventoryId).then(d => setHosts(d || [])).catch(() => {});
      api.getGroups(selectedInventoryId).then(d => setGroups(d || [])).catch(() => {});
    }
  };
  const handleCreateSource = async () => {
    if (!selectedInventoryId || !newSource.name.trim()) return;
    const payload = {
      name: newSource.name,
      source_kind: newSource.source_kind,
      source: newSource.source,
      credential_id: newSource.credential_id === '' ? null : newSource.credential_id,
    };
    await api.createInventorySource(selectedInventoryId, payload);
    setShowSourceModal(false);
    setNewSource({ name: '', source_kind: 'inventory', source: '', credential_id: '' });
    refreshSources();
  };
  const handleSyncSource = async (sid: number) => {
    if (!selectedInventoryId) return;
    await api.syncInventorySource(selectedInventoryId, sid);
    // Sync runs async; poll the host list a few times so the UI reflects it.
    setTimeout(() => { refreshHosts(); refreshSources(); }, 4000);
    setTimeout(() => { refreshHosts(); refreshSources(); }, 9000);
  };
  const handleDeleteSource = async (sid: number) => {
    if (!selectedInventoryId) return;
    await api.deleteInventorySource(selectedInventoryId, sid);
    refreshSources();
  };

  // Load host groups when selected host changes
  useEffect(() => {
    if (!selectedHostId) {
      setHostGroups([]);
      return;
    }
    api.getHostGroups(selectedHostId)
      .then(data => setHostGroups((data || []).map((g: Group) => g.id)))
      .catch(err => console.error('Failed to load host groups', err));
  }, [selectedHostId]);

  // Create Inventory
  const handleCreateInventory = async () => {
    if (!newInventoryName.trim()) return;
    try {
      await api.createInventory({
        name: newInventoryName,
        organization_id: 1,
        kind: 'standard'
      });
      setNewInventoryName('');
      setShowInventoryModal(false);
      fetchInventories();
    } catch (err) {
      console.error('Failed to create inventory', err);
      alert('Failed to create inventory');
    }
  };

  // Delete Inventory
  const handleDeleteInventory = async (id: number) => {
    if (!confirm('Are you sure you want to delete this inventory?')) return;
    try {
      await api.deleteInventory(id);
      if (selectedInventoryId === id) {
        setSelectedInventoryId(null);
      }
      fetchInventories();
    } catch (err) {
      console.error('Failed to delete inventory', err);
    }
  };

  // Create Host
  const handleCreateHost = async () => {
    if (!newHostName.trim() || !selectedInventoryId) return;
    try {
      await api.createHost(selectedInventoryId, {
        name: newHostName,
        enabled: true
      });
      setNewHostName('');
      setShowHostModal(false);
      // Refresh hosts
      const hostsData = await api.getHosts(selectedInventoryId);
      setHosts(hostsData || []);
    } catch (err) {
      console.error('Failed to create host', err);
      alert('Failed to create host');
    }
  };

  // Handle Import
  const handleImport = async () => {
    if (!selectedInventoryId || !importContent.trim()) return;
    try {
      const result = await api.importInventory(selectedInventoryId, importContent, importFormat);
      alert(`Import complete! Created ${result.hosts_created} hosts, ${result.groups_created} groups.${result.errors?.length > 0 ? `\nErrors: ${result.errors.join(', ')}` : ''}`);
      setShowImportModal(false);
      setImportContent('');
      // Refresh hosts and groups
      const [hostsData, groupsData] = await Promise.all([
        api.getHosts(selectedInventoryId),
        api.getGroups(selectedInventoryId)
      ]);
      setHosts(hostsData || []);
      setGroups(groupsData || []);
    } catch (err) {
      console.error('Failed to import inventory', err);
      alert('Failed to import inventory');
    }
  };

  // Delete Host
  const handleDeleteHost = async (hostId: number) => {
    if (!confirm('Are you sure you want to delete this host?')) return;
    try {
      await api.deleteHost(hostId);
      if (selectedHostId === hostId) {
        setSelectedHostId(null);
      }
      if (selectedInventoryId) {
        const hostsData = await api.getHosts(selectedInventoryId);
        setHosts(hostsData || []);
      }
    } catch (err) {
      console.error('Failed to delete host', err);
    }
  };

  // Create Group
  const handleCreateGroup = async () => {
    if (!newGroupName.trim() || !selectedInventoryId) return;
    try {
      await api.createGroup(selectedInventoryId, { name: newGroupName });
      setNewGroupName('');
      setShowGroupModal(false);
      const groupsData = await api.getGroups(selectedInventoryId);
      setGroups(groupsData || []);
    } catch (err) {
      console.error('Failed to create group', err);
      alert('Failed to create group');
    }
  };

  // Set Runner Host
  const handleSetRunner = async (hostId: number) => {
    if (!selectedInventoryId) return;
    setSettingRunner(true);
    try {
      await api.setRunnerHost(hostId);
      // Refresh hosts list to update runner badges
      const hostsData = await api.getHosts(selectedInventoryId);
      setHosts(hostsData || []);
      // Keep the same host selected to show the badge update
      setSelectedHostId(hostId);
    } catch (err) {
      console.error('Failed to set runner host', err);
      alert('Failed to set runner host');
    } finally {
      setSettingRunner(false);
    }
  };

  const selectedHost = hosts.find(h => h.id === selectedHostId);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader className="animate-spin text-brand-600" size={32} />
      </div>
    );
  }

  if (error) {
    return <div className="text-red-600 p-4">{error}</div>;
  }

  return (
    <div className="h-[calc(100vh-8rem)] flex flex-col">
      <h1 className="text-2xl font-bold text-gray-900 mb-6">Inventories</h1>

      <div className="flex flex-1 gap-6 overflow-hidden">
        {/* Left Sidebar: Inventory List */}
        <Card className="w-64 flex flex-col overflow-hidden">
          <div className="p-4 border-b border-gray-100 bg-gray-50">
            <h3 className="font-semibold text-gray-700">Inventories</h3>
          </div>
          <div className="flex-1 overflow-y-auto p-2 space-y-1">
            {inventories.map(inv => (
              <div
                key={inv.id}
                className={`flex items-center justify-between px-3 py-2 rounded-md text-sm transition-colors cursor-pointer ${selectedInventoryId === inv.id
                  ? 'bg-brand-50 text-brand-700 font-medium'
                  : 'text-gray-600 hover:bg-gray-50'
                  }`}
                onClick={() => { setSelectedInventoryId(inv.id); setSelectedHostId(null); }}
              >
                <span>{inv.name}</span>
                <button
                  onClick={(e) => { e.stopPropagation(); handleDeleteInventory(inv.id); }}
                  className="text-gray-400 hover:text-red-600"
                >
                  <Trash size={14} />
                </button>
              </div>
            ))}
            {inventories.length === 0 && <p className="p-4 text-sm text-gray-500">No inventories found.</p>}
          </div>
          <div className="p-2 border-t border-gray-100 space-y-1">
            <Button size="sm" variant="ghost" className="w-full justify-start" icon={<Plus size={14} />} onClick={() => setShowInventoryModal(true)}>New Inventory</Button>
            {selectedInventoryId && (
              <Button size="sm" variant="ghost" className="w-full justify-start" icon={<Server size={14} />} onClick={() => setShowImportModal(true)}>Import Hosts</Button>
            )}
          </div>
        </Card>

        {/* Middle Pane: Hosts/Groups List */}
        <Card className="w-80 flex flex-col overflow-hidden">
          <div className="flex border-b border-gray-200">
            <button
              className={`flex-1 py-3 text-sm font-medium border-b-2 ${activeTab === 'hosts' ? 'border-brand-500 text-brand-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}
              onClick={() => setActiveTab('hosts')}
            >
              Hosts ({hosts.length})
            </button>
            <button
              className={`flex-1 py-3 text-sm font-medium border-b-2 ${activeTab === 'groups' ? 'border-brand-500 text-brand-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}
              onClick={() => setActiveTab('groups')}
            >
              Groups ({groups.length})
            </button>
            <button
              className={`flex-1 py-3 text-sm font-medium border-b-2 ${activeTab === 'sources' ? 'border-brand-500 text-brand-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}
              onClick={() => setActiveTab('sources')}
            >
              Sources ({sources.length})
            </button>
          </div>

          <div className="flex-1 overflow-y-auto">
            {activeTab === 'sources' ? (
              <ul className="divide-y divide-gray-100">
                {sources.map(s => (
                  <li key={s.id} className="p-4 hover:bg-gray-50">
                    <div className="flex justify-between items-center">
                      <div>
                        <span className="text-sm font-medium text-gray-900">{s.name}</span>
                        <span className="ml-2 text-xs text-gray-400">{s.source_kind}</span>
                      </div>
                      <div className="flex items-center gap-2">
                        <button onClick={() => handleSyncSource(s.id)} className="text-brand-600 hover:text-brand-700" title="Sync now">
                          <Play size={14} />
                        </button>
                        <button onClick={() => handleDeleteSource(s.id)} className="text-gray-400 hover:text-red-600" title="Delete">
                          <Trash size={14} />
                        </button>
                      </div>
                    </div>
                    {s.last_synced_at && (
                      <p className="mt-1 text-xs text-gray-400">Last synced {new Date(s.last_synced_at).toLocaleString()}</p>
                    )}
                  </li>
                ))}
                {sources.length === 0 && <p className="p-4 text-sm text-gray-500">No sources. Add one to populate this inventory dynamically.</p>}
              </ul>
            ) : activeTab === 'hosts' ? (
              <ul className="divide-y divide-gray-100">
                {hosts.map(host => (
                  <li
                    key={host.id}
                    className={`p-4 cursor-pointer hover:bg-gray-50 ${selectedHostId === host.id ? 'bg-brand-50' : ''}`}
                    onClick={() => setSelectedHostId(host.id)}
                  >
                    <div className="flex justify-between items-start">
                      <div className="flex items-center gap-2">
                        <div className={`w-2 h-2 rounded-full ${host.enabled ? 'bg-green-500' : 'bg-gray-300'}`} />
                        <span className="text-sm font-medium text-gray-900">{host.name}</span>
                        {host.is_runner_host && (
                          <span className="px-1.5 py-0.5 text-xs font-medium bg-purple-100 text-purple-700 rounded">
                            Runner
                          </span>
                        )}
                      </div>
                      <button
                        onClick={(e) => { e.stopPropagation(); handleDeleteHost(host.id); }}
                        className="text-gray-400 hover:text-red-600"
                      >
                        <Trash size={14} />
                      </button>
                    </div>
                    {host.is_runner_host && (
                      <div className="mt-2 ml-4 text-xs">
                        {host.runner_healthy ? (
                          <span className="flex items-center text-green-600">
                            <Activity className="h-3 w-3 mr-1" /> Agent healthy
                          </span>
                        ) : host.runner_last_seen ? (
                          <span className="flex items-center text-yellow-600">
                            <Clock className="h-3 w-3 mr-1" /> Agent stale
                          </span>
                        ) : (
                          <span className="text-gray-400">Agent not installed</span>
                        )}
                      </div>
                    )}
                    {!host.is_runner_host && (
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          handleSetRunner(host.id);
                        }}
                        className="mt-1 ml-4 text-xs text-brand-600 hover:text-brand-700 flex items-center gap-1"
                        disabled={settingRunner}
                      >
                        <Play size={10} /> Set as runner
                      </button>
                    )}
                  </li>
                ))}
                {hosts.length === 0 && <p className="p-4 text-sm text-gray-500">No hosts found.</p>}
              </ul>
            ) : (
              <ul className="divide-y divide-gray-100">
                {groups.map(group => (
                  <li key={group.id} className="p-4 hover:bg-gray-50 flex justify-between items-center">
                    <div className="flex items-center gap-2">
                      <Users size={16} className="text-gray-400" />
                      <span className="text-sm font-medium text-gray-900">{group.name}</span>
                    </div>
                  </li>
                ))}
                {groups.length === 0 && <p className="p-4 text-sm text-gray-500">No groups found.</p>}
              </ul>
            )}
          </div>
          <div className="p-3 border-t border-gray-100 bg-gray-50">
            <Button
              size="sm"
              className="w-full"
              icon={<Plus size={14} />}
              onClick={() => activeTab === 'hosts' ? setShowHostModal(true) : activeTab === 'groups' ? setShowGroupModal(true) : setShowSourceModal(true)}
              disabled={!selectedInventoryId}
            >
              Add {activeTab === 'hosts' ? 'Host' : activeTab === 'groups' ? 'Group' : 'Source'}
            </Button>
          </div>
        </Card>

        {/* Right Pane: Details */}
        <div className="flex-1 flex flex-col bg-white rounded-lg border border-gray-200 shadow-sm overflow-hidden">
          {selectedHost ? (
            <>
              <div className="p-6 border-b border-gray-100 flex justify-between items-start bg-gray-50">
                <div>
                  <div className="flex items-center gap-2">
                    <h2 className="text-xl font-bold text-gray-900">{selectedHost.name}</h2>
                    {selectedHost.is_runner_host && (
                      <span className="px-2 py-0.5 text-xs font-semibold bg-purple-100 text-purple-700 rounded-full">
                        Runner
                      </span>
                    )}
                  </div>
                  <p className="text-sm text-gray-500 mt-1">Inventory ID: {selectedHost.inventory_id}</p>
                </div>
                <div className="flex gap-2">
                  {!selectedHost.is_runner_host && (
                    <Button
                      size="sm"
                      variant="secondary"
                      icon={settingRunner ? <Loader size={14} className="animate-spin" /> : <Play size={14} />}
                      onClick={() => handleSetRunner(selectedHost.id)}
                      disabled={settingRunner}
                    >
                      {settingRunner ? 'Setting...' : 'Set as Runner'}
                    </Button>
                  )}
                  <Button size="sm" variant="danger" icon={<Trash size={14} />} onClick={() => handleDeleteHost(selectedHost.id)}>Delete</Button>
                </div>
              </div>

              <div className="p-6 overflow-y-auto space-y-6">
                <div>
                  <label className="flex items-center gap-2 text-sm font-medium text-gray-700 mb-2">
                    <input type="checkbox" checked={selectedHost.enabled} className="rounded text-brand-600 focus:ring-brand-500" readOnly />
                    Enabled
                  </label>
                  <p className="text-xs text-gray-500">Disabled hosts are skipped during job execution.</p>
                </div>

                {/* Groups Section */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">
                    <Users size={14} className="inline mr-1" /> Groups
                  </label>
                  {groups.length === 0 ? (
                    <p className="text-xs text-gray-400">No groups in this inventory. Create a group first.</p>
                  ) : (
                    <div className="space-y-2 bg-gray-50 rounded-md p-3 border border-gray-200">
                      {groups.map(group => {
                        const isInGroup = hostGroups.includes(group.id);
                        return (
                          <label key={group.id} className="flex items-center gap-2 cursor-pointer hover:bg-gray-100 p-1 rounded">
                            <input
                              type="checkbox"
                              checked={isInGroup}
                              className="rounded text-brand-600 focus:ring-brand-500"
                              onChange={async () => {
                                try {
                                  if (isInGroup) {
                                    await api.removeHostFromGroup(group.id, selectedHost.id);
                                    setHostGroups(prev => prev.filter(id => id !== group.id));
                                  } else {
                                    await api.addHostToGroup(group.id, selectedHost.id);
                                    setHostGroups(prev => [...prev, group.id]);
                                  }
                                } catch (err) {
                                  console.error('Failed to update group membership', err);
                                }
                              }}
                            />
                            <span className="text-sm text-gray-700">{group.name}</span>
                          </label>
                        );
                      })}
                    </div>
                  )}
                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">Variables (JSON)</label>
                  <textarea
                    className="w-full h-64 font-mono text-sm bg-slate-50 border border-gray-300 rounded-md p-3 focus:ring-brand-500 focus:border-brand-500"
                    defaultValue={typeof selectedHost.variables === 'string' ? selectedHost.variables : JSON.stringify(selectedHost.variables, null, 2)}
                  />
                </div>
              </div>
            </>
          ) : (
            <div className="flex-1 flex flex-col items-center justify-center text-gray-400 p-8">
              <Settings size={48} className="mb-4 opacity-20" />
              <p>Select a host to view details</p>
            </div>
          )}
        </div>
      </div>

      {/* New Inventory Modal */}
      <Modal isOpen={showInventoryModal} onClose={() => setShowInventoryModal(false)} title="New Inventory">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Name</label>
            <input
              type="text"
              className="w-full border border-gray-300 rounded-md p-2 focus:ring-brand-500 focus:border-brand-500"
              value={newInventoryName}
              onChange={(e) => setNewInventoryName(e.target.value)}
              placeholder="My Inventory"
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowInventoryModal(false)}>Cancel</Button>
            <Button onClick={handleCreateInventory}>Create</Button>
          </div>
        </div>
      </Modal>

      {/* New Host Modal */}
      <Modal isOpen={showHostModal} onClose={() => setShowHostModal(false)} title="New Host">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Hostname</label>
            <input
              type="text"
              className="w-full border border-gray-300 rounded-md p-2 focus:ring-brand-500 focus:border-brand-500"
              value={newHostName}
              onChange={(e) => setNewHostName(e.target.value)}
              placeholder="web-server-01"
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowHostModal(false)}>Cancel</Button>
            <Button onClick={handleCreateHost}>Create</Button>
          </div>
        </div>
      </Modal>

      {/* New Group Modal */}
      <Modal isOpen={showGroupModal} onClose={() => setShowGroupModal(false)} title="New Group">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Group Name</label>
            <input
              type="text"
              className="w-full border border-gray-300 rounded-md p-2 focus:ring-brand-500 focus:border-brand-500"
              value={newGroupName}
              onChange={(e) => setNewGroupName(e.target.value)}
              placeholder="webservers"
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowGroupModal(false)}>Cancel</Button>
            <Button onClick={handleCreateGroup}>Create</Button>
          </div>
        </div>
      </Modal>

      {/* Import Inventory Modal */}
      <Modal isOpen={showImportModal} onClose={() => setShowImportModal(false)} title="Import Inventory">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Format</label>
            <select
              className="w-full border border-gray-300 rounded-md p-2 focus:ring-brand-500 focus:border-brand-500"
              value={importFormat}
              onChange={(e) => setImportFormat(e.target.value as 'ini' | 'yaml')}
            >
              <option value="ini">INI (Ansible format)</option>
              <option value="yaml">YAML</option>
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Inventory Content
            </label>
            <textarea
              className="w-full h-64 font-mono text-sm border border-gray-300 rounded-md p-3 focus:ring-brand-500 focus:border-brand-500"
              placeholder={importFormat === 'ini' ? `# INI format example
[webservers]
web1.example.com
web2.example.com

[databases]
db1.example.com` : `# YAML format example
all:
  children:
    webservers:
      hosts:
        web1.example.com:
        web2.example.com:`}
              value={importContent}
              onChange={(e) => setImportContent(e.target.value)}
            />
          </div>
          <p className="text-xs text-gray-500">
            Paste your Ansible inventory file content. Hosts and groups will be created automatically.
          </p>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowImportModal(false)}>Cancel</Button>
            <Button onClick={handleImport} disabled={!importContent.trim()}>Import</Button>
          </div>
        </div>
      </Modal>

      <Modal isOpen={showSourceModal} onClose={() => setShowSourceModal(false)} title="New Inventory Source">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700">Name</label>
            <input type="text" className="mt-1 block w-full rounded-md border-gray-300 border p-2"
              value={newSource.name} onChange={e => setNewSource({ ...newSource, name: e.target.value })} />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">Kind</label>
            <select className="mt-1 block w-full rounded-md border-gray-300 border p-2"
              value={newSource.source_kind} onChange={e => setNewSource({ ...newSource, source_kind: e.target.value })}>
              <option value="inventory">Inventory / plugin (YAML)</option>
              <option value="script">Script (executable)</option>
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">Credential <span className="text-gray-400 font-normal">(optional)</span></label>
            <p className="text-xs text-gray-400 mb-1">A cloud credential (e.g. AWS) whose injectors set the env vars / files the inventory plugin needs to authenticate.</p>
            <select className="mt-1 block w-full rounded-md border-gray-300 border p-2"
              value={newSource.credential_id} onChange={e => setNewSource({ ...newSource, credential_id: e.target.value === '' ? '' : Number(e.target.value) })}>
              <option value="">None</option>
              {credentials.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">Source</label>
            <p className="text-xs text-gray-400 mb-1">A YAML inventory/plugin config, or a script that outputs Ansible inventory JSON. `ansible-inventory --list` is run against it on sync.</p>
            <textarea rows={8} className="mt-1 block w-full rounded-md border-gray-300 border p-2 font-mono text-xs"
              value={newSource.source} onChange={e => setNewSource({ ...newSource, source: e.target.value })} />
          </div>
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={() => setShowSourceModal(false)}>Cancel</Button>
            <Button onClick={handleCreateSource}>Create</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default InventoriesPage;