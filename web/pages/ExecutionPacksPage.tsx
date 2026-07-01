import React, { useEffect, useState } from 'react';
import { api } from '../services/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Package, Plus, Trash2, Loader } from 'lucide-react';

interface Pack {
  id: number;
  name: string;
  description?: string;
  spec?: string;
  status: string;
  build_log?: string;
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
  const [form, setForm] = useState({ name: '', description: '', spec: '' });

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

  const create = async () => {
    if (!form.name.trim()) return;
    try {
      await api.createExecutionPack({ name: form.name.trim(), description: form.description || null, spec: form.spec || null });
      setShowModal(false); setForm({ name: '', description: '', spec: '' }); load();
    } catch (e) { alert('Failed to create pack (name may already exist).'); }
  };
  const remove = async (id: number) => {
    if (!confirm('Delete this Execution Pack registration? (does not delete the built artifact)')) return;
    await api.deleteExecutionPack(id).catch(() => { });
    load();
  };

  if (loading) return <div className="flex items-center justify-center h-64"><Loader className="animate-spin text-brand-600" size={32} /></div>;

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-900">Execution Packs</h1>
        <Button icon={<Plus size={16} />} onClick={() => setShowModal(true)}>Register Pack</Button>
      </div>

      <Card className="bg-brand-50/40 border-brand-100">
        <p className="text-sm text-gray-600">
          An <b>Execution Pack</b> is the self-contained Python + Ansible runtime Praetor pushes onto a host at run time,
          so hosts need nothing pre-installed. Build one from a YAML spec, then register it here so templates can select it:
        </p>
        <pre className="mt-2 bg-white border border-gray-200 rounded-md p-2 text-[11px] font-mono text-gray-700 overflow-x-auto">make execpack SPEC=build/execpack/specs/docker.yml   # builds build/runtime/docker-tools-linux-&lt;arch&gt;.tar.gz</pre>
      </Card>

      <Card className="overflow-hidden">
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
                <td className="px-6 py-4 whitespace-nowrap">
                  <span className="flex items-center gap-2 text-sm font-medium text-gray-900">
                    <Package size={16} className="text-brand-600" /> {p.name}
                  </span>
                </td>
                <td className="px-6 py-4 whitespace-nowrap" title={p.status === 'failed' ? (p.build_log || '') : ''}><StatusBadge s={p.status} /></td>
                <td className="px-6 py-4 text-sm text-gray-500">{p.description || '—'}</td>
                <td className="px-6 py-4 whitespace-nowrap text-right">
                  <button onClick={() => remove(p.id)} className="text-gray-400 hover:text-red-600" title="Delete"><Trash2 size={18} /></button>
                </td>
              </tr>
            ))}
            {packs.length === 0 && <tr><td colSpan={4} className="px-6 py-8 text-center text-gray-500">No packs registered.</td></tr>}
          </tbody>
        </table>
      </Card>

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title="Register Execution Pack">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Name</label>
            <p className="text-xs text-gray-500 mb-1">Must match the built artifact: <code>&lt;name&gt;-linux-&lt;arch&gt;.tar.gz</code>.</p>
            <input className="w-full border border-gray-300 rounded-md p-2 font-mono text-sm" placeholder="docker-tools"
              value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Description</label>
            <input className="w-full border border-gray-300 rounded-md p-2" placeholder="ansible-core + community.docker + docker SDK"
              value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Spec (YAML)</label>
            <p className="text-xs text-gray-500 mb-1">Provide a spec and Praetor <b>builds the pack</b> for you (status → building → ready). Leave empty to register a pre-built artifact.</p>
            <textarea rows={6} className="w-full border border-gray-300 rounded-md p-2 font-mono text-xs"
              placeholder={'name: docker-tools\nansible: ansible-core\ncollections:\n  - community.docker'}
              value={form.spec} onChange={e => setForm({ ...form, spec: e.target.value })} />
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button onClick={create}>Register</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default ExecutionPacksPage;
