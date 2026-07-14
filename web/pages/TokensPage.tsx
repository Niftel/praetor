import React, { useEffect, useState } from 'react';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Input } from '../components/ui/Input';
import { api } from '../services/api';
import { KeyRound, Plus, Trash2, Copy, Check, X } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';

interface Token { id: number; name: string; last_used_at?: string | null; expires_at?: string | null; created_at: string; }
const fmt = (s?: string | null) => (s ? new Date(s).toLocaleString() : '—');

const TokensPage = () => {
  const [tokens, setTokens] = useState<Token[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [name, setName] = useState('');
  const [expires, setExpires] = useState('');
  const [newSecret, setNewSecret] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const load = () => api.listTokens().then(d => setTokens(d || [])).catch(() => setTokens([])).finally(() => setLoading(false));
  useEffect(() => { load(); }, []);

  const create = async () => {
    if (!name.trim()) return;
    try {
      const res = await api.createToken({ name: name.trim(), expires_at: expires ? new Date(expires).toISOString() : null });
      setNewSecret(res.token); setName(''); setExpires(''); setShowModal(false); load();
    } catch { toast.error('Failed to create token'); }
  };
  const revoke = async (t: Token) => {
    if (!(await confirmDialog(`Revoke token "${t.name}"? Any client using it will stop working.`, { destructive: true, confirmText: 'Revoke' }))) return;
    await api.revokeToken(t.id).catch(() => { }); load();
  };
  const copy = () => { if (newSecret) { navigator.clipboard?.writeText(newSecret); setCopied(true); setTimeout(() => setCopied(false), 1500); } };

  if (loading) return <PageSpinner />;

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink">
      <div className="flex items-center gap-4 px-8 pt-6 pb-4 shrink-0">
        <div>
          <h1 className="text-[19px] font-semibold tracking-tight">API Tokens</h1>
          <p className="text-[12.5px] text-mut mt-0.5">Personal access tokens authenticate API calls as you — for scripts &amp; CI. Sent as <span className="font-mono text-ink2">Authorization: Bearer &lt;token&gt;</span>.</p>
        </div>
        <Button className="ml-auto" icon={<Plus size={15} />} onClick={() => setShowModal(true)}>New token</Button>
      </div>

      <div className="px-8 pb-4 max-w-[980px] w-full">
        {newSecret && (
          <div className="rounded-xl border border-ok/30 bg-ok/[0.07] px-4 py-3 mb-4">
            <p className="text-[12.5px] font-medium text-ok mb-2">New token — copy it now, it won't be shown again:</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 font-mono text-[12.5px] bg-[#070809] border border-ok/25 rounded-lg px-2.5 py-1.5 break-all text-ink2">{newSecret}</code>
              <button onClick={copy} className="p-1.5 rounded text-ok hover:bg-ok/10" title="Copy">{copied ? <Check size={17} /> : <Copy size={17} />}</button>
              <button onClick={() => setNewSecret(null)} className="p-1.5 rounded text-dim hover:text-ink" title="Dismiss"><X size={17} /></button>
            </div>
          </div>
        )}

        <div className="rounded-xl border border-line overflow-hidden">
          <div className="grid grid-cols-[1fr_170px_140px_170px_60px] items-center px-5 h-[34px] border-b border-line bg-panel2 font-mono text-[9.5px] tracking-[0.1em] uppercase text-dim max-[720px]:grid-cols-[1fr_120px_50px]">
            <span>Name</span><span className="max-[720px]:hidden">Last used</span><span>Expires</span><span className="max-[720px]:hidden">Created</span><span className="text-right">·</span>
          </div>
          {tokens.map(t => (
            <div key={t.id} className="grid grid-cols-[1fr_170px_140px_170px_60px] items-center px-5 h-[48px] border-b border-line last:border-0 hover:bg-white/[0.02] max-[720px]:grid-cols-[1fr_120px_50px]">
              <span className="flex items-center gap-2 text-[13px] font-medium text-ink"><KeyRound size={15} className="text-acc2" /> {t.name}</span>
              <span className="font-mono text-[11.5px] text-mut max-[720px]:hidden">{fmt(t.last_used_at)}</span>
              <span className="font-mono text-[11.5px] text-mut">{t.expires_at ? fmt(t.expires_at) : 'never'}</span>
              <span className="font-mono text-[11.5px] text-mut max-[720px]:hidden">{fmt(t.created_at)}</span>
              <span className="text-right"><button onClick={() => revoke(t)} className="p-1.5 rounded text-faint hover:text-err hover:bg-white/5" title="Revoke"><Trash2 size={15} /></button></span>
            </div>
          ))}
          {tokens.length === 0 && <p className="px-5 py-10 text-center text-sm text-dim">No tokens yet. Create one for a script or CI job.</p>}
        </div>
      </div>

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title="New API token">
        <div className="space-y-4">
          <Input label="Name" placeholder="ci-pipeline" value={name} onChange={e => setName(e.target.value)} />
          <Input label="Expires (optional)" type="date" value={expires} onChange={e => setExpires(e.target.value)} hint="Leave blank for a token that never expires." />
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button><Button onClick={create}>Create</Button></div>
        </div>
      </Modal>
    </div>
  );
};

export default TokensPage;
