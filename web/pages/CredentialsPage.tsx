import React, { useState, useEffect, useMemo } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Credential, CredentialType } from '../types';
import { Input, Textarea, Select } from '../components/ui/Input';
import { FormActions, FormErrorSummary, FormSection, SecretField, useDirtyFormGuard } from '../components/ui';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Key, Lock, Plus, ArrowLeft, Cloud, GitBranch, Shield, Server, Pencil, Trash2 } from 'lucide-react';
import { PageSpinner } from '../components/ui/PageSpinner';
import { toast, confirmDialog } from '../components/ui/toast';

interface Field { id: string; label?: string; type?: string; secret?: boolean; multiline?: boolean; }

const kindIcon = (typeName: string, size = 15) => {
  const n = typeName.toLowerCase();
  if (n.includes('cloud') || n.includes('aws') || n.includes('gcp') || n.includes('azure')) return <Cloud size={size} />;
  if (n.includes('source') || n.includes('git') || n.includes('scm')) return <GitBranch size={size} />;
  if (n.includes('vault')) return <Shield size={size} />;
  if (n.includes('machine') || n.includes('ssh')) return <Key size={size} />;
  return <Server size={size} />;
};

const CredentialsPage = () => {
  const { orgId: orgIdStr } = useParams();
  const orgId = Number(orgIdStr);
  const [orgName, setOrgName] = useState('');
  const [credentials, setCredentials] = useState<Credential[]>([]);
  const [credentialTypes, setCredentialTypes] = useState<CredentialType[]>([]);
  const [templates, setTemplates] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedId, setSelectedId] = useState<number | null>(null);

  const [modalOpen, setModalOpen] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [typeId, setTypeId] = useState<number | null>(null);
  const [formFields, setFormFields] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [formErrors, setFormErrors] = useState<string[]>([]);
  const [initialForm, setInitialForm] = useState('');

  useEffect(() => {
    (async () => {
      try {
        setLoading(true);
        const [creds, types, tpls, orgs] = await Promise.all([
          api.getCredentials(), api.getCredentialTypes(), api.getTemplates().catch(() => []), api.getOrganizations().catch(() => []),
        ]);
        const list = unwrap<Credential>(creds).filter(c => (c as any).organization_id === orgId);
        setCredentials(list);
        setCredentialTypes(types || []);
        setTemplates(unwrap(tpls));
        setSelectedId(prev => prev ?? (list[0]?.id ?? null));
        setOrgName(unwrap<{ id: number; name: string }>(orgs).find(o => o.id === orgId)?.name ?? `Org ${orgId}`);
      } catch (err) { console.error('Failed to load credentials', err); }
      finally { setLoading(false); }
    })();
  }, [orgId]);

  const typeOf = (c?: Credential | null) => credentialTypes.find(t => t.id === c?.credential_type_id);
  const typeName = (c?: Credential | null) => typeOf(c)?.name || 'Credential';
  const fieldsOf = (t?: CredentialType): Field[] => {
    if (!t?.inputs) return [];
    try { const s = typeof t.inputs === 'string' ? JSON.parse(t.inputs) : t.inputs; return s.fields || []; } catch { return []; }
  };

  // Group credentials by their type (the "keyring by kind").
  const groups = useMemo(() => {
    const m = new Map<string, Credential[]>();
    for (const c of credentials) { const k = typeName(c); if (!m.has(k)) m.set(k, []); m.get(k)!.push(c); }
    return [...m.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [credentials, credentialTypes]);

  const usedByCount = (id: number) => templates.filter(t => t.credential_id === id).length;

  const formSnapshot = (nextName = name, nextDescription = description, nextTypeId = typeId, nextFields = formFields) => JSON.stringify({ name: nextName, description: nextDescription, typeId: nextTypeId, fields: nextFields });
  const beginForm = (nextEditingId: number | null, nextName: string, nextDescription: string, nextTypeId: number | null) => {
    setEditingId(nextEditingId); setName(nextName); setDescription(nextDescription); setTypeId(nextTypeId); setFormFields({}); setFormErrors([]); setSaving(false);
    setInitialForm(formSnapshot(nextName, nextDescription, nextTypeId, {})); setModalOpen(true);
  };
  const openCreate = () => beginForm(null, '', '', credentialTypes[0]?.id ?? null);
  const openEdit = (c: Credential) => beginForm(c.id, c.name, c.description || '', c.credential_type_id);
  const dirty = modalOpen && formSnapshot() !== initialForm;
  const canDiscard = useDirtyFormGuard(dirty);
  const closeForm = async () => { if (saving || !(await canDiscard())) return; setModalOpen(false); setFormErrors([]); };

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (saving) return;
    const errors: string[] = [];
    if (!name.trim()) errors.push('Name is required.');
    if (!typeId) errors.push('Credential type is required.');
    setFormErrors(errors);
    if (errors.length) return;
    // Only send the fields the operator actually filled (secrets are write-only;
    // blank = keep existing on edit).
    const inputs: Record<string, string> = {};
    for (const [k, v] of Object.entries(formFields)) if (v.trim() !== '') inputs[k] = v;
    const body: any = { name: name.trim(), description: description || '', credential_type_id: typeId, organization_id: orgId, inputs };
    setSaving(true);
    try {
      if (editingId) { const u = await api.updateCredential(editingId, body); setCredentials(cs => cs.map(c => c.id === editingId ? u : c)); toast.success('Credential updated'); }
      else { const c = await api.createCredential(body); setCredentials(cs => [...cs, c]); setSelectedId(c.id); toast.success('Credential created'); }
      setModalOpen(false);
    } catch { setFormErrors([`Praetor could not ${editingId ? 'update' : 'create'} this credential. No changes were saved.`]); }
    finally { setSaving(false); }
  };

  const remove = async (id: number) => {
    if (!(await confirmDialog('Delete this credential?', { destructive: true, confirmText: 'Delete' }))) return;
    try { await api.deleteCredential(id); setCredentials(cs => cs.filter(c => c.id !== id)); if (selectedId === id) setSelectedId(null); }
    catch { toast.error('Failed to delete credential'); }
  };

  const selected = credentials.find(c => c.id === selectedId) || null;
  const selType = typeOf(selected);
  const selFields = fieldsOf(selType);
  const selInputs: Record<string, any> = (selected?.inputs as any) || {};
  const usedByTemplates = selected ? templates.filter(t => t.credential_id === selected.id) : [];
  const modalType = credentialTypes.find(t => t.id === typeId);

  if (loading) return <PageSpinner />;

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink">
      <div className="grid grid-cols-[300px_1fr] flex-1 min-h-0 max-[820px]:grid-cols-1">
        {/* Keyring */}
        <div className="flex flex-col min-h-0 border-r border-line bg-tree max-[820px]:hidden">
          <div className="flex items-center gap-2.5 h-[52px] px-4 border-b border-line shrink-0">
            <Link to="/credentials" className="text-mut hover:text-ink" title="All organizations"><ArrowLeft size={16} /></Link>
            <span className="text-[14px] font-semibold">Credentials</span>
            <span className="font-mono text-[11px] text-dim">{credentials.length}</span>
            <button onClick={openCreate} className="ml-auto flex items-center gap-1.5 h-7 px-2.5 rounded-md border border-line2 text-[11.5px] font-medium text-ink2 hover:bg-panel"><Plus size={13} /> Add</button>
          </div>
          <div className="flex-1 overflow-auto scroll-tint px-2.5 pb-6">
            {groups.map(([kind, creds]) => (
              <div key={kind} className="mt-3">
                <div className="flex items-center gap-2.5 h-6 px-2.5">
                  <span className="text-dim">{kindIcon(kind, 13)}</span>
                  <span className="font-mono text-[9px] tracking-[0.14em] uppercase text-dim">{kind}</span>
                  <span className="ml-auto font-mono text-[10px] text-faint">{creds.length}</span>
                </div>
                {creds.map(c => {
                  const sel = c.id === selectedId;
                  const used = usedByCount(c.id);
                  return (
                    <button key={c.id} onClick={() => setSelectedId(c.id)} className={`w-full flex items-center gap-3 h-[38px] px-2.5 rounded-lg ${sel ? 'bg-acc/[0.09]' : 'hover:bg-white/[0.028]'}`}>
                      <span className={sel ? 'text-acc2' : 'text-mut'}>{kindIcon(kind)}</span>
                      <span className="flex-1 min-w-0 text-left">
                        <span className={`block font-mono text-[12.5px] truncate ${sel ? 'text-ink' : 'text-ink2'}`}>{c.name}</span>
                        <span className="block font-mono text-[10px] text-faint">{used ? `used by ${used}` : 'unused'}</span>
                      </span>
                      <Lock size={12} className="text-faint shrink-0" />
                    </button>
                  );
                })}
              </div>
            ))}
            {credentials.length === 0 && <p className="px-3 py-8 text-[12px] text-dim text-center">No credentials. Add one to store a secret.</p>}
          </div>
        </div>

        {/* Sealed-secret detail */}
        {selected ? (
          <div className="flex flex-col min-h-0 bg-bg">
            <div className="flex items-start gap-4 px-9 pt-6 pb-5 border-b border-line shrink-0 max-[820px]:px-5">
              <div>
                <div className="font-mono text-[21px] font-semibold tracking-tight">{selected.name}</div>
                <div className="mt-2.5 flex items-center gap-2.5">
                  <span className="font-mono text-[11.5px] text-dim">{typeName(selected)}</span>
                  <span className="inline-flex items-center gap-1.5 h-[22px] px-2.5 rounded-md font-mono text-[10.5px] text-ink2 border border-line2"><Lock size={12} /> encrypted</span>
                </div>
              </div>
              <div className="ml-auto flex items-center gap-2.5 pt-0.5">
                <button onClick={() => openEdit(selected)} className="h-[34px] px-3.5 rounded-lg text-[12.5px] font-medium flex items-center gap-1.5 border border-line2 text-ink2 hover:border-white/25"><Pencil size={13} /> Edit</button>
                <button onClick={() => remove(selected.id)} className="w-[34px] h-[34px] grid place-items-center rounded-lg text-dim hover:text-err hover:bg-white/5" title="Delete"><Trash2 size={15} /></button>
              </div>
            </div>

            <div className="flex-1 overflow-auto scroll-tint px-9 pb-16 max-[820px]:px-5">
              <div className="max-w-[620px]">
                <Sec title="Inputs" hint="secrets are write-only">
                  {selFields.length === 0 && <p className="font-mono text-[12px] text-faint py-1">This credential type declares no input fields.</p>}
                  {selFields.map(f => {
                    const val = selInputs[f.id];
                    return (
                      <div key={f.id} className="grid grid-cols-[158px_1fr] gap-5 items-center py-2.5 border-b border-line last:border-0 group">
                        <span className="font-mono text-[12px] text-dim">{f.label || f.id}</span>
                        <span className="font-mono text-[12.5px] flex items-center gap-3 min-w-0">
                          {f.secret ? (
                            <>
                              <span className="text-faint tracking-[2px] truncate">••••••••••••••••</span>
                              <span className="inline-flex items-center gap-1.5 font-mono text-[9.5px] text-acc2 whitespace-nowrap"><Lock size={11} /> sealed</span>
                            </>
                          ) : (
                            <span className="text-ink truncate">{val !== undefined && val !== '' ? String(val) : <span className="text-faint">stored</span>}</span>
                          )}
                          <button onClick={() => openEdit(selected)} className="ml-auto font-mono text-[10.5px] text-dim hover:text-acc opacity-0 group-hover:opacity-100">replace</button>
                        </span>
                      </div>
                    );
                  })}
                </Sec>

                <Sec title="Security">
                  <KV k="encryption" v="encrypted at rest" />
                  <KV k="scope" v={`org · ${orgName}`} />
                  {(selected as any).created_at && <KV k="created" v={new Date((selected as any).created_at).toLocaleDateString()} />}
                </Sec>

                <Sec title="Used by" hint={`${usedByTemplates.length} template${usedByTemplates.length === 1 ? '' : 's'}`}>
                  {usedByTemplates.length === 0 ? <p className="font-mono text-[11.5px] text-dim">No templates use this credential.</p> : (
                    <div className="flex flex-wrap gap-2">
                      {usedByTemplates.map(t => <span key={t.id} className="font-mono text-[12px] text-ink2 border border-line rounded-lg px-2.5 py-1.5">{t.name}</span>)}
                    </div>
                  )}
                </Sec>
              </div>
            </div>
          </div>
        ) : (
          <div className="grid place-items-center text-dim">
            <div className="text-center"><Key size={38} className="mx-auto mb-3 opacity-20" /><p className="text-sm mb-4">No credentials in {orgName} yet.</p><Button icon={<Plus size={15} />} onClick={openCreate}>Add credential</Button></div>
          </div>
        )}
      </div>

      <Modal isOpen={modalOpen} onClose={() => { void closeForm(); }} title={editingId ? 'Edit credential' : `New credential in ${orgName}`}>
        <form onSubmit={save} className="space-y-4">
          <FormErrorSummary errors={formErrors} />
          <FormSection>
            <Input label="Name" required value={name} error={formErrors.includes('Name is required.') ? 'Enter a credential name.' : undefined} onChange={e => setName(e.target.value)} />
            <Input label="Description" value={description} onChange={e => setDescription(e.target.value)} />
            <Select label="Type" value={typeId || ''} disabled={!!editingId} error={formErrors.includes('Credential type is required.') ? 'Choose a credential type.' : undefined} onChange={e => { setTypeId(Number(e.target.value)); setFormFields({}); }}>
              {credentialTypes.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
            </Select>
          </FormSection>
          <FormSection title="Credential inputs" description={editingId ? 'Stored secrets remain sealed. Enter a replacement only when you intend to rotate a value.' : 'Secret values are encrypted and cannot be retrieved after saving.'}>
            {fieldsOf(modalType).map(f => {
              const isTextarea = f.type === 'textarea' || f.multiline;
              const label = f.label || f.id;
              const value = formFields[f.id] || '';
              const onChange = (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setFormFields({ ...formFields, [f.id]: e.target.value });
              if (f.secret) return <SecretField key={f.id} label={label} multiline={isTextarea} placeholder={isTextarea ? '-----BEGIN OPENSSH PRIVATE KEY-----\n...' : undefined} value={value} onChange={onChange} />;
              return isTextarea ? (
                <Textarea key={f.id} label={label} rows={6} className="font-mono text-xs" value={value} onChange={onChange} />
              ) : (
                <Input key={f.id} label={label} value={value} onChange={onChange} />
              );
            })}
          </FormSection>
          <FormActions onCancel={() => { void closeForm(); }} submitting={saving} submitLabel="Save credential" />
        </form>
      </Modal>
    </div>
  );
};

const Sec: React.FC<{ title: string; hint?: string; children: React.ReactNode }> = ({ title, hint, children }) => (
  <div className="py-6 border-t border-line first:border-t-0 first:pt-2">
    <div className="flex items-baseline gap-2.5 mb-4"><span className="font-mono text-[10px] tracking-[0.16em] uppercase text-mut">{title}</span>{hint && <span className="font-mono text-[9.5px] text-faint">{hint}</span>}</div>
    {children}
  </div>
);

const KV: React.FC<{ k: string; v: React.ReactNode }> = ({ k, v }) => (
  <div className="flex justify-between gap-4 py-2.5 border-b border-line last:border-0 font-mono text-[12px]"><span className="text-dim">{k}</span><span className="text-ink2">{v}</span></div>
);

export default CredentialsPage;
