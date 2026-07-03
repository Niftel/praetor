import React, { useEffect, useState } from 'react';
import { api } from '../services/api';
import Card from '../components/ui/Card';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { KeyRound, Plus, Trash2, Copy, Check, Loader } from 'lucide-react';

interface Token {
  id: number;
  name: string;
  last_used_at?: string | null;
  expires_at?: string | null;
  created_at: string;
}

const fmt = (s?: string | null) => (s ? new Date(s).toLocaleString() : '—');

const TokensPage = () => {
  const [tokens, setTokens] = useState<Token[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [name, setName] = useState('');
  const [expires, setExpires] = useState('');
  const [newSecret, setNewSecret] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const load = () => {
    api.listTokens().then(d => setTokens(d || [])).catch(() => setTokens([])).finally(() => setLoading(false));
  };
  useEffect(load, []);

  const create = async () => {
    if (!name.trim()) return;
    try {
      const res = await api.createToken({ name: name.trim(), expires_at: expires ? new Date(expires).toISOString() : null });
      setNewSecret(res.token); // shown once
      setName(''); setExpires(''); setShowModal(false);
      load();
    } catch {
      alert('Failed to create token');
    }
  };

  const revoke = async (t: Token) => {
    if (!confirm(`Revoke token "${t.name}"? Any client using it will stop working.`)) return;
    await api.revokeToken(t.id).catch(() => {});
    load();
  };

  const copy = () => {
    if (newSecret) { navigator.clipboard?.writeText(newSecret); setCopied(true); setTimeout(() => setCopied(false), 1500); }
  };

  if (loading) return <div className="flex items-center justify-center h-64"><Loader className="animate-spin text-brand-600" size={32} /></div>;

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-900">API Tokens</h1>
        <Button icon={<Plus size={16} />} onClick={() => setShowModal(true)}>New Token</Button>
      </div>

      <Card className="bg-brand-50/40 border-brand-100">
        <p className="text-sm text-gray-600">
          A <b>personal access token</b> authenticates API calls as you (same permissions), for scripts and CI.
          Use it as <code className="bg-white border border-gray-200 rounded px-1">Authorization: Bearer &lt;token&gt;</code>.
          The secret is shown only once at creation.
        </p>
      </Card>

      {newSecret && (
        <Card className="border-green-200 bg-green-50">
          <p className="text-sm font-medium text-green-800 mb-1">New token — copy it now, it won't be shown again:</p>
          <div className="flex items-center gap-2">
            <code className="flex-1 font-mono text-sm bg-white border border-green-300 rounded px-2 py-1 break-all">{newSecret}</code>
            <button onClick={copy} className="text-green-700 hover:text-green-900 p-1" title="Copy">
              {copied ? <Check size={18} /> : <Copy size={18} />}
            </button>
            <button onClick={() => setNewSecret(null)} className="text-gray-400 hover:text-gray-600 text-sm px-2">Dismiss</button>
          </div>
        </Card>
      )}

      <Card className="overflow-hidden">
        <table className="min-w-full divide-y divide-gray-200">
          <thead className="bg-gray-50">
            <tr>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Name</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Last used</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Expires</th>
              <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Created</th>
              <th className="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
            </tr>
          </thead>
          <tbody className="bg-white divide-y divide-gray-200">
            {tokens.map(t => (
              <tr key={t.id} className="hover:bg-gray-50">
                <td className="px-6 py-4 text-sm font-medium text-gray-900 flex items-center gap-2">
                  <KeyRound size={15} className="text-brand-600" /> {t.name}
                </td>
                <td className="px-6 py-4 text-sm text-gray-500">{fmt(t.last_used_at)}</td>
                <td className="px-6 py-4 text-sm text-gray-500">{t.expires_at ? fmt(t.expires_at) : 'Never'}</td>
                <td className="px-6 py-4 text-sm text-gray-500">{fmt(t.created_at)}</td>
                <td className="px-6 py-4 text-right">
                  <button onClick={() => revoke(t)} className="text-gray-400 hover:text-red-600 p-1" title="Revoke"><Trash2 size={16} /></button>
                </td>
              </tr>
            ))}
            {tokens.length === 0 && <tr><td colSpan={5} className="px-6 py-8 text-center text-gray-500">No tokens yet.</td></tr>}
          </tbody>
        </table>
      </Card>

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title="New API Token">
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Name</label>
            <input className="w-full border border-gray-300 rounded-md p-2" placeholder="ci-pipeline"
              value={name} onChange={e => setName(e.target.value)} />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Expires (optional)</label>
            <input type="date" className="w-full border border-gray-300 rounded-md p-2"
              value={expires} onChange={e => setExpires(e.target.value)} />
            <p className="text-xs text-gray-500 mt-1">Leave blank for a token that never expires.</p>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button onClick={create}>Create</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default TokensPage;
