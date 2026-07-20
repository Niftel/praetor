import React, { useEffect, useMemo, useState } from 'react';
import { Bot, Check, Copy, KeyRound, Pencil, Plus, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Input } from '../components/ui/Input';
import { PageSpinner } from '../components/ui/PageSpinner';
import { confirmDialog, toast } from '../components/ui/toast';
import { api, unwrap } from '../services/api';
import type { DelegatedLaunchGrant, Group, Host, Inventory, ServiceCredential, ServicePrincipal, Team } from '../types';

type Named = { id: number; name: string; organization_id?: number };
type SecretResult = { token: string; name: string; expires_at: string };
type PrincipalForm = { name: string; description: string };
type CredentialForm = { name: string; expires: string };
type GrantForm = {
  workflowId: string; inventoryId: string; hostIds: number[]; groupIds: number[];
  maxHosts: string; extraVarKeys: string; approvalTeamId: string; notBefore: string; expires: string;
};

const blankPrincipal: PrincipalForm = { name: '', description: '' };
const blankCredential = (): CredentialForm => ({ name: '', expires: localDate(90) });
const blankGrant = (): GrantForm => ({
  workflowId: '', inventoryId: '', hostIds: [], groupIds: [], maxHosts: '',
  extraVarKeys: '', approvalTeamId: '', notBefore: localDateTime(0), expires: localDateTime(90),
});
const fmt = (value?: string | null) => value ? new Date(value).toLocaleString() : '—';
const activeCredential = (c: ServiceCredential) => !c.revoked_at && new Date(c.expires_at) > new Date();
const activeGrant = (g: DelegatedLaunchGrant) => !g.revoked_at && new Date(g.not_before) <= new Date() && new Date(g.expires_at) > new Date();
function localDate(days: number) { const d = new Date(Date.now() + days * 86400000); return d.toISOString().slice(0, 10); }
function localDateTime(days: number) { const d = new Date(Date.now() + days * 86400000); d.setMinutes(d.getMinutes() - d.getTimezoneOffset()); return d.toISOString().slice(0, 16); }
function iso(value: string) { return new Date(value).toISOString(); }

