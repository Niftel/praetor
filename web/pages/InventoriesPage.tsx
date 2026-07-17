import React, { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Inventory, Host, Group } from '../types';
import { Input, Textarea, Select } from '../components/ui/Input';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import ResourceAccess from '../components/ResourceAccess';
import { splitConnection, mergeConnection, emptyConnection, HostConnection } from '../lib/hostConnection';
import {
  Plus, Trash2, Loader, Search, ChevronDown, ChevronRight, ArrowLeft,
  Server, RefreshCw, Radio, Check, MoreHorizontal, Upload, Shield,
} from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';
import { useCapabilities } from '../lib/useCapabilities';

// Coerce an edited string back toward its JSON-native type (number / bool /
// object) so round-tripping a var through the editor doesn't stringify it.
const coerce = (v: string): any => {
  const t = v.trim();
  if (t === '') return '';
  if (t === 'true') return true;
  if (t === 'false') return false;
  if (/^-?\d+(\.\d+)?$/.test(t)) return Number(t);
  if ((t[0] === '{' && t.endsWith('}')) || (t[0] === '[' && t.endsWith(']'))) {
    try { return JSON.parse(t); } catch { /* fall through */ }
  }
  return v;
};
const showVal = (v: any): string => (typeof v === 'object' && v !== null ? JSON.stringify(v) : String(v));

