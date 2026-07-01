import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import { Inventory, Host, Group } from '../types';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import ResourceAccess from '../components/ResourceAccess';
import { splitConnection, mergeConnection, emptyConnection, HostConnection } from '../lib/hostConnection';
import { Server, Users, Plus, Trash, Loader, Play, Activity, Clock, Plug, Save, ChevronDown, ChevronRight } from 'lucide-react';

const InventoriesPage = () => {
  const [inventories, setInventories] = useState<Inventory[]>([]);
  const [selectedInventoryId, setSelectedInventoryId] = useState<number | null>(null);
  const [activeTab, setActiveTab] = useState<'hosts' | 'groups' | 'sources' | 'access'>('hosts');
  const [sources, setSources] = useState<any[]>([]);
  const [showSourceModal, setShowSourceModal] = useState(false);
  const [newSource, setNewSource] = useState<{ name: string; source_kind: string; source: string; credential_id: number | '' }>({ name: '', source_kind: 'inventory', source: '', credential_id: '' });
  const [credentials, setCredentials] = useState<any[]>([]);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedHostId, setSelectedHostId] = useState<number | null>(null);
  const [showHostDetail, setShowHostDetail] = useState(false);
  const [hostGroups, setHostGroups] = useState<number[]>([]); // Group IDs the selected host belongs to
  const [settingRunner, setSettingRunner] = useState(false);
  // Editable SSH connection for the selected host + any non-connection vars we
  // preserve verbatim, and a save state.
  const [connForm, setConnForm] = useState<HostConnection>(emptyConnection());
  const [extraVars, setExtraVars] = useState<Record<string, any>>({});
  const [showExtraVars, setShowExtraVars] = useState(false);
  const [savingHost, setSavingHost] = useState(false);
  // Connection fields for the New Host modal.
  const [newHostConn, setNewHostConn] = useState<HostConnection>(emptyConnection());

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

  // Populate the connection form from the selected host's variables.
  useEffect(() => {
    const host = hosts.find(h => h.id === selectedHostId);
    if (!host) return;
    const { conn, extra } = splitConnection(host.variables);
    setConnForm(conn);
    setExtraVars(extra);
    setShowExtraVars(false);
  }, [selectedHostId, hosts]);

  // Save the connection form back into the host's variables (extras preserved).
  const handleSaveConnection = async () => {
    const host = hosts.find(h => h.id === selectedHostId);
    if (!host) return;
    setSavingHost(true);
    try {
      const variables = mergeConnection(connForm, extraVars);
      const updated = await api.updateHost(host.id, { variables });
      setHosts(prev => prev.map(h => (h.id === host.id ? updated : h)));
    } catch (err) {
      console.error('Failed to save host connection', err);
      alert('Failed to save connection. You need admin on this inventory.');
    } finally {
      setSavingHost(false);
    }
  };

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
        enabled: true,
        variables: mergeConnection(newHostConn, {}),
      });
      setNewHostName('');
      setNewHostConn(emptyConnection());
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
  const selectedInv = inventories.find(i => i.id === selectedInventoryId);
  const openHost = (id: number) => { setSelectedHostId(id); setShowHostDetail(true); };
  const addLabel = activeTab === 'hosts' ? 'Host' : activeTab === 'groups' ? 'Group' : 'Source';
  const onAdd = () => activeTab === 'hosts' ? setShowHostModal(true) : activeTab === 'groups' ? setShowGroupModal(true) : setShowSourceModal(true);

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
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-gray-900">Inventories</h1>
        <Button icon={<Plus size={16} />} onClick={() => setShowInventoryModal(true)}>New Inventory</Button>
      </div>

      <div className="flex gap-6 items-start">
        {/* Left: inventory list */}
        <Card className="w-72 shrink-0 overflow-hidden">
          <div className="px-4 py-3 border-b border-gray-100 bg-gray-50 flex items-center justify-between">
            <h3 className="text-sm font-semibold text-gray-700">All inventories</h3>
            <span className="text-xs text-gray-400">{inventories.length}</span>
          </div>
          <div className="p-2 space-y-0.5 max-h-[72vh] overflow-y-auto">
            {inventories.map(inv => (
              <div
                key={inv.id}
                onClick={() => { setSelectedInventoryId(inv.id); setSelectedHostId(null); }}
                className={`group flex items-center justify-between gap-2 px-3 py-2 rounded-md cursor-pointer ${selectedInventoryId === inv.id ? 'bg-brand-50' : 'hover:bg-gray-50'}`}
              >
                <div className="flex items-center gap-2 min-w-0">
                  <Server size={15} className={`shrink-0 ${selectedInventoryId === inv.id ? 'text-brand-600' : 'text-gray-400'}`} />
                  <span className={`text-sm truncate ${selectedInventoryId === inv.id ? 'text-brand-700 font-medium' : 'text-gray-700'}`} title={inv.name}>{inv.name}</span>
                </div>
                <button onClick={(e) => { e.stopPropagation(); handleDeleteInventory(inv.id); }} className="shrink-0 text-gray-300 group-hover:text-gray-400 hover:!text-red-600" title="Delete inventory">
                  <Trash size={14} />
                </button>
              </div>
            ))}
            {inventories.length === 0 && <p className="px-3 py-6 text-sm text-gray-400 text-center">No inventories yet.</p>}
          </div>
        </Card>

        {/* Right: selected inventory detail */}
        {selectedInv ? (
          <div className="flex-1 min-w-0 space-y-4">
            {/* Header + summary */}
            <Card>
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                  <h2 className="text-xl font-bold text-gray-900 truncate" title={selectedInv.name}>{selectedInv.name}</h2>
                  <div className="flex flex-wrap gap-x-5 gap-y-1 mt-2 text-sm text-gray-500">
                    <span><b className="text-gray-900">{hosts.length}</b> hosts</span>
                    <span><b className="text-gray-900">{groups.length}</b> groups</span>
                    <span><b className="text-gray-900">{sources.length}</b> sources</span>
                  </div>
                </div>
                <Button variant="secondary" icon={<Server size={16} />} onClick={() => setShowImportModal(true)}>Import</Button>
              </div>
            </Card>

            {/* Tabs + content */}
            <Card className="overflow-hidden">
              <div className="flex items-center justify-between border-b border-gray-200 px-2">
                <div className="flex">
                  {(['hosts', 'groups', 'sources', 'access'] as const).map(t => (
                    <button key={t} onClick={() => setActiveTab(t)}
                      className={`px-4 py-3 text-sm font-medium border-b-2 capitalize ${activeTab === t ? 'border-brand-500 text-brand-600' : 'border-transparent text-gray-500 hover:text-gray-700'}`}>
                      {t}{t !== 'access' && <span className="text-gray-400"> ({t === 'hosts' ? hosts.length : t === 'groups' ? groups.length : sources.length})</span>}
                    </button>
                  ))}
                </div>
                {activeTab !== 'access' && <Button size="sm" icon={<Plus size={14} />} onClick={onAdd}>Add {addLabel}</Button>}
              </div>

              {/* Access */}
              {activeTab === 'access' && (
                <div className="p-4">
                  <ResourceAccess contentType="inventory" objectId={selectedInv.id} />
                </div>
              )}

              {/* Hosts */}
              {activeTab === 'hosts' && (
                <table className="min-w-full divide-y divide-gray-100">
                  <tbody className="divide-y divide-gray-50">
                    {hosts.map(host => (
                      <tr key={host.id} className="hover:bg-gray-50 cursor-pointer" onClick={() => openHost(host.id)}>
                        <td className="px-4 py-2.5">
                          <div className="flex items-center gap-2">
                            <span className={`w-2 h-2 rounded-full shrink-0 ${host.enabled ? 'bg-green-500' : 'bg-gray-300'}`} />
                            <span className="text-sm font-medium text-gray-900 truncate" title={host.name}>{host.name}</span>
                          </div>
                        </td>
                        <td className="px-4 py-2.5 w-56">
                          {host.is_runner_host ? (
                            <span className="inline-flex items-center gap-2">
                              <span className="px-2 py-0.5 text-xs font-medium bg-purple-100 text-purple-700 rounded-full">Runner</span>
                              {host.runner_healthy ? (
                                <span title="Runner agent is healthy (recent heartbeat)" className="inline-flex items-center gap-1 text-xs text-green-600"><Activity size={12} /> healthy</span>
                              ) : host.runner_last_seen ? (
                                <span title="Runner agent is stale (no recent heartbeat)" className="inline-flex items-center gap-1 text-xs text-yellow-600"><Clock size={12} /> stale</span>
                              ) : (
                                <span title="Runner agent not installed yet" className="text-xs text-gray-400">no agent</span>
                              )}
                            </span>
                          ) : (
                            <span className="text-xs text-gray-400">{host.enabled ? 'enabled' : 'disabled'}</span>
                          )}
                        </td>
                        <td className="px-4 py-2.5 w-44 text-right whitespace-nowrap" onClick={e => e.stopPropagation()}>
                          {!host.is_runner_host && (
                            <Button variant="ghost" size="sm" icon={<Play size={13} />} disabled={settingRunner} onClick={() => handleSetRunner(host.id)}>Runner</Button>
                          )}
                          <Button variant="ghost" size="sm" icon={<Trash size={14} />} onClick={() => handleDeleteHost(host.id)} />
                        </td>
                      </tr>
                    ))}
                    {hosts.length === 0 && <tr><td className="px-4 py-8 text-center text-sm text-gray-400">No hosts. Add one, import, or sync a source.</td></tr>}
                  </tbody>
                </table>
              )}

              {/* Groups */}
              {activeTab === 'groups' && (
                <table className="min-w-full divide-y divide-gray-100">
                  <tbody className="divide-y divide-gray-50">
                    {groups.map(group => (
                      <tr key={group.id} className="hover:bg-gray-50">
                        <td className="px-4 py-2.5">
                          <div className="flex items-center gap-2">
                            <Users size={15} className="text-gray-400" />
                            <span className="text-sm font-medium text-gray-900">{group.name}</span>
                          </div>
                        </td>
                      </tr>
                    ))}
                    {groups.length === 0 && <tr><td className="px-4 py-8 text-center text-sm text-gray-400">No groups yet.</td></tr>}
                  </tbody>
                </table>
              )}

              {/* Sources */}
              {activeTab === 'sources' && (
                <table className="min-w-full divide-y divide-gray-100">
                  <thead className="bg-gray-50"><tr>
                    <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Source</th>
                    <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Kind</th>
                    <th className="px-4 py-2 text-left text-xs font-medium text-gray-500 uppercase">Last synced</th>
                    <th className="px-4 py-2 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
                  </tr></thead>
                  <tbody className="divide-y divide-gray-50">
                    {sources.map(s => (
                      <tr key={s.id} className="hover:bg-gray-50">
                        <td className="px-4 py-2.5 text-sm font-medium text-gray-900">
                          {s.name}{s.credential_id ? <span className="ml-2 text-xs text-gray-400">🔑</span> : null}
                        </td>
                        <td className="px-4 py-2.5 text-sm text-gray-500">{s.source_kind}</td>
                        <td className="px-4 py-2.5 text-sm text-gray-500">{s.last_synced_at ? new Date(s.last_synced_at).toLocaleString() : 'never'}</td>
                        <td className="px-4 py-2.5 text-right whitespace-nowrap">
                          <Button variant="ghost" size="sm" icon={<Play size={13} />} onClick={() => handleSyncSource(s.id)}>Sync</Button>
                          <Button variant="ghost" size="sm" icon={<Trash size={14} />} onClick={() => handleDeleteSource(s.id)} />
                        </td>
                      </tr>
                    ))}
                    {sources.length === 0 && <tr><td colSpan={4} className="px-4 py-8 text-center text-sm text-gray-400">No sources. Add one to populate this inventory dynamically (e.g. AWS).</td></tr>}
                  </tbody>
                </table>
              )}
            </Card>
          </div>
        ) : (
          <Card className="flex-1">
            <div className="flex flex-col items-center justify-center text-gray-400 py-20">
              <Server size={40} className="mb-3 opacity-20" />
              <p className="text-sm">Select an inventory to view its hosts, groups and sources.</p>
            </div>
          </Card>
        )}
      </div>

      {/* Host detail modal */}
      <Modal isOpen={showHostDetail && !!selectedHost} onClose={() => setShowHostDetail(false)} title={selectedHost ? selectedHost.name : ''} size="lg">
        {selectedHost && (
          <div className="space-y-5">
            <div className="flex items-center gap-2">
              <span className={`w-2.5 h-2.5 rounded-full ${selectedHost.enabled ? 'bg-green-500' : 'bg-gray-300'}`} />
              <span className="text-sm text-gray-600">{selectedHost.enabled ? 'Enabled' : 'Disabled'}</span>
              {selectedHost.is_runner_host && <span className="px-2 py-0.5 text-xs font-semibold bg-purple-100 text-purple-700 rounded-full">Runner</span>}
              <div className="ml-auto flex gap-2">
                {!selectedHost.is_runner_host && (
                  <Button size="sm" variant="secondary" icon={settingRunner ? <Loader size={14} className="animate-spin" /> : <Play size={14} />} disabled={settingRunner} onClick={() => handleSetRunner(selectedHost.id)}>Set as runner</Button>
                )}
                <Button size="sm" variant="danger" icon={<Trash size={14} />} onClick={() => { handleDeleteHost(selectedHost.id); setShowHostDetail(false); }}>Delete</Button>
              </div>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2"><Users size={14} className="inline mr-1" /> Group membership</label>
              {groups.length === 0 ? (
                <p className="text-xs text-gray-400">No groups in this inventory.</p>
              ) : (
                <div className="grid grid-cols-2 gap-1 bg-gray-50 rounded-md p-3 border border-gray-200">
                  {groups.map(group => {
                    const isInGroup = hostGroups.includes(group.id);
                    return (
                      <label key={group.id} className="flex items-center gap-2 cursor-pointer hover:bg-gray-100 p-1 rounded">
                        <input type="checkbox" checked={isInGroup} className="rounded text-brand-600 focus:ring-brand-500"
                          onChange={async () => {
                            try {
                              if (isInGroup) { await api.removeHostFromGroup(group.id, selectedHost.id); setHostGroups(prev => prev.filter(id => id !== group.id)); }
                              else { await api.addHostToGroup(group.id, selectedHost.id); setHostGroups(prev => [...prev, group.id]); }
                            } catch (err) { console.error('Failed to update group membership', err); }
                          }} />
                        <span className="text-sm text-gray-700 truncate">{group.name}</span>
                      </label>
                    );
                  })}
                </div>
              )}
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2"><Plug size={14} className="inline mr-1" /> Connection</label>
              <div className="bg-gray-50 border border-gray-200 rounded-md p-3 space-y-3">
                <div className="grid grid-cols-2 gap-3">
                  <div className="col-span-2 sm:col-span-1">
                    <label className="block text-xs font-medium text-gray-500 mb-1">Address <span className="text-gray-400">(ansible_host)</span></label>
                    <input className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm font-mono" placeholder={selectedHost.name}
                      value={connForm.ansible_host} onChange={e => setConnForm({ ...connForm, ansible_host: e.target.value })} />
                    <p className="text-[11px] text-gray-400 mt-0.5">IP or DNS name. Defaults to the hostname.</p>
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-gray-500 mb-1">Port <span className="text-gray-400">(ansible_port)</span></label>
                    <input className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm font-mono" placeholder="22" inputMode="numeric"
                      value={connForm.ansible_port} onChange={e => setConnForm({ ...connForm, ansible_port: e.target.value })} />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-gray-500 mb-1">User <span className="text-gray-400">(ansible_user)</span></label>
                    <input className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm font-mono" placeholder="root"
                      value={connForm.ansible_user} onChange={e => setConnForm({ ...connForm, ansible_user: e.target.value })} />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-gray-500 mb-1">Connection <span className="text-gray-400">(ansible_connection)</span></label>
                    <select className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm"
                      value={connForm.ansible_connection} onChange={e => setConnForm({ ...connForm, ansible_connection: e.target.value })}>
                      <option value="">ssh (default)</option>
                      <option value="ssh">ssh</option>
                      <option value="local">local</option>
                      <option value="paramiko">paramiko</option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-gray-500 mb-1">Python <span className="text-gray-400">(interpreter)</span></label>
                    <input className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm font-mono" placeholder="/usr/bin/python3"
                      value={connForm.ansible_python_interpreter} onChange={e => setConnForm({ ...connForm, ansible_python_interpreter: e.target.value })} />
                  </div>
                </div>
                <div className="flex justify-end">
                  <Button size="sm" icon={savingHost ? <Loader size={14} className="animate-spin" /> : <Save size={14} />} disabled={savingHost} onClick={handleSaveConnection}>Save connection</Button>
                </div>
              </div>
            </div>

            {Object.keys(extraVars).length > 0 && (
              <div>
                <button onClick={() => setShowExtraVars(v => !v)} className="flex items-center gap-1 text-sm font-medium text-gray-700">
                  {showExtraVars ? <ChevronDown size={14} /> : <ChevronRight size={14} />} Other variables ({Object.keys(extraVars).length})
                </button>
                {showExtraVars && (
                  <pre className="mt-2 w-full max-h-56 overflow-auto font-mono text-xs bg-slate-50 border border-gray-200 rounded-md p-3 text-gray-700">
                    {JSON.stringify(extraVars, null, 2)}
                  </pre>
                )}
              </div>
            )}
          </div>
        )}
      </Modal>

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
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-2"><Plug size={14} className="inline mr-1" /> Connection <span className="text-gray-400 font-normal">(optional)</span></label>
            <div className="grid grid-cols-2 gap-3 bg-gray-50 border border-gray-200 rounded-md p-3">
              <div className="col-span-2 sm:col-span-1">
                <label className="block text-xs font-medium text-gray-500 mb-1">Address</label>
                <input className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm font-mono" placeholder={newHostName || 'ansible_host'}
                  value={newHostConn.ansible_host} onChange={e => setNewHostConn({ ...newHostConn, ansible_host: e.target.value })} />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-500 mb-1">Port</label>
                <input className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm font-mono" placeholder="22" inputMode="numeric"
                  value={newHostConn.ansible_port} onChange={e => setNewHostConn({ ...newHostConn, ansible_port: e.target.value })} />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-500 mb-1">User</label>
                <input className="w-full border border-gray-300 rounded-md px-2 py-1.5 text-sm font-mono" placeholder="root"
                  value={newHostConn.ansible_user} onChange={e => setNewHostConn({ ...newHostConn, ansible_user: e.target.value })} />
              </div>
            </div>
            <p className="text-[11px] text-gray-400 mt-1">Leave address blank to connect by hostname. You can edit these later on the host.</p>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => { setShowHostModal(false); setNewHostConn(emptyConnection()); }}>Cancel</Button>
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