const ServicePrincipalsPage: React.FC = () => {
  const [organizations, setOrganizations] = useState<Named[]>([]);
  const [orgId, setOrgId] = useState<number | null>(null);
  const [principals, setPrincipals] = useState<ServicePrincipal[]>([]);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [credentials, setCredentials] = useState<ServiceCredential[]>([]);
  const [grants, setGrants] = useState<DelegatedLaunchGrant[]>([]);
  const [workflows, setWorkflows] = useState<Named[]>([]);
  const [inventories, setInventories] = useState<Inventory[]>([]);
  const [teams, setTeams] = useState<Team[]>([]);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(true);
  const [detailLoading, setDetailLoading] = useState(false);
  const [principalModal, setPrincipalModal] = useState(false);
  const [editingPrincipal, setEditingPrincipal] = useState(false);
  const [credentialModal, setCredentialModal] = useState(false);
  const [grantModal, setGrantModal] = useState(false);
  const [editingGrant, setEditingGrant] = useState<DelegatedLaunchGrant | null>(null);
  const [rotating, setRotating] = useState<ServiceCredential | null>(null);
  const [secret, setSecret] = useState<SecretResult | null>(null);
  const [copied, setCopied] = useState(false);
  const [principalForm, setPrincipalForm] = useState(blankPrincipal);
  const [credentialForm, setCredentialForm] = useState(blankCredential);
  const [grantForm, setGrantForm] = useState(blankGrant);
  const selected = principals.find(p => p.id === selectedId) || null;

  useEffect(() => {
    api.getOrganizations().then((rows: Named[]) => {
      const organizations = unwrap<Named>(rows);
      setOrganizations(organizations);
      if (organizations.length) setOrgId(organizations[0].id);
    }).catch(e => toast.error(e.message || 'Failed to load organizations')).finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    if (!orgId) return;
    setLoading(true);
    Promise.all([api.getServicePrincipals(orgId), api.getWorkflows(), api.getInventories(), api.getOrganizationTeams(orgId)])
      .then(([ps, ws, invs, ts]) => {
        const orgPrincipals = unwrap<ServicePrincipal>(ps);
        setPrincipals(orgPrincipals);
        setWorkflows(unwrap<Named>(ws).filter(w => w.organization_id === orgId));
        setInventories(unwrap<Inventory>(invs).filter(i => i.organization_id === orgId));
        setTeams(unwrap<Team>(ts));
        setSelectedId(current => orgPrincipals.some((p: ServicePrincipal) => p.id === current) ? current : orgPrincipals[0]?.id || null);
      })
      .catch(e => { setPrincipals([]); toast.error(e.message || 'You cannot administer service principals in this organization'); })
      .finally(() => setLoading(false));
  }, [orgId]);

  const loadDetail = async (id: number) => {
    setDetailLoading(true);
    try {
      const [cs, gs] = await Promise.all([api.getServiceCredentials(id), api.getDelegatedLaunchGrants(id)]);
      setCredentials(unwrap<ServiceCredential>(cs));
      setGrants(unwrap<DelegatedLaunchGrant>(gs).map(grant => ({
        ...grant,
        allowed_host_ids: Array.isArray(grant.allowed_host_ids) ? grant.allowed_host_ids : [],
        allowed_group_ids: Array.isArray(grant.allowed_group_ids) ? grant.allowed_group_ids : [],
        allowed_extra_var_keys: Array.isArray(grant.allowed_extra_var_keys) ? grant.allowed_extra_var_keys : [],
      })));
    } catch (e: any) { toast.error(e.message || 'Failed to load service principal'); }
    finally { setDetailLoading(false); }
  };
  useEffect(() => { if (selectedId) loadDetail(selectedId); else { setCredentials([]); setGrants([]); } }, [selectedId]);

  useEffect(() => {
    const inventoryId = Number(grantForm.inventoryId);
    if (!inventoryId) { setHosts([]); setGroups([]); return; }
    Promise.all([api.getHosts(inventoryId), api.getGroups(inventoryId)])
      .then(([hs, gs]) => { setHosts(hs || []); setGroups(gs || []); })
      .catch(() => { setHosts([]); setGroups([]); });
  }, [grantForm.inventoryId]);

  const names = useMemo(() => ({
    workflows: new Map(workflows.map(x => [x.id, x.name])),
    inventories: new Map(inventories.map(x => [x.id, x.name])),
    teams: new Map(teams.map(x => [x.id, x.name])),
  }), [workflows, inventories, teams]);

  const savePrincipal = async () => {
    if (!orgId || !principalForm.name.trim()) return;
    const wasEditing = editingPrincipal;
    try {
      const p = editingPrincipal && selected
        ? await api.updateServicePrincipal(selected.id, { name: principalForm.name.trim(), description: principalForm.description.trim() })
        : await api.createServicePrincipal(orgId, { name: principalForm.name.trim(), description: principalForm.description.trim() });
      setPrincipals(rows => (editingPrincipal ? rows.map(row => row.id === p.id ? p : row) : [...rows, p]).sort((a, b) => a.name.localeCompare(b.name)));
      setSelectedId(p.id); setPrincipalModal(false); setPrincipalForm(blankPrincipal); setEditingPrincipal(false);
      toast.success(wasEditing ? 'Service principal updated' : 'Service principal created');
    } catch (e: any) { toast.error(e.message || 'Failed to create service principal'); }
  };

  const togglePrincipal = async () => {
    if (!selected) return;
    const action = selected.enabled ? 'disable' : 'enable';
    if (selected.enabled && !(await confirmDialog(`Disable "${selected.name}"? All of its credentials will immediately stop authenticating.`, { destructive: true, confirmText: 'Disable' }))) return;
    try {
      const updated = selected.enabled
        ? (await api.disableServicePrincipal(selected.id), { ...selected, enabled: false, disabled_at: new Date().toISOString() })
        : await api.updateServicePrincipal(selected.id, { enabled: true });
      setPrincipals(rows => rows.map(p => p.id === selected.id ? updated : p));
      toast.success(`Service principal ${action}d`);
    } catch (e: any) { toast.error(e.message || `Failed to ${action} service principal`); }
  };

  const saveCredential = async () => {
    if (!selected || !credentialForm.name.trim() || !credentialForm.expires) return;
    try {
      const result = rotating
        ? await api.rotateServiceCredential(selected.id, rotating.id, { name: credentialForm.name.trim(), expires_at: iso(credentialForm.expires) })
        : await api.createServiceCredential(selected.id, { name: credentialForm.name.trim(), expires_at: iso(credentialForm.expires) });
      setSecret(result); setCredentialModal(false); setRotating(null); setCredentialForm(blankCredential());
      await loadDetail(selected.id);
    } catch (e: any) { toast.error(e.message || 'Failed to create credential'); }
  };

  const revokeCredential = async (credential: ServiceCredential) => {
    if (!selected || !(await confirmDialog(`Revoke credential "${credential.name}"? The token cannot be recovered.`, { destructive: true, confirmText: 'Revoke' }))) return;
    try { await api.revokeServiceCredential(selected.id, credential.id); await loadDetail(selected.id); }
    catch (e: any) { toast.error(e.message || 'Failed to revoke credential'); }
  };

  const openGrant = (grant?: DelegatedLaunchGrant) => {
    setEditingGrant(grant || null);
    setGrantForm(grant ? {
      workflowId: String(grant.workflow_template_id), inventoryId: String(grant.inventory_id),
      hostIds: grant.allowed_host_ids || [], groupIds: grant.allowed_group_ids || [],
      maxHosts: grant.max_hosts ? String(grant.max_hosts) : '',
      extraVarKeys: (grant.allowed_extra_var_keys || []).join(', '),
      approvalTeamId: grant.approval_team_id ? String(grant.approval_team_id) : '',
      notBefore: toLocalInput(grant.not_before), expires: toLocalInput(grant.expires_at),
    } : blankGrant());
    setGrantModal(true);
  };

  const saveGrant = async () => {
    if (!selected || !grantForm.workflowId || !grantForm.inventoryId || !grantForm.expires) return;
    const data = {
      workflow_template_id: Number(grantForm.workflowId), inventory_id: Number(grantForm.inventoryId),
      allowed_host_ids: grantForm.hostIds, allowed_group_ids: grantForm.groupIds,
      max_hosts: grantForm.maxHosts ? Number(grantForm.maxHosts) : null,
      allowed_extra_var_keys: grantForm.extraVarKeys.split(',').map(x => x.trim()).filter(Boolean),
      approval_team_id: grantForm.approvalTeamId ? Number(grantForm.approvalTeamId) : null,
      not_before: iso(grantForm.notBefore), expires_at: iso(grantForm.expires),
    };
    try {
      if (editingGrant) await api.updateDelegatedLaunchGrant(selected.id, editingGrant.id, data);
      else await api.createDelegatedLaunchGrant(selected.id, data);
      setGrantModal(false); setEditingGrant(null); await loadDetail(selected.id);
      toast.success(editingGrant ? 'Grant updated' : 'Grant created');
    } catch (e: any) { toast.error(e.message || 'Failed to save grant'); }
  };

  const revokeGrant = async (grant: DelegatedLaunchGrant) => {
    if (!selected || !(await confirmDialog('Revoke this delegated launch grant? New launches using it will be denied.', { destructive: true, confirmText: 'Revoke' }))) return;
    try { await api.revokeDelegatedLaunchGrant(selected.id, grant.id); await loadDetail(selected.id); }
    catch (e: any) { toast.error(e.message || 'Failed to revoke grant'); }
  };

  if (loading && !organizations.length) return <PageSpinner />;
  return (
    <div className="flex h-full min-h-0 bg-bg text-ink">
      <aside className="w-[310px] shrink-0 border-r border-line bg-tree flex flex-col max-[760px]:w-[240px]">
        <div className="p-4 border-b border-line">
          <label className="block font-mono text-[9px] tracking-[0.14em] uppercase text-dim mb-2">Organization</label>
          <select className="w-full h-9 rounded-lg border border-line2 bg-panel px-3 text-[12.5px]" value={orgId || ''} onChange={e => setOrgId(Number(e.target.value))}>
            {organizations.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}
          </select>
        </div>
        <div className="flex items-center px-4 py-3">
          <span className="font-mono text-[9px] tracking-[0.14em] uppercase text-dim">Application identities</span>
          <button className="ml-auto p-1.5 rounded text-acc2 hover:bg-white/5" title="New service principal" onClick={() => { setEditingPrincipal(false); setPrincipalForm(blankPrincipal); setPrincipalModal(true); }}><Plus size={15} /></button>
        </div>
        <div className="overflow-auto scroll-tint px-2 pb-3">
          {principals.map(p => (
            <button key={p.id} onClick={() => setSelectedId(p.id)} className={`w-full text-left px-3 py-2.5 rounded-lg mb-1 ${selectedId === p.id ? 'bg-acc/[0.09] text-ink' : 'text-mut hover:bg-white/[0.03] hover:text-ink2'}`}>
              <span className="flex items-center gap-2 text-[12.5px] font-medium"><Bot size={14} className={selectedId === p.id ? 'text-acc2' : 'text-dim'} />{p.name}</span>
              <span className="mt-1 flex items-center gap-1.5 font-mono text-[9.5px]"><span className={`w-1.5 h-1.5 rounded-full ${p.enabled ? 'bg-ok' : 'bg-faint'}`} />{p.enabled ? 'enabled' : 'disabled'}</span>
            </button>
          ))}
          {!principals.length && <p className="px-3 py-8 text-center text-[12px] text-dim">No service principals in this organization.</p>}
        </div>
      </aside>

      <main className="flex-1 min-w-0 overflow-auto scroll-tint">
        {!selected ? (
          <div className="h-full grid place-items-center p-8 text-center"><div><Bot size={34} className="mx-auto text-dim mb-3" /><h1 className="text-[18px] font-semibold">No application identity selected</h1><p className="mt-1 text-[12.5px] text-mut">Create a service principal to give an application narrowly scoped workflow access.</p><Button className="mt-4" icon={<Plus size={15} />} onClick={() => { setEditingPrincipal(false); setPrincipalForm(blankPrincipal); setPrincipalModal(true); }}>New service principal</Button></div></div>
        ) : (
          <>
            <header className="px-8 pt-6 pb-5 border-b border-line flex gap-4 items-start">
              <div className="min-w-0"><div className="flex items-center gap-2.5"><h1 className="text-[20px] font-semibold tracking-tight truncate">{selected.name}</h1><Status active={selected.enabled} /></div><p className="mt-1.5 text-[12.5px] text-mut max-w-[70ch]">{selected.description || 'Non-human identity for an application or integration.'}</p><p className="mt-2 font-mono text-[10.5px] text-dim">principal id {selected.id} · created {fmt(selected.created_at)}</p></div>
              <div className="ml-auto flex gap-2 shrink-0">
                <Button variant="secondary" icon={<Pencil size={14} />} onClick={() => { setEditingPrincipal(true); setPrincipalForm({ name: selected.name, description: selected.description }); setPrincipalModal(true); }}>Edit</Button>
                <Button variant={selected.enabled ? 'danger' : 'secondary'} onClick={togglePrincipal}>{selected.enabled ? 'Disable' : 'Enable'}</Button>
              </div>
            </header>
            {secret && <SecretPanel secret={secret.token} onClose={() => setSecret(null)} copied={copied} onCopy={() => { navigator.clipboard?.writeText(secret.token); setCopied(true); setTimeout(() => setCopied(false), 1500); }} />}
            {detailLoading ? <PageSpinner /> : <div className="px-8 py-6 max-w-[1180px]">
              <Section title="Credentials" description="Expiring bearer credentials. Plaintext is shown once and never stored by the UI." action={<Button size="sm" icon={<Plus size={14} />} disabled={!selected.enabled} onClick={() => { setRotating(null); setCredentialForm(blankCredential()); setCredentialModal(true); }}>New credential</Button>}>
                <DataHeader columns="grid-cols-[1fr_170px_170px_110px]"><span>Name</span><span>Last used</span><span>Expires</span><span className="text-right">Actions</span></DataHeader>
                {credentials.map(c => <div key={c.id} className="grid grid-cols-[1fr_170px_170px_110px] items-center min-h-[50px] px-4 border-b border-line last:border-0 max-[900px]:grid-cols-[1fr_150px_100px]">
                  <div><span className="flex items-center gap-2 text-[12.5px] font-medium"><KeyRound size={14} className="text-acc2" />{c.name}</span><span className={`font-mono text-[9.5px] ${activeCredential(c) ? 'text-ok' : 'text-dim'}`}>{activeCredential(c) ? 'active' : c.revoked_at ? 'revoked' : 'expired'}</span></div>
                  <span className="font-mono text-[10.5px] text-mut max-[900px]:hidden">{fmt(c.last_used_at)}</span><span className="font-mono text-[10.5px] text-mut">{fmt(c.expires_at)}</span>
                  <span className="flex justify-end gap-1">{activeCredential(c) && <><button title="Rotate" className="p-1.5 text-mut hover:text-acc2" onClick={() => { setRotating(c); setCredentialForm({ name: `${c.name}-replacement`, expires: localDate(90) }); setCredentialModal(true); }}><RefreshCw size={14} /></button><button title="Revoke" className="p-1.5 text-mut hover:text-err" onClick={() => revokeCredential(c)}><Trash2 size={14} /></button></>}</span>
                </div>)}
                {!credentials.length && <Empty text="No credentials. Create one only when the consuming application is ready to store it securely." />}
              </Section>

              <Section title="Delegated launch grants" description="Each grant binds this identity to one workflow and inventory. Praetor resolves host IDs into the effective limit." action={<Button size="sm" icon={<Plus size={14} />} disabled={!selected.enabled} onClick={() => openGrant()}>New grant</Button>}>
                <DataHeader columns="grid-cols-[1fr_1fr_170px_125px]"><span>Workflow</span><span>Inventory scope</span><span>Validity</span><span className="text-right">Actions</span></DataHeader>
                {grants.map(g => <div key={g.id} className="grid grid-cols-[1fr_1fr_170px_125px] gap-3 items-center min-h-[62px] px-4 border-b border-line last:border-0 max-[900px]:grid-cols-[1fr_1fr_115px]">
                  <div><span className="block text-[12.5px] font-medium">{names.workflows.get(g.workflow_template_id) || `Workflow ${g.workflow_template_id}`}</span><span className="font-mono text-[9.5px] text-dim">grant {g.id}</span></div>
                  <div><span className="block text-[11.5px] text-ink2">{names.inventories.get(g.inventory_id) || `Inventory ${g.inventory_id}`}</span><span className="font-mono text-[9.5px] text-dim">{(g.allowed_host_ids || []).length} hosts · {(g.allowed_group_ids || []).length} groups · max {g.max_hosts || 'unbounded'}</span></div>
                  <div><span className={`font-mono text-[9.5px] ${activeGrant(g) ? 'text-ok' : 'text-dim'}`}>{activeGrant(g) ? 'active' : g.revoked_at ? 'revoked' : 'inactive'}</span><span className="block font-mono text-[9.5px] text-mut">to {fmt(g.expires_at)}</span></div>
                  <span className="flex justify-end gap-1">{!g.revoked_at && <><button title="Edit" className="px-2 py-1 text-[10.5px] text-mut hover:text-acc2" onClick={() => openGrant(g)}>Edit</button><button title="Revoke" className="p-1.5 text-mut hover:text-err" onClick={() => revokeGrant(g)}><Trash2 size={14} /></button></>}</span>
                </div>)}
                {!grants.length && <Empty text="No grants. This identity cannot launch anything until an administrator creates one." />}
              </Section>
            </div>}
          </>
        )}
      </main>

      <Modal isOpen={principalModal} onClose={() => { setPrincipalModal(false); setEditingPrincipal(false); }} title={editingPrincipal ? 'Edit service principal' : 'New service principal'}><div className="space-y-4"><Input label="Name" placeholder="customer-portal" value={principalForm.name} onChange={e => setPrincipalForm(f => ({ ...f, name: e.target.value }))} /><Input label="Description" placeholder="Launches approved maintenance workflows for customer hosts" value={principalForm.description} onChange={e => setPrincipalForm(f => ({ ...f, description: e.target.value }))} /><ModalActions close={() => setPrincipalModal(false)} save={savePrincipal} label={editingPrincipal ? 'Save changes' : 'Create principal'} /></div></Modal>
      <Modal isOpen={credentialModal} onClose={() => { setCredentialModal(false); setRotating(null); }} title={rotating ? 'Rotate credential' : 'New credential'}><div className="space-y-4">{rotating && <p className="rounded-lg bg-changed/10 px-3 py-2 text-[11.5px] text-changed">Rotation revokes the current credential and creates its replacement atomically.</p>}<Input label="Name" value={credentialForm.name} onChange={e => setCredentialForm(f => ({ ...f, name: e.target.value }))} /><Input label="Expires" type="date" value={credentialForm.expires} onChange={e => setCredentialForm(f => ({ ...f, expires: e.target.value }))} hint="Required. Maximum lifetime is 366 days." /><ModalActions close={() => setCredentialModal(false)} save={saveCredential} label={rotating ? 'Rotate credential' : 'Create credential'} /></div></Modal>
      <GrantModal open={grantModal} close={() => setGrantModal(false)} save={saveGrant} form={grantForm} setForm={setGrantForm} workflows={workflows} inventories={inventories} teams={teams} hosts={hosts} groups={groups} editing={!!editingGrant} />
    </div>
  );
};