const InventoriesPage = () => {
  const { orgId: orgIdStr } = useParams();
  const orgId = Number(orgIdStr);
  const [orgName, setOrgName] = useState('');
  const [inventories, setInventories] = useState<Inventory[]>([]);
  const [selectedInventoryId, setSelectedInventoryId] = useState<number | null>(null);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [groupHosts, setGroupHosts] = useState<Record<number, number[]>>({});
  const [sources, setSources] = useState<any[]>([]);
  const [credentials, setCredentials] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const { capabilities: orgCapabilities, loading: orgCapabilitiesLoading } = useCapabilities('organization', orgId);

  const [selectedHostId, setSelectedHostId] = useState<number | null>(null);
  const [hostGroups, setHostGroups] = useState<number[]>([]);
  const [settingRunner, setSettingRunner] = useState(false);
  const [savingHost, setSavingHost] = useState(false);
  const [connForm, setConnForm] = useState<HostConnection>(emptyConnection());
  const [extraVars, setExtraVars] = useState<Record<string, string>>({});
  const originalRef = useRef<string>('');

  const [treeFilter, setTreeFilter] = useState('');
  const [collapsed, setCollapsed] = useState<Set<number | 'ungrouped'>>(new Set());
  const [invMenu, setInvMenu] = useState(false);
  const [addMenu, setAddMenu] = useState(false);

  // Modals
  const [showInventoryModal, setShowInventoryModal] = useState(false);
  const [showHostModal, setShowHostModal] = useState(false);
  const [showGroupModal, setShowGroupModal] = useState(false);
  const [showImportModal, setShowImportModal] = useState(false);
  const [showSourceModal, setShowSourceModal] = useState(false);
  const [showVarsModal, setShowVarsModal] = useState(false);
  const [varsDraft, setVarsDraft] = useState('');
  const [newInventoryName, setNewInventoryName] = useState('');
  const [newHostName, setNewHostName] = useState('');
  const [newHostConn, setNewHostConn] = useState<HostConnection>(emptyConnection());
  const [newGroupName, setNewGroupName] = useState('');
  const [importContent, setImportContent] = useState('');
  const [importFormat, setImportFormat] = useState<'ini' | 'yaml'>('ini');
  const [newSource, setNewSource] = useState<{ name: string; source_kind: string; source: string; credential_id: number | '' }>({ name: '', source_kind: 'inventory', source: '', credential_id: '' });

  const fetchInventories = useCallback(async () => {
    try {
      setLoading(true);
      const items = unwrap<Inventory>(await api.getInventories()).filter(i => (i as any).organization_id === orgId);
      setInventories(items);
      setSelectedInventoryId(prev => prev ?? (items[0]?.id ?? null));
    } catch (err) { setError('Failed to load inventories'); console.error(err); }
    finally { setLoading(false); }
  }, [orgId]);

  useEffect(() => {
    fetchInventories();
    api.getCredentials().then(r => setCredentials(unwrap(r))).catch(() => { });
    api.getOrganizations().then(o => setOrgName(unwrap<{ id: number; name: string }>(o).find(x => x.id === orgId)?.name ?? `Org ${orgId}`)).catch(() => setOrgName(`Org ${orgId}`));
  }, [orgId, fetchInventories]);

  const loadStructure = useCallback(async (invId: number) => {
    try {
      const [hostsData, groupsData, sourcesData] = await Promise.all([
        api.getHosts(invId), api.getGroups(invId), api.getInventorySources(invId).catch(() => []),
      ]);
      const gs: Group[] = groupsData || [];
      setHosts(hostsData || []);
      setGroups(gs);
      setSources(sourcesData || []);
      const entries = await Promise.all(gs.map(async g => {
        try { return [g.id, ((await api.getGroupHosts(g.id)) || []).map((h: Host) => h.id)] as const; }
        catch { return [g.id, []] as const; }
      }));
      setGroupHosts(Object.fromEntries(entries));
    } catch (err) { console.error('Failed to load hosts/groups', err); }
  }, []);

  useEffect(() => { if (selectedInventoryId) loadStructure(selectedInventoryId); }, [selectedInventoryId, loadStructure]);

  useEffect(() => {
    if (!selectedHostId) { setHostGroups([]); return; }
    api.getHostGroups(selectedHostId).then(d => setHostGroups((d || []).map((g: Group) => g.id))).catch(() => { });
  }, [selectedHostId]);

  // Populate the connection form + extra vars from the selected host.
  useEffect(() => {
    const host = hosts.find(h => h.id === selectedHostId);
    if (!host) return;
    const { conn, extra } = splitConnection(host.variables);
    setConnForm(conn);
    const strExtra: Record<string, string> = {};
    for (const [k, v] of Object.entries(extra)) strExtra[k] = showVal(v);
    setExtraVars(strExtra);
    originalRef.current = JSON.stringify(host.variables ?? {});
  }, [selectedHostId, hosts]);

  const coercedExtra = useMemo(() => {
    const out: Record<string, any> = {};
    for (const [k, v] of Object.entries(extraVars)) out[k] = coerce(v);
    return out;
  }, [extraVars]);

  const dirty = useMemo(() => {
    const host = hosts.find(h => h.id === selectedHostId);
    if (!host) return false;
    return JSON.stringify(mergeConnection(connForm, coercedExtra)) !== originalRef.current;
  }, [connForm, coercedExtra, selectedHostId, hosts]);

  const refreshHosts = () => { if (selectedInventoryId) loadStructure(selectedInventoryId); };

  const saveHost = async () => {
    const host = hosts.find(h => h.id === selectedHostId);
    if (!host || !dirty) return;
    setSavingHost(true);
    try {
      const updated = await api.updateHost(host.id, { variables: mergeConnection(connForm, coercedExtra) });
      setHosts(prev => prev.map(h => (h.id === host.id ? updated : h)));
      originalRef.current = JSON.stringify(updated.variables ?? {});
      toast.success('Host saved');
    } catch { toast.error('Failed to save. You need admin on this inventory.'); }
    finally { setSavingHost(false); }
  };

  const setRunner = async (hostId: number) => {
    setSettingRunner(true);
    try { await api.setRunnerHost(hostId); refreshHosts(); setSelectedHostId(hostId); }
    catch { toast.error('Failed to set runner host'); }
    finally { setSettingRunner(false); }
  };

  const deleteHost = async (hostId: number) => {
    if (!(await confirmDialog('Delete this host?', { destructive: true, confirmText: 'Delete' }))) return;
    try { await api.deleteHost(hostId); if (selectedHostId === hostId) setSelectedHostId(null); refreshHosts(); }
    catch { toast.error('Failed to delete host'); }
  };

  const toggleMembership = async (groupId: number, isIn: boolean) => {
    if (!selectedHostId) return;
    try {
      if (isIn) { await api.removeHostFromGroup(groupId, selectedHostId); setHostGroups(p => p.filter(id => id !== groupId)); }
      else { await api.addHostToGroup(groupId, selectedHostId); setHostGroups(p => [...p, groupId]); }
      setGroupHosts(prev => {
        const cur = new Set(prev[groupId] || []);
        if (isIn) cur.delete(selectedHostId); else cur.add(selectedHostId);
        return { ...prev, [groupId]: [...cur] };
      });
    } catch { toast.error('Failed to update membership'); }
  };

  const createInventory = async () => {
    if (!newInventoryName.trim()) return;
    try { await api.createInventory({ name: newInventoryName, organization_id: orgId, kind: 'standard' }); setNewInventoryName(''); setShowInventoryModal(false); fetchInventories(); }
    catch { toast.error('Failed to create inventory'); }
  };
  const deleteInventory = async (idToDel: number) => {
    if (!(await confirmDialog('Delete this inventory?', { destructive: true, confirmText: 'Delete' }))) return;
    try { await api.deleteInventory(idToDel); if (selectedInventoryId === idToDel) setSelectedInventoryId(null); fetchInventories(); }
    catch { toast.error('Failed to delete inventory'); }
  };
  const createHost = async () => {
    if (!newHostName.trim() || !selectedInventoryId) return;
    try { await api.createHost(selectedInventoryId, { name: newHostName, enabled: true, variables: mergeConnection(newHostConn, {}) }); setNewHostName(''); setNewHostConn(emptyConnection()); setShowHostModal(false); refreshHosts(); }
    catch { toast.error('Failed to create host'); }
  };
  const createGroup = async () => {
    if (!newGroupName.trim() || !selectedInventoryId) return;
    try { await api.createGroup(selectedInventoryId, { name: newGroupName }); setNewGroupName(''); setShowGroupModal(false); refreshHosts(); }
    catch { toast.error('Failed to create group'); }
  };
  const doImport = async () => {
    if (!selectedInventoryId || !importContent.trim()) return;
    try {
      const r = await api.importInventory(selectedInventoryId, importContent, importFormat);
      toast.success(`Imported ${r.hosts_created} hosts, ${r.groups_created} groups.`);
      setShowImportModal(false); setImportContent(''); refreshHosts();
    } catch { toast.error('Failed to import inventory'); }
  };
  const createSource = async () => {
    if (!selectedInventoryId || !newSource.name.trim()) return;
    try {
      await api.createInventorySource(selectedInventoryId, { ...newSource, credential_id: newSource.credential_id === '' ? null : newSource.credential_id });
      setShowSourceModal(false); setNewSource({ name: '', source_kind: 'inventory', source: '', credential_id: '' });
      api.getInventorySources(selectedInventoryId).then(d => setSources(d || [])).catch(() => { });
    } catch { toast.error('Failed to create source'); }
  };
  const syncSource = async (sid: number) => {
    if (!selectedInventoryId) return;
    await api.syncInventorySource(selectedInventoryId, sid);
    toast.info('Sync started');
    setTimeout(refreshHosts, 4000); setTimeout(refreshHosts, 9000);
  };
  const deleteSource = async (sid: number) => {
    if (!selectedInventoryId) return;
    await api.deleteInventorySource(selectedInventoryId, sid);
    api.getInventorySources(selectedInventoryId).then(d => setSources(d || [])).catch(() => { });
  };

  const selectedInv = inventories.find(i => i.id === selectedInventoryId);
  const selectedHost = hosts.find(h => h.id === selectedHostId);
  const { capabilities: inventoryCapabilities, loading: inventoryCapabilitiesLoading } = useCapabilities('inventory', selectedInventoryId);
  const canCreateInventory = !orgCapabilitiesLoading && !!orgCapabilities.add_inventory;
  const canManageInventory = !inventoryCapabilitiesLoading && inventoryCapabilities.manage;

  // Build the tree: groups (filtered) each with member hosts, then ungrouped.
  const tree = useMemo(() => {
    const q = treeFilter.trim().toLowerCase();
    const byId = new Map(hosts.map(h => [h.id, h]));
    const grouped = new Set<number>();
    Object.values(groupHosts).forEach(ids => ids.forEach(id => grouped.add(id)));
    const match = (h: Host) => !q || h.name.toLowerCase().includes(q);
    const groupNodes = groups.map(g => ({
      group: g,
      members: (groupHosts[g.id] || []).map(id => byId.get(id)).filter((h): h is Host => !!h && match(h)),
    })).filter(n => !q || n.members.length > 0 || g_match(n.group, q));
    const ungrouped = hosts.filter(h => !grouped.has(h.id) && match(h));
    return { groupNodes, ungrouped };
  }, [groups, groupHosts, hosts, treeFilter]);

  const toggleCollapse = (key: number | 'ungrouped') =>
    setCollapsed(prev => { const n = new Set(prev); n.has(key) ? n.delete(key) : n.add(key); return n; });

  const openVarsModal = () => { setVarsDraft(JSON.stringify(coercedExtra, null, 2)); setShowVarsModal(true); };
  const applyVarsDraft = () => {
    try {
      const parsed = JSON.parse(varsDraft || '{}');
      const strExtra: Record<string, string> = {};
      for (const [k, v] of Object.entries(parsed)) strExtra[k] = showVal(v);
      setExtraVars(strExtra); setShowVarsModal(false);
    } catch { toast.error('Invalid JSON'); }
  };

  if (loading) return <PageSpinner />;
  if (error) return <div className="text-err p-6">{error}</div>;

  const memberGroupNames = selectedHost ? groups.filter(g => hostGroups.includes(g.id)).map(g => g.name) : [];

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink">
      {/* Inventory context bar */}
      <div className="flex items-center gap-4 h-[54px] px-6 border-b border-line shrink-0">
        <Link to="/inventories" className="w-7 h-7 grid place-items-center rounded-md border border-line2 text-mut hover:text-ink hover:border-white/20 transition-colors shrink-0" title="All organizations">
          <ArrowLeft size={15} />
        </Link>
        <div className="relative">
          <button onClick={() => setInvMenu(v => !v)} onBlur={() => setTimeout(() => setInvMenu(false), 150)} className="flex items-center gap-2 group">
            <span className="text-[15px] font-semibold tracking-tight">{selectedInv?.name || 'No inventory'}</span>
            <ChevronDown size={14} className="text-mut group-hover:text-ink" />
          </button>
          {invMenu && (
            <div className="absolute z-30 top-9 left-0 w-64 bg-panel border border-line2 rounded-lg shadow-2xl py-1.5 max-h-80 overflow-auto scroll-tint">
              <div className="px-3 py-1 font-mono text-[9px] tracking-[0.14em] uppercase text-dim">{orgName}</div>
              {inventories.map(inv => (
                <button key={inv.id} onMouseDown={() => { setSelectedInventoryId(inv.id); setSelectedHostId(null); }}
                  className={`w-full flex items-center gap-2 px-3 py-2 text-left text-[13px] hover:bg-white/5 ${inv.id === selectedInventoryId ? 'text-acc' : 'text-ink2'}`}>
                  <Server size={14} className="shrink-0 opacity-70" /> <span className="truncate flex-1">{inv.name}</span>
                  {inv.id === selectedInventoryId && <Check size={13} />}
                </button>
              ))}
              {canCreateInventory && <div className="border-t border-line mt-1 pt-1">
                <button onMouseDown={() => setShowInventoryModal(true)} className="w-full flex items-center gap-2 px-3 py-2 text-left text-[13px] text-mut hover:text-ink hover:bg-white/5"><Plus size={14} /> New inventory</button>
              </div>}
            </div>
          )}
        </div>
        {selectedInv && (
          <div className="flex gap-4 font-mono text-[11px] text-dim">
            <span><b className="text-mut font-medium">{hosts.length}</b> hosts</span>
            <span><b className="text-mut font-medium">{groups.length}</b> groups</span>
            <span><b className="text-mut font-medium">{sources.length}</b> sources</span>
          </div>
        )}
        {canManageInventory && <div className="ml-auto flex items-center gap-1">
          <button onClick={() => setShowImportModal(true)} className="h-8 px-3 rounded-md text-xs font-medium flex items-center gap-1.5 text-mut hover:text-ink hover:bg-white/5 transition-colors"><Upload size={14} /> Import</button>
          {selectedInv && (
            <button onClick={() => deleteInventory(selectedInv.id)} className="w-8 h-8 grid place-items-center rounded-md text-dim hover:text-err hover:bg-white/5 transition-colors" title="Delete inventory"><Trash2 size={15} /></button>
          )}
        </div>}
      </div>

      {!selectedInv ? (
        <div className="flex-1 grid place-items-center text-dim">
          <div className="text-center">
            <Server size={40} className="mx-auto mb-3 opacity-20" />
            <p className="text-sm mb-4">No inventories in {orgName} yet.</p>
            {canCreateInventory && <Button icon={<Plus size={15} />} onClick={() => setShowInventoryModal(true)}>New inventory</Button>}
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-[268px_1fr] flex-1 min-h-0 max-[820px]:grid-cols-1">
          {/* STRUCTURE */}
          <div className="flex flex-col min-h-0 border-r border-line bg-tree max-[820px]:hidden">
            <div className="flex items-center gap-2.5 h-[46px] px-4 border-b border-line shrink-0">
              <Search size={14} className="text-dim shrink-0" />
              <input value={treeFilter} onChange={e => setTreeFilter(e.target.value)} placeholder="Filter hosts" className="flex-1 bg-transparent border-none outline-none text-[12.5px] text-ink placeholder:text-dim" />
            </div>
            <div className="flex items-center h-[34px] px-4 mt-1.5 shrink-0">
              <span className="font-mono text-[9px] tracking-[0.16em] uppercase text-dim">Structure</span>
              {canManageInventory && <div className="ml-auto relative">
                <button onClick={() => setAddMenu(v => !v)} onBlur={() => setTimeout(() => setAddMenu(false), 150)} className="text-dim hover:text-ink" title="Add"><Plus size={15} /></button>
                {addMenu && (
                  <div className="absolute z-30 top-6 right-0 w-40 bg-panel border border-line2 rounded-lg shadow-2xl py-1.5">
                    <button onMouseDown={() => setShowHostModal(true)} className="w-full text-left px-3 py-1.5 text-[13px] text-ink2 hover:bg-white/5">Add host</button>
                    <button onMouseDown={() => setShowGroupModal(true)} className="w-full text-left px-3 py-1.5 text-[13px] text-ink2 hover:bg-white/5">Add group</button>
                    <button onMouseDown={() => setShowSourceModal(true)} className="w-full text-left px-3 py-1.5 text-[13px] text-ink2 hover:bg-white/5">Add source</button>
                  </div>
                )}
              </div>}
            </div>
            <div className="flex-1 overflow-auto scroll-tint px-2.5 pb-6">
              {tree.groupNodes.map(({ group, members }) => {
                const isCollapsed = collapsed.has(group.id);
                return (
                  <div key={group.id}>
                    <button onClick={() => toggleCollapse(group.id)} className="w-full flex items-center gap-2 h-[30px] px-2.5 rounded-lg hover:bg-white/[0.028]">
                      {isCollapsed ? <ChevronRight size={11} className="text-dim shrink-0" /> : <ChevronDown size={11} className="text-dim shrink-0" />}
                      <span className="text-[12.5px] font-semibold text-ink2 tracking-[0.01em]">{group.name}</span>
                      <span className="ml-auto font-mono text-[10.5px] text-faint">{members.length}</span>
                    </button>
                    {!isCollapsed && (
                      <div className="relative mb-1 before:content-[''] before:absolute before:left-4 before:top-0.5 before:bottom-3.5 before:w-px before:bg-line">
                        {members.map(h => <HostRow key={h.id} host={h} sel={h.id === selectedHostId} onClick={() => setSelectedHostId(h.id)} />)}
                        {members.length === 0 && <div className="pl-7 py-1 font-mono text-[11px] text-faint">empty</div>}
                      </div>
                    )}
                  </div>
                );
              })}
              {tree.ungrouped.length > 0 && (
                <div className="mt-3 pt-2.5 border-t border-line">
                  <button onClick={() => toggleCollapse('ungrouped')} className="w-full flex items-center gap-2 h-[30px] px-2.5 rounded-lg hover:bg-white/[0.028]">
                    {collapsed.has('ungrouped') ? <ChevronRight size={11} className="text-dim shrink-0" /> : <ChevronDown size={11} className="text-dim shrink-0" />}
                    <span className="text-[12.5px] font-semibold text-dim">ungrouped</span>
                    <span className="ml-auto font-mono text-[10.5px] text-faint">{tree.ungrouped.length}</span>
                  </button>
                  {!collapsed.has('ungrouped') && (
                    <div className="relative mb-1 before:content-[''] before:absolute before:left-4 before:top-0.5 before:bottom-3.5 before:w-px before:bg-line">
                      {tree.ungrouped.map(h => <HostRow key={h.id} host={h} sel={h.id === selectedHostId} onClick={() => setSelectedHostId(h.id)} />)}
                    </div>
                  )}
                </div>
              )}
              {hosts.length === 0 && <p className="px-3 py-6 text-[12px] text-dim text-center">No hosts. Add one, import, or sync a source.</p>}
            </div>
          </div>

          {/* EDITOR / OVERVIEW */}
          <div className="flex flex-col min-h-0 bg-bg">
            {selectedHost ? (
              <>
                <div className="flex items-start gap-4 px-10 pt-6 pb-5 border-b border-line shrink-0 max-[820px]:px-5">
                  <div className="min-w-0">
                    <div className="font-mono text-[11px] text-dim mb-2">
                      <span className="text-mut">{selectedInv.name}</span>
                      {memberGroupNames[0] && <><span className="mx-1.5 text-faint">/</span><span className="text-mut">{memberGroupNames[0]}</span></>}
                      <span className="mx-1.5 text-faint">/</span>{selectedHost.name}
                    </div>
                    <h1 className="font-mono text-[23px] font-semibold tracking-tight leading-none truncate">{selectedHost.name}</h1>
                    <div className="mt-2.5 text-[12px] text-dim flex items-center gap-2 flex-wrap">
                      <span>host{memberGroupNames.length ? <> · member of {memberGroupNames.map((n, i) => <React.Fragment key={n}>{i > 0 && ', '}<b className="text-mut font-medium">{n}</b></React.Fragment>)}</> : ' · no groups'}</span>
                      {selectedHost.is_runner_host && <span className="px-1.5 py-0.5 rounded font-mono text-[10px] bg-violet/15 text-violet">runner</span>}
                    </div>
                  </div>
                  <div className="ml-auto flex items-center gap-3 pt-1 shrink-0">
                    <span className="font-mono text-[10.5px] text-dim flex items-center gap-1.5">
                      {dirty ? <><span className="w-1.5 h-1.5 rounded-full bg-changed" /> unsaved</> : <><Check size={12} className="text-faint" /> saved</>}
                    </span>
                    {canManageInventory && <Button size="sm" disabled={!dirty || savingHost} onClick={saveHost} icon={savingHost ? <Loader size={13} className="animate-spin" /> : undefined}>Save</Button>}
                    {canManageInventory && <HostActions host={selectedHost} settingRunner={settingRunner} onRunner={() => setRunner(selectedHost.id)} onDelete={() => deleteHost(selectedHost.id)} />}
                  </div>
                </div>

                <div className="flex-1 overflow-auto scroll-tint px-10 pb-16 max-[820px]:px-5">
                  <div className="max-w-[640px]">
                    {/* Connection */}
                    <Section title="Connection">
                      <ConnRow readOnly={!canManageInventory} label="Address" varName="ansible_host" value={connForm.ansible_host} placeholder={selectedHost.name} onChange={v => setConnForm({ ...connForm, ansible_host: v })} />
                      <ConnRow readOnly={!canManageInventory} label="Port" varName="ansible_port" value={connForm.ansible_port} placeholder="22" sm onChange={v => setConnForm({ ...connForm, ansible_port: v })} />
                      <ConnRow readOnly={!canManageInventory} label="User" varName="ansible_user" value={connForm.ansible_user} placeholder="root" onChange={v => setConnForm({ ...connForm, ansible_user: v })} />
                      <div className="grid grid-cols-[118px_1fr] items-center gap-5 py-1.5">
                        <div className="text-[12.5px] text-mut">Transport<span className="block font-mono text-[9.5px] text-faint mt-0.5">ansible_connection</span></div>
                        <select disabled={!canManageInventory} value={connForm.ansible_connection} onChange={e => setConnForm({ ...connForm, ansible_connection: e.target.value })}
                          className="max-w-[120px] bg-transparent border-b border-line focus:border-acc text-ink font-mono text-[13px] py-1.5 outline-none hover:border-line2">
                          <option value="" className="bg-panel">ssh</option>
                          <option value="ssh" className="bg-panel">ssh</option>
                          <option value="local" className="bg-panel">local</option>
                          <option value="paramiko" className="bg-panel">paramiko</option>
                        </select>
                      </div>
                      <ConnRow readOnly={!canManageInventory} label="Python" varName="ansible_python_interpreter" value={connForm.ansible_python_interpreter} placeholder="/usr/bin/python3" onChange={v => setConnForm({ ...connForm, ansible_python_interpreter: v })} />
                    </Section>

                    {/* Defined vars */}
                    <Section title="Defined on this host" hint={`host_vars · ${Object.keys(extraVars).length}`} action={canManageInventory ? <button onClick={openVarsModal} className="font-mono text-[11px] text-dim hover:text-acc">edit as JSON</button> : undefined}>
                      {Object.keys(extraVars).length === 0 && <p className="font-mono text-[12px] text-faint py-1">No host-specific variables.</p>}
                      {Object.entries(extraVars).map(([k, v]) => (
                        <div key={k} className="flex items-center gap-3.5 py-2 group">
                          <span className="w-1.5 h-1.5 rounded-full bg-acc shrink-0" />
                          <span className="font-mono text-[13px] text-ink min-w-[158px]">{k}</span>
                          <span className="text-faint font-mono">=</span>
                          <input readOnly={!canManageInventory} value={v} onChange={e => setExtraVars(p => ({ ...p, [k]: e.target.value }))}
                            className="flex-1 bg-transparent border-b border-transparent group-hover:border-line focus:border-acc text-ink2 font-mono text-[13px] pb-0.5 outline-none" />
                          {canManageInventory && <button onClick={() => setExtraVars(p => { const n = { ...p }; delete n[k]; return n; })} className="text-faint hover:text-err opacity-0 group-hover:opacity-100" title="Remove"><Trash2 size={13} /></button>}
                        </div>
                      ))}
                      {canManageInventory && <button onClick={() => { let i = 1; let key = 'new_var'; while (key in extraVars) key = `new_var_${i++}`; setExtraVars(p => ({ ...p, [key]: '' })); }}
                        className="flex items-center gap-2 pt-3 ml-5 font-mono text-[12px] text-dim hover:text-acc">
                        <Plus size={13} /> add variable
                      </button>}
                    </Section>

                    {/* Membership */}
                    <Section title="Group membership">
                      {groups.length === 0 ? <p className="font-mono text-[12px] text-faint py-1">No groups in this inventory.</p> : (
                        <div className="flex flex-wrap gap-2">
                          {groups.map(g => {
                            const isIn = hostGroups.includes(g.id);
                            return (
                              <button key={g.id} disabled={!canManageInventory} onClick={() => toggleMembership(g.id, isIn)}
                                className={`flex items-center gap-1.5 px-2.5 py-1 rounded-md font-mono text-[12px] border transition-colors ${isIn ? 'bg-grp/10 text-grp border-grp/30' : 'text-mut border-line hover:border-line2'}`}>
                                {isIn && <Check size={12} />} {g.name}
                              </button>
                            );
                          })}
                        </div>
                      )}
                    </Section>
                  </div>
                </div>
              </>
            ) : (
              /* Inventory overview */
              <div className="flex-1 overflow-auto scroll-tint px-10 pt-7 pb-16 max-[820px]:px-5">
                <div className="max-w-[720px]">
                  <h1 className="text-[21px] font-semibold tracking-tight">{selectedInv.name}</h1>
                  <p className="text-sm text-mut mt-1">Select a host in the structure to {canManageInventory ? 'edit' : 'inspect'} its connection and variables.</p>

                  <Section title="Sources" hint={`${sources.length}`} action={canManageInventory ? <button onClick={() => setShowSourceModal(true)} className="font-mono text-[11px] text-dim hover:text-acc">add source</button> : undefined}>
                    {sources.length === 0 ? <p className="font-mono text-[12px] text-faint py-1">No dynamic sources. Add one to populate hosts (e.g. AWS).</p> : (
                      <div className="space-y-0">
                        {sources.map(s => (
                          <div key={s.id} className="flex items-center gap-3 py-2.5 border-b border-line last:border-0">
                            <span className="text-[13px] text-ink font-medium">{s.name}</span>
                            <span className="font-mono text-[11px] text-dim">{s.source_kind}</span>
                            <span className="ml-auto font-mono text-[11px] text-dim">{s.last_synced_at ? new Date(s.last_synced_at).toLocaleString() : 'never synced'}</span>
                            {canManageInventory && <button onClick={() => syncSource(s.id)} className="text-mut hover:text-acc" title="Sync now"><RefreshCw size={14} /></button>}
                            {canManageInventory && <button onClick={() => deleteSource(s.id)} className="text-faint hover:text-err" title="Delete source"><Trash2 size={13} /></button>}
                          </div>
                        ))}
                      </div>
                    )}
                  </Section>

                  <Section title="Access" icon={<Shield size={13} className="text-dim" />}>
                    <ResourceAccess contentType="inventory" objectId={selectedInv.id} canManage={canManageInventory} />
                  </Section>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* ── Modals ─────────────────────────────────────────────────────── */}
      <Modal isOpen={canCreateInventory && showInventoryModal} onClose={() => setShowInventoryModal(false)} title={`New inventory in ${orgName}`}>
        <div className="space-y-4">
          <Input label="Name" value={newInventoryName} onChange={e => setNewInventoryName(e.target.value)} placeholder="My inventory" />
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setShowInventoryModal(false)}>Cancel</Button><Button onClick={createInventory}>Create</Button></div>
        </div>
      </Modal>

      <Modal isOpen={canManageInventory && showHostModal} onClose={() => setShowHostModal(false)} title="New host">
        <div className="space-y-4">
          <Input label="Hostname" value={newHostName} onChange={e => setNewHostName(e.target.value)} placeholder="web-01" />
          <div className="grid grid-cols-2 gap-3">
            <Input label="Address" wrapperClassName="col-span-2" value={newHostConn.ansible_host} onChange={e => setNewHostConn({ ...newHostConn, ansible_host: e.target.value })} placeholder={newHostName || 'ansible_host'} className="font-mono" />
            <Input label="Port" value={newHostConn.ansible_port} onChange={e => setNewHostConn({ ...newHostConn, ansible_port: e.target.value })} placeholder="22" inputMode="numeric" className="font-mono" />
            <Input label="User" value={newHostConn.ansible_user} onChange={e => setNewHostConn({ ...newHostConn, ansible_user: e.target.value })} placeholder="root" className="font-mono" />
          </div>
          <p className="text-[11px] text-dim">Leave address blank to connect by hostname. Editable later on the host.</p>
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => { setShowHostModal(false); setNewHostConn(emptyConnection()); }}>Cancel</Button><Button onClick={createHost}>Create</Button></div>
        </div>
      </Modal>

      <Modal isOpen={canManageInventory && showGroupModal} onClose={() => setShowGroupModal(false)} title="New group">
        <div className="space-y-4">
          <Input label="Group name" value={newGroupName} onChange={e => setNewGroupName(e.target.value)} placeholder="webservers" />
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setShowGroupModal(false)}>Cancel</Button><Button onClick={createGroup}>Create</Button></div>
        </div>
      </Modal>

      <Modal isOpen={canManageInventory && showImportModal} onClose={() => setShowImportModal(false)} title="Import inventory" size="lg">
        <div className="space-y-4">
          <Select label="Format" value={importFormat} onChange={e => setImportFormat(e.target.value as 'ini' | 'yaml')}>
            <option value="ini">INI (Ansible format)</option>
            <option value="yaml">YAML</option>
          </Select>
          <Textarea label="Inventory content" className="h-64 font-mono text-xs" value={importContent} onChange={e => setImportContent(e.target.value)}
            placeholder={importFormat === 'ini' ? '[webservers]\nweb1.example.com\n\n[databases]\ndb1.example.com' : 'all:\n  children:\n    webservers:\n      hosts:\n        web1.example.com:'} />
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setShowImportModal(false)}>Cancel</Button><Button onClick={doImport} disabled={!importContent.trim()}>Import</Button></div>
        </div>
      </Modal>

      <Modal isOpen={canManageInventory && showSourceModal} onClose={() => setShowSourceModal(false)} title="New inventory source" size="lg">
        <div className="space-y-4">
          <Input label="Name" value={newSource.name} onChange={e => setNewSource({ ...newSource, name: e.target.value })} />
          <Select label="Kind" value={newSource.source_kind} onChange={e => setNewSource({ ...newSource, source_kind: e.target.value })}>
            <option value="inventory">Inventory / plugin (YAML)</option>
            <option value="script">Script (executable)</option>
          </Select>
          <Select label="Credential (optional)" hint="A cloud credential whose injectors authenticate the plugin." value={newSource.credential_id} onChange={e => setNewSource({ ...newSource, credential_id: e.target.value === '' ? '' : Number(e.target.value) })}>
            <option value="">None</option>
            {credentials.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
          </Select>
          <Textarea label="Source" rows={8} className="font-mono text-xs" value={newSource.source} onChange={e => setNewSource({ ...newSource, source: e.target.value })} hint="YAML plugin config, or a script emitting Ansible inventory JSON." />
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setShowSourceModal(false)}>Cancel</Button><Button onClick={createSource}>Create</Button></div>
        </div>
      </Modal>

      <Modal isOpen={canManageInventory && showVarsModal} onClose={() => setShowVarsModal(false)} title="Edit host variables (JSON)" size="lg">
        <div className="space-y-4">
          <Textarea rows={14} className="font-mono text-xs" value={varsDraft} onChange={e => setVarsDraft(e.target.value)} />
          <p className="text-[11px] text-dim">Connection fields (ansible_host, ansible_port…) are edited above; this covers everything else.</p>
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setShowVarsModal(false)}>Cancel</Button><Button onClick={applyVarsDraft}>Apply</Button></div>
        </div>
      </Modal>
    </div>
  );
};

