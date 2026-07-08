import React, { useEffect, useState } from 'react';
import { api } from '../services/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Input, Textarea } from '../components/ui/Input';
import { Package, Plus, Trash2, Loader, GitBranch, Copy, Pencil, RefreshCw } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';

interface Pack {
  id: number;
  name: string;
  description?: string;
  spec?: string;
  status: string;
  build_log?: string;
  scm_url?: string;
  scm_branch?: string;
  spec_path?: string;
  created_at: string;
}

const StatusBadge = ({ s }: { s: string }) => {
  const map: Record<string, string> = {
    ready: 'bg-green-100 text-green-700',
    building: 'bg-blue-100 text-blue-700',
    pending: 'bg-amber-100 text-amber-700',
    failed: 'bg-red-100 text-red-700',
  };
  return <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium ${map[s] || 'bg-gray-100 text-gray-600'}`}>
    {(s === 'building' || s === 'pending') && <Loader size={11} className="animate-spin" />}{s}
  </span>;
};

const ExecutionPacksPage = () => {
  const [packs, setPacks] = useState<Pack[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const blank = { name: '', description: '', spec: '', scm_url: '', scm_branch: 'main', spec_path: '', webhook_key: '' };
  const [form, setForm] = useState(blank);

  const load = () => {
    api.getExecutionPacks().then(d => setPacks(d || [])).catch(() => setPacks([])).finally(() => setLoading(false));
  };
  useEffect(() => {
    load();
    // Poll while any pack is building so status updates live.
    const h = setInterval(() => {
      setPacks(prev => {
        if (prev.some(p => p.status === 'building' || p.status === 'pending')) load();
        return prev;
      });
    }, 3000);
    return () => clearInterval(h);
  }, []);

  const openCreate = () => { setEditingId(null); setForm(blank); setShowModal(true); };
  const openEdit = (p: Pack) => {
    setEditingId(p.id);
    setForm({
      name: p.name, description: p.description || '', spec: p.spec || '',
      scm_url: p.scm_url || '', scm_branch: p.scm_branch || 'main', spec_path: p.spec_path || '',
      webhook_key: '', // never returned; blank keeps the existing secret
    });
    setShowModal(true);
  };

  const save = async () => {
    if (!form.name.trim()) return;
    const gitBacked = !!form.scm_url.trim();
    const body = {
      name: form.name.trim(),
      description: form.description || null,
      spec: form.spec || null,
      scm_url: gitBacked ? form.scm_url.trim() : null,
      scm_branch: gitBacked ? (form.scm_branch.trim() || 'main') : null,
      spec_path: gitBacked ? form.spec_path.trim() : null,
      webhook_key: gitBacked ? (form.webhook_key.trim() || null) : null,
    };
    try {
      if (editingId) await api.updateExecutionPack(editingId, body);
      else await api.createExecutionPack(body);
      setShowModal(false); setForm(blank); setEditingId(null); load();
    } catch (e) { toast.error(`Failed to ${editingId ? 'update' : 'create'} pack (name may already exist).`); }
  };
  const rebuild = async (id: number) => {
    try { await api.rebuildExecutionPack(id); load(); } catch (e: any) { toast.error(e?.message || 'Rebuild failed'); }
  };
  const remove = async (id: number) => {
    if (!(await confirmDialog('Delete this Execution Pack registration? (does not delete the built artifact)'))) return;
    await api.deleteExecutionPack(id).catch(() => { });
    load();
  };

  if (loading) return <PageSpinner />;

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-900">Execution Packs</h1>
        <Button icon={<Plus size={16} />} onClick={openCreate}>Register Pack</Button>
      </div>

      <Card className="bg-brand-50/40 border-brand-100">
        <p className="text-sm text-gray-600">
          An <b>Execution Pack</b> is the self-contained Python + Ansible runtime Praetor pushes onto a host at run time,
          so hosts need nothing pre-installed. Build one from a YAML spec, then register it here so templates can select it.
        </p>
      </Card>

      <Card className="overflow-hidden">
        <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Pack</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Description</th>
              <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {packs.map(p => (
              <tr key={p.id} className="hover:bg-gray-50">
                <td className="px-6 py-4">
                  <span className="flex items-center gap-2 text-sm font-medium text-gray-900">
                    <Package size={16} className="text-brand-600" /> {p.name}
                  </span>
                  {p.scm_url && (
                    <div className="mt-1.5 ml-6 space-y-1">
                      <div className="flex items-center gap-1 text-[11px] text-gray-500">
                        <GitBranch size={11} /> <span className="font-mono truncate max-w-[220px]">{p.scm_url}</span>
                        <span className="text-gray-300">·</span><span className="font-mono">{p.spec_path}</span>
                        <span className="text-gray-300">·</span><span>{p.scm_branch || 'main'}</span>
                      </div>
                      <div className="flex items-center gap-1">
                        <code className="text-[11px] bg-cyan-50 border border-cyan-200 rounded px-1.5 py-0.5 text-cyan-800 truncate max-w-[260px]">
                          POST …/webhooks/execution-packs/{p.id}/generic?token=…
                        </code>
                        <button title="Copy push-to-build webhook URL (add your secret as ?token=)"
                          onClick={() => navigator.clipboard?.writeText(`${window.location.origin}/api/v1/webhooks/execution-packs/${p.id}/generic?token=YOUR_SECRET`)}
                          className="text-gray-400 hover:text-brand-600"><Copy size={12} /></button>
                      </div>
                    </div>
                  )}
                </td>
                <td className="px-6 py-4 whitespace-nowrap" title={p.status === 'failed' ? (p.build_log || '') : ''}><StatusBadge s={p.status} /></td>
                <td className="px-6 py-4 text-sm text-gray-500">{p.description || '—'}</td>
                <td className="px-6 py-4 whitespace-nowrap text-right">
                  <div className="inline-flex items-center gap-1">
                    {/* Rebuild only applies to packs Praetor builds (a spec or a git source);
                        pre-built artifacts have nothing to rebuild. */}
                    {(p.spec || p.scm_url) && (
                      <button onClick={() => rebuild(p.id)} disabled={p.status === 'building' || p.status === 'pending'}
                        className="text-gray-400 hover:text-brand-600 disabled:opacity-30 p-1" title="Rebuild now"><RefreshCw size={16} /></button>
                    )}
                    <button onClick={() => openEdit(p)} className="text-gray-400 hover:text-brand-600 p-1" title="Edit"><Pencil size={16} /></button>
                    <button onClick={() => remove(p.id)} className="text-gray-400 hover:text-red-600 p-1" title="Delete"><Trash2 size={16} /></button>
                  </div>
                </td>
              </tr>
            ))}
            {packs.length === 0 && <tr><td colSpan={4} className="px-6 py-8 text-center text-gray-500">No packs registered.</td></tr>}
          </tbody>
        </table>
        </div>
      </Card>

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title={editingId ? 'Edit Execution Pack' : 'Register Execution Pack'}>
        <div className="space-y-4">
          <Input label="Name" className="font-mono text-sm" placeholder="docker-tools"
            hint="Must match the built artifact: <name>-linux-<arch>.tar.gz"
            value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />
          <Input label="Description" placeholder="ansible-core + community.docker + docker SDK"
            value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} />
          <Textarea label="Spec (YAML)" rows={6} className="font-mono text-xs"
            hint="Provide a spec and Praetor builds the pack for you (status → building → ready). Leave empty to register a pre-built artifact."
            placeholder={'name: docker-tools\nansible: ansible-core\ncollections:\n  - community.docker'}
            value={form.spec} onChange={e => setForm({ ...form, spec: e.target.value })} />

          {/* Git-backed: pull the spec from a repo; a push webhook rebuilds it. */}
          <div className="border border-gray-200 rounded-md p-3 bg-gray-50 space-y-3">
            <div className="flex items-center gap-2 text-sm font-medium text-gray-700">
              <GitBranch size={14} className="text-cyan-600" /> Git source (optional)
            </div>
            <p className="text-xs text-gray-500 -mt-1">Point the pack at a repo + the path to its YAML. Praetor pulls the spec and builds; add the webhook below to your git host so a <b>push rebuilds the pack</b>. Leave blank to manage the spec inline above.</p>
            <div className="grid grid-cols-3 gap-2">
              <Input wrapperClassName="col-span-2" label="Repo URL" className="font-mono text-xs" placeholder="https://gitea.local/me/packs.git"
                value={form.scm_url} onChange={e => setForm({ ...form, scm_url: e.target.value })} />
              <Input label="Branch" className="font-mono text-xs" placeholder="main"
                value={form.scm_branch} onChange={e => setForm({ ...form, scm_branch: e.target.value })} />
            </div>
            <Input label="Spec path" className="font-mono text-xs" placeholder="path/in/repo/docker.yml"
              value={form.spec_path} onChange={e => setForm({ ...form, spec_path: e.target.value })} />
            <Input label="Webhook secret" className="font-mono text-xs"
              placeholder={editingId ? 'webhook secret (leave blank to keep current)' : 'webhook secret (token) for the push trigger'}
              value={form.webhook_key} onChange={e => setForm({ ...form, webhook_key: e.target.value })} />
          </div>

          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button onClick={save}>{editingId ? 'Save changes' : 'Register'}</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default ExecutionPacksPage;