function toLocalInput(value: string) { const d = new Date(value); d.setMinutes(d.getMinutes() - d.getTimezoneOffset()); return d.toISOString().slice(0, 16); }
const Status = ({ active }: { active: boolean }) => <span className={`font-mono text-[9.5px] px-2 py-0.5 rounded-md ring-1 ring-inset ${active ? 'text-ok bg-ok/10 ring-ok/20' : 'text-dim bg-white/[0.03] ring-white/10'}`}>{active ? 'enabled' : 'disabled'}</span>;
const Empty = ({ text }: { text: string }) => <p className="px-4 py-9 text-center text-[12px] text-dim">{text}</p>;
const DataHeader = ({ columns, children }: { columns: string; children: React.ReactNode }) => <div className={`grid ${columns} px-4 h-8 items-center border-b border-line bg-panel2 font-mono text-[9px] tracking-[0.1em] uppercase text-dim max-[900px]:[&>*:nth-child(2)]:hidden`}>{children}</div>;
const Section = ({ title, description, action, children }: { title: string; description: string; action: React.ReactNode; children: React.ReactNode }) => <section className="mb-8"><div className="flex items-end gap-4 mb-3"><div><h2 className="text-[14px] font-semibold">{title}</h2><p className="mt-1 text-[11.5px] text-mut">{description}</p></div><div className="ml-auto">{action}</div></div><div className="rounded-xl border border-line overflow-hidden bg-panel">{children}</div></section>;
const ModalActions = ({ close, save, label }: { close: () => void; save: () => void; label: string }) => <div className="flex justify-end gap-2 pt-1"><Button variant="secondary" onClick={close}>Cancel</Button><Button onClick={save}>{label}</Button></div>;
const SecretPanel = ({ secret, onClose, copied, onCopy }: { secret: string; onClose: () => void; copied: boolean; onCopy: () => void }) => <div className="mx-8 mt-5 rounded-xl border border-ok/25 bg-ok/[0.07] p-4"><div className="flex items-center gap-2 text-ok"><ShieldCheck size={15} /><span className="text-[12.5px] font-medium">Credential created — copy it now</span></div><p className="mt-1 text-[11px] text-mut">Praetor stores only its hash. This value cannot be displayed again.</p><div className="flex gap-2 mt-3"><code className="flex-1 min-w-0 break-all rounded-lg bg-[#070809] border border-ok/20 px-3 py-2 font-mono text-[11.5px] text-ink2">{secret}</code><Button variant="secondary" icon={copied ? <Check size={14} /> : <Copy size={14} />} onClick={onCopy}>{copied ? 'Copied' : 'Copy'}</Button><Button variant="ghost" onClick={onClose}>Dismiss</Button></div></div>;