// Whether a group's own name matches the tree filter (so an empty matching group still shows).
function g_match(g: Group, q: string) { return g.name.toLowerCase().includes(q); }

const HostRow: React.FC<{ host: Host; sel: boolean; onClick: () => void }> = ({ host, sel, onClick }) => (
  <button onClick={onClick} className={`w-full flex items-center gap-2.5 h-7 pl-7 pr-2.5 rounded-lg ${sel ? 'bg-acc/[0.09]' : 'hover:bg-white/[0.028]'}`}>
    <span className={`w-[5px] h-[5px] rounded-full shrink-0 ${sel ? 'bg-acc' : host.is_runner_host ? 'bg-violet' : host.enabled ? 'bg-faint' : 'bg-faint/50'}`} />
    <span className={`font-mono text-[12px] truncate ${sel ? 'text-ink font-medium' : 'text-mut'}`}>{host.name}</span>
  </button>
);

const HostActions: React.FC<{ host: Host; settingRunner: boolean; onRunner: () => void; onDelete: () => void }> = ({ host, settingRunner, onRunner, onDelete }) => {
  const [open, setOpen] = useState(false);
  return (
    <div className="relative">
      <button onClick={() => setOpen(v => !v)} onBlur={() => setTimeout(() => setOpen(false), 150)} className="w-8 h-8 grid place-items-center rounded-md text-dim hover:text-ink hover:bg-white/5" title="Host actions"><MoreHorizontal size={16} /></button>
      {open && (
        <div className="absolute z-30 top-9 right-0 w-48 bg-panel border border-line2 rounded-lg shadow-2xl py-1.5">
          {!host.is_runner_host && (
            <button onMouseDown={onRunner} disabled={settingRunner} className="w-full flex items-center gap-2 px-3 py-2 text-left text-[13px] text-ink2 hover:bg-white/5 disabled:opacity-50">
              {settingRunner ? <Loader size={13} className="animate-spin" /> : <Radio size={14} />} Set as runner host
            </button>
          )}
          <button onMouseDown={onDelete} className="w-full flex items-center gap-2 px-3 py-2 text-left text-[13px] text-err/90 hover:bg-err/10"><Trash2 size={14} /> Delete host</button>
        </div>
      )}
    </div>
  );
};

const Section: React.FC<{ title: string; hint?: string; icon?: React.ReactNode; action?: React.ReactNode; children: React.ReactNode }> = ({ title, hint, icon, action, children }) => (
  <div className="py-6 border-t border-line first:border-t-0">
    <div className="flex items-baseline gap-3 mb-4">
      {icon}
      <span className="font-mono text-[10px] tracking-[0.16em] uppercase text-mut">{title}</span>
      {hint && <span className="font-mono text-[9.5px] text-faint">{hint}</span>}
      {action && <span className="ml-auto">{action}</span>}
    </div>
    {children}
  </div>
);

const ConnRow: React.FC<{ label: string; varName: string; value: string; placeholder?: string; sm?: boolean; readOnly?: boolean; onChange: (v: string) => void }> = ({ label, varName, value, placeholder, sm, readOnly, onChange }) => (
  <div className="grid grid-cols-[118px_1fr] items-center gap-5 py-1.5">
    <div className="text-[12.5px] text-mut">{label}<span className="block font-mono text-[9.5px] text-faint mt-0.5">{varName}</span></div>
    <input readOnly={readOnly} value={value} placeholder={placeholder} onChange={e => onChange(e.target.value)}
      className={`${sm ? 'max-w-[120px]' : 'max-w-[300px]'} w-full bg-transparent border-b border-line focus:border-acc text-ink font-mono text-[13px] py-1.5 outline-none hover:border-line2 placeholder:text-faint`} />
  </div>
);

export default InventoriesPage;