const MultiCheck = ({ rows, selected, change }: { rows: Named[]; selected: number[]; change: (ids: number[]) => void }) => <div className="max-h-32 overflow-auto scroll-tint rounded-lg border border-line bg-panel2 p-2">{rows.map(row => <label key={row.id} className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-white/[0.03] text-[11.5px]"><input type="checkbox" checked={selected.includes(row.id)} onChange={e => change(e.target.checked ? [...selected, row.id] : selected.filter(id => id !== row.id))} />{row.name}</label>)}{!rows.length && <p className="p-2 text-[11px] text-dim">No resources available.</p>}</div>;
const SelectField = ({ label, value, onChange, rows, optional }: { label: string; value: string; onChange: (v: string) => void; rows: Named[]; optional?: boolean }) => <label className="block"><span className="block text-[12px] font-medium mb-1.5">{label}</span><select className="w-full h-9 rounded-lg border border-line2 bg-panel px-3 text-[12px]" value={value} onChange={e => onChange(e.target.value)}><option value="">{optional ? 'None' : 'Select…'}</option>{rows.map(r => <option key={r.id} value={r.id}>{r.name}</option>)}</select></label>;
const GrantModal = ({ open, close, save, form, setForm, workflows, inventories, teams, hosts, groups, editing }: any) => <Modal isOpen={open} onClose={close} title={editing ? 'Edit delegated launch grant' : 'New delegated launch grant'}><div className="space-y-4"><div className="grid grid-cols-2 gap-3"><SelectField label="Workflow" value={form.workflowId} onChange={v => setForm((f: GrantForm) => ({ ...f, workflowId: v }))} rows={workflows} /><SelectField label="Inventory" value={form.inventoryId} onChange={v => setForm((f: GrantForm) => ({ ...f, inventoryId: v, hostIds: [], groupIds: [] }))} rows={inventories} /></div><div className="grid grid-cols-2 gap-3"><div><span className="block text-[12px] font-medium mb-1.5">Allowed hosts</span><MultiCheck rows={hosts} selected={form.hostIds} change={ids => setForm((f: GrantForm) => ({ ...f, hostIds: ids }))} /></div><div><span className="block text-[12px] font-medium mb-1.5">Allowed groups</span><MultiCheck rows={groups} selected={form.groupIds} change={ids => setForm((f: GrantForm) => ({ ...f, groupIds: ids }))} /></div></div><p className="text-[10.5px] text-mut">If both lists are empty, every enabled host in the inventory is eligible. The caller still submits explicit host IDs; raw limit expressions are never accepted.</p><div className="grid grid-cols-2 gap-3"><Input label="Maximum hosts (optional)" type="number" min="1" value={form.maxHosts} onChange={e => setForm((f: GrantForm) => ({ ...f, maxHosts: e.target.value }))} /><SelectField label="Approval team" optional value={form.approvalTeamId} onChange={v => setForm((f: GrantForm) => ({ ...f, approvalTeamId: v }))} rows={teams} /></div><Input label="Allowed extra-variable keys" value={form.extraVarKeys} onChange={e => setForm((f: GrantForm) => ({ ...f, extraVarKeys: e.target.value }))} hint="Comma-separated identifiers, for example: change_ticket, environment" /><div className="grid grid-cols-2 gap-3"><Input label="Active from" type="datetime-local" value={form.notBefore} onChange={e => setForm((f: GrantForm) => ({ ...f, notBefore: e.target.value }))} /><Input label="Expires" type="datetime-local" value={form.expires} onChange={e => setForm((f: GrantForm) => ({ ...f, expires: e.target.value }))} /></div><ModalActions close={close} save={save} label={editing ? 'Update grant' : 'Create grant'} /></div></Modal>;

export default ServicePrincipalsPage;
