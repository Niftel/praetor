import React, { useEffect, useMemo, useState } from 'react';
import { api, unwrap } from '../services/api';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Input, Textarea } from '../components/ui/Input';
import {
  Plus, Trash2, Loader, GitBranch, Copy, Pencil, RefreshCw, ChevronDown, ChevronRight,
  Package, Boxes, FileCode, Layers, Terminal, Cpu,
} from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { PageSpinner } from '../components/ui/PageSpinner';

interface Pack {
  id: number; name: string; description?: string; spec?: string; status: string;
  build_log?: string; scm_url?: string; scm_branch?: string; spec_path?: string; created_at: string;
}

// Derive a pack's real bill of materials from its own YAML spec (see
// pkg/packspec.Spec) — the honest contents of the tarball Praetor pushes:
// a standalone CPython, the Ansible engine, the bundled host-runner daemon,
// the target arch, and any extra pip deps. (Project collections are installed
// from requirements.yml at run time, not baked in — see host-runner/galaxy.go.)
interface SpecContents {
  python?: string;
  ansible?: string;
  hostRunner?: string;
  arches: string[];
  collections: { name: string; version?: string }[];
  pip: { name: string; version?: string }[];
}
const parseSpec = (spec?: string): SpecContents => {
  const out: SpecContents = { arches: [], collections: [], pip: [] };
  if (!spec) return out;
  let mode: 'collections' | 'pip' | 'arches' | null = null;
  const unquote = (s: string) => s.trim().replace(/^["']|["']$/g, '');
  const parseItem = (s: string) => {
    const m = s.match(/^-\s*(.+?)(?:[=:]{1,2}\s*([\w.]+))?\s*$/);
    if (!m) return null;
    const nm = unquote(m[1].replace(/:$/, ''));
    return { name: nm, version: m[2] };
  };
  for (const raw of spec.split('\n')) {
    const line = raw.replace(/\s+$/, '');
    const indent = line.match(/^\s*/)?.[0].length ?? 0;
    const t = line.trim();
    if (!t || t.startsWith('#')) continue;
    // List openers (key with nothing after the colon).
    if (/^collections\s*:\s*$/.test(t)) { mode = 'collections'; continue; }
    if (/^(pip|python_packages|packages)\s*:\s*$/.test(t)) { mode = 'pip'; continue; }
    if (/^arches\s*:\s*$/.test(t)) { mode = 'arches'; continue; }
    // List items under the active opener.
    if (t.startsWith('-') && mode) {
      if (mode === 'arches') { const m = t.replace(/\s+#.*$/, '').match(/^-\s*(.+)$/); if (m) out.arches.push(unquote(m[1])); }
      else { const item = parseItem(t.replace(/\s+#.*$/, '')); if (item?.name) (mode === 'collections' ? out.collections : out.pip).push(item); }
      continue;
    }
    // Scalar `key: value` (top-level), trailing comments stripped.
    const sm = t.replace(/\s+#.*$/, '').match(/^([A-Za-z_]+)\s*:\s*(.+)$/);
    if (indent === 0 && sm) {
      const key = sm[1].toLowerCase(); const val = unquote(sm[2]);
      if (key === 'python') out.python = val;
      else if (key === 'ansible' || key === 'ansible_core') out.ansible = val;
      else if (key === 'host_runner') out.hostRunner = val;
      mode = null;
      continue;
    }
    if (indent === 0 && t.includes(':')) mode = null;
  }
  return out;
};

const dot = (s: string) => s === 'ready' ? 'bg-ok' : s === 'building' || s === 'pending' ? 'bg-run' : s === 'failed' ? 'bg-err' : 'bg-dim';
const verColor = (s: string) => s === 'building' || s === 'pending' ? 'text-run' : s === 'failed' ? 'text-err' : 'text-dim';

const ExecutionPacksPage = () => {
  const [packs, setPacks] = useState<Pack[]>([]);
  const [templates, setTemplates] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const [showLog, setShowLog] = useState(false);

  const [showModal, setShowModal] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const blank = { name: '', description: '', spec: '', scm_url: '', scm_branch: 'main', spec_path: '', webhook_key: '' };
  const [form, setForm] = useState(blank);

  const load = () => {
    api.getExecutionPacks().then(d => {
      const list: Pack[] = d || [];
      setPacks(list);
      setSelectedId(prev => prev ?? (list[0]?.id ?? null));
    }).catch(() => setPacks([])).finally(() => setLoading(false));
  };
  useEffect(() => {
    load();
    api.getTemplates().then(t => setTemplates(unwrap(t))).catch(() => { });
    const h = setInterval(() => { setPacks(prev => { if (prev.some(p => p.status === 'building' || p.status === 'pending')) load(); return prev; }); }, 3000);
    return () => clearInterval(h);
  }, []);

  const openCreate = () => { setEditingId(null); setForm(blank); setShowModal(true); };
  const openEdit = (p: Pack) => {
    setEditingId(p.id);
    setForm({ name: p.name, description: p.description || '', spec: p.spec || '', scm_url: p.scm_url || '', scm_branch: p.scm_branch || 'main', spec_path: p.spec_path || '', webhook_key: '' });
    setShowModal(true);
  };
  const save = async () => {
    if (!form.name.trim()) return;
    const gitBacked = !!form.scm_url.trim();
    const body = {
      name: form.name.trim(), description: form.description || null, spec: form.spec || null,
      scm_url: gitBacked ? form.scm_url.trim() : null, scm_branch: gitBacked ? (form.scm_branch.trim() || 'main') : null,
      spec_path: gitBacked ? form.spec_path.trim() : null, webhook_key: gitBacked ? (form.webhook_key.trim() || null) : null,
    };
    try { if (editingId) await api.updateExecutionPack(editingId, body); else await api.createExecutionPack(body); setShowModal(false); setForm(blank); setEditingId(null); load(); }
    catch { toast.error(`Failed to ${editingId ? 'update' : 'register'} pack (name may already exist).`); }
  };
  const rebuild = async (id: number) => { try { await api.rebuildExecutionPack(id); toast.info('Rebuild started'); load(); } catch (e: any) { toast.error(e?.message || 'Rebuild failed'); } };
  const remove = async (id: number) => {
    if (!(await confirmDialog('Delete this pack registration? (does not delete the built artifact)', { destructive: true, confirmText: 'Delete' }))) return;
    await api.deleteExecutionPack(id).catch(() => { });
    if (selectedId === id) setSelectedId(null);
    load();
  };
  const toggle = (id: number) => setExpanded(p => { const n = new Set(p); n.has(id) ? n.delete(id) : n.add(id); return n; });

  const selected = packs.find(p => p.id === selectedId) || null;
  const selContents = useMemo(() => parseSpec(selected?.spec), [selected]);
  const selHasContents = !!(selContents.python || selContents.ansible || selContents.hostRunner || selContents.arches.length || selContents.collections.length || selContents.pip.length);
  const usedBy = useMemo(() => selected ? templates.filter(t => t.execution_pack_id === selected.id) : [], [templates, selected]);
  const copy = (text: string) => { navigator.clipboard?.writeText(text); toast.info('Copied'); };

  if (loading) return <PageSpinner />;

  return (
    <div className="flex flex-col h-full min-h-0 bg-bg text-ink">
      <div className="flex items-center gap-4 h-[54px] px-8 border-b border-line shrink-0">
        <span className="text-[15px] font-semibold tracking-tight">Execution Packs</span>
        <span className="text-[12px] text-dim max-w-[460px] max-[900px]:hidden">Self-contained runtimes Praetor pushes to a host at run time — nothing pre-installed.</span>
        <Button size="sm" className="ml-auto" icon={<Plus size={15} />} onClick={openCreate}>Register pack</Button>
      </div>

      {packs.length === 0 ? (
        <div className="flex-1 grid place-items-center text-dim">
          <div className="text-center"><Package size={38} className="mx-auto mb-3 opacity-20" /><p className="text-sm mb-4">No packs registered.</p><Button icon={<Plus size={15} />} onClick={openCreate}>Register pack</Button></div>
        </div>
      ) : (
        <div className="flex-1 min-h-0 overflow-auto scroll-tint flex flex-col">
          <div className="m-auto w-full max-w-[1160px] px-8 py-10 grid grid-cols-[1.5fr_1fr] gap-11 max-[900px]:grid-cols-1 max-[900px]:gap-6">
          {/* LEFT: pack explorer */}
          <div>
            <div className="font-mono text-[10px] tracking-[0.16em] uppercase text-mut mb-4 flex items-baseline gap-2.5">Packs <span className="text-faint tracking-normal">{packs.length} registered</span></div>
            <div className="rounded-xl border border-line bg-panel2 p-1.5">
              {packs.map(p => {
                const isSel = p.id === selectedId;
                const isOpen = expanded.has(p.id);
                const contents = parseSpec(p.spec);
                const hasContents = !!(contents.python || contents.ansible || contents.hostRunner || contents.arches.length || contents.collections.length || contents.pip.length);
                return (
                  <div key={p.id}>
                    <div className={`flex items-center gap-2.5 h-[34px] px-2.5 rounded-lg cursor-pointer ${isSel ? 'bg-acc/[0.09]' : 'hover:bg-white/[0.03]'}`}
                      onClick={() => setSelectedId(p.id)}>
                      <button onClick={e => { e.stopPropagation(); toggle(p.id); }} className="text-dim hover:text-ink transition-colors">
                        {isOpen ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                      </button>
                      <span className={`w-[7px] h-[7px] rounded-full shrink-0 ${dot(p.status)} ${p.status === 'building' ? 'animate-pulse' : ''}`} />
                      <span className={`font-mono text-[13px] font-semibold ${isSel ? 'text-ink' : 'text-ink2'}`}>{p.name}</span>
                      <span className={`font-mono text-[10.5px] ${verColor(p.status)}`}>{p.status === 'ready' ? '' : p.status}</span>
                      <span className="ml-auto font-mono text-[11px] text-dim">{p.scm_url ? 'git' : p.spec ? 'spec' : 'prebuilt'}</span>
                    </div>
                    {isOpen && hasContents && (
                      <div className="ml-4 pl-4 border-l border-line py-1">
                        {contents.python && <ContentRow icon={<Boxes size={14} />} name="python" ver={contents.python} />}
                        {contents.ansible && <ContentRow icon={<Layers size={14} />} name="ansible" ver={contents.ansible} />}
                        {contents.hostRunner && <ContentRow icon={<Terminal size={14} />} name="praetor-host-runner" ver={contents.hostRunner} />}
                        {contents.collections.map((c, i) => <ContentRow key={`c${i}`} icon={<Boxes size={14} />} name={c.name} ver={c.version} />)}
                        {contents.pip.map((c, i) => <ContentRow key={`p${i}`} icon={<FileCode size={14} />} name={c.name} ver={c.version} />)}
                        {contents.arches.length > 0 && <ContentRow icon={<Cpu size={14} />} name="arch" ver={contents.arches.join(', ')} />}
                      </div>
                    )}
                    {isOpen && !hasContents && <div className="ml-8 py-1.5 font-mono text-[11px] text-faint">{p.scm_url ? `contents defined in ${p.spec_path || 'repo spec'}` : 'pre-built artifact'}</div>}
                  </div>
                );
              })}
            </div>
          </div>

          {/* RIGHT: selected detail */}
          {selected && (
            <div className="min-w-0">
              <div className="flex items-center gap-3 mb-6">
                <span className="font-mono text-[16px] font-semibold tracking-tight">{selected.name}</span>
                <span className={`inline-flex items-center gap-1.5 h-[22px] px-2.5 rounded-md font-mono text-[10.5px] uppercase tracking-[0.06em] ${selected.status === 'ready' ? 'text-ok bg-ok/10' : selected.status === 'failed' ? 'text-err bg-err/10' : 'text-run bg-run/10'}`}>
                  <span className={`w-[7px] h-[7px] rounded-full ${dot(selected.status)}`} />{selected.status}
                </span>
                <span className="font-mono text-[11px] text-dim ml-auto">registered {new Date(selected.created_at).toLocaleDateString()}</span>
              </div>

              {/* Build & source */}
              <Rsec title="Build & source" action={(selected.spec || selected.scm_url) && (
                <button onClick={() => rebuild(selected.id)} disabled={selected.status === 'building' || selected.status === 'pending'}
                  className="inline-flex items-center gap-1.5 h-[27px] px-3 rounded-md text-[11.5px] font-semibold border border-acc/40 text-acc2 hover:bg-acc/10 disabled:opacity-40">
                  {selected.status === 'building' ? <Loader size={12} className="animate-spin" /> : <RefreshCw size={12} />} Rebuild
                </button>
              )}>
                <KV k="source" v={selected.scm_url ? `${selected.scm_url.split('/').pop()} · ${selected.spec_path || '—'}` : selected.spec ? 'inline spec' : 'pre-built'} />
                {selected.scm_url && <KV k="branch" v={selected.scm_branch || 'main'} />}
                {selected.description && <KV k="description" v={selected.description} />}
                {selected.build_log && <button onClick={() => setShowLog(true)} className="mt-3 inline-flex items-center gap-2 font-mono text-[11px] text-dim hover:text-acc"><FileCode size={13} /> view build log</button>}
                {selected.scm_url && (
                  <>
                    <div className="mt-3 flex items-center gap-2.5 rounded-lg border border-line bg-[#070809] px-3 py-2">
                      <code className="flex-1 font-mono text-[10.5px] text-mut truncate">POST /api/v1/webhooks/execution-packs/{selected.id}/generic?token=…</code>
                      <button onClick={() => copy(`${window.location.origin}/api/v1/webhooks/execution-packs/${selected.id}/generic?token=YOUR_SECRET`)} className="text-dim hover:text-acc"><Copy size={14} /></button>
                    </div>
                    <p className="mt-2.5 text-[11.5px] text-dim leading-relaxed">A <b className="text-acc2 font-medium">push rebuilds the pack</b> — spec pulled from the repo, built, versioned, and republished automatically.</p>
                  </>
                )}
              </Rsec>

              {/* Runtime — the pack's actual bill of materials */}
              {selHasContents && (
                <Rsec title="Runtime" hint="bundled in the pack">
                  {selContents.python && <KV k="python" v={selContents.python} />}
                  {selContents.ansible && <KV k="ansible" v={selContents.ansible} />}
                  {selContents.hostRunner && <KV k="host-runner" v={selContents.hostRunner} />}
                  {selContents.pip.length > 0 && <KV k="pip" v={selContents.pip.map(p => p.version ? `${p.name} ${p.version}` : p.name).join(', ')} />}
                  {selContents.collections.length > 0 && <KV k="collections" v={selContents.collections.map(c => c.name).join(', ')} />}
                  {selContents.arches.length > 0 && <KV k="target arch" v={selContents.arches.join(', ')} />}
                  <p className="mt-2.5 text-[11.5px] text-dim leading-relaxed">Self-contained: a standalone CPython, the Ansible engine and the <b className="text-acc2 font-medium">host-runner daemon</b> — nothing is installed on the target. Project collections come from <code className="text-mut">requirements.yml</code> at run time.</p>
                </Rsec>
              )}

              {/* Distribution */}
              <Rsec title="Distribution">
                <KV k="artifact" v={<span className="inline-flex items-center gap-2">{selected.name}-linux-&lt;arch&gt;.tar.gz<button onClick={() => copy(`${selected.name}-linux-amd64.tar.gz`)} className="text-faint hover:text-acc"><Copy size={13} /></button></span>} />
                <KV k="registry" v="gitea · generic" />
                <p className="mt-2.5 text-[11.5px] text-dim leading-relaxed">Pushed to the target host over the run connection and unpacked to a temp dir — <b className="text-acc2 font-medium">removed after the run</b>.</p>
              </Rsec>

              {/* Used by */}
              <Rsec title="Used by" hint={`${usedBy.length} template${usedBy.length === 1 ? '' : 's'}`}>
                {usedBy.length === 0 ? <p className="font-mono text-[11px] text-faint">No templates select this pack.</p> : (
                  <div>
                    {usedBy.map(t => (
                      <div key={t.id} className="flex items-center gap-2.5 py-2 border-b border-line last:border-0 font-mono text-[12px] text-ink2">
                        <span className="w-[5px] h-[5px] rounded-full bg-faint" />{t.name}<span className="ml-auto text-[8.5px] uppercase tracking-[0.1em] text-dim border border-line rounded px-1.5 py-px">template</span>
                      </div>
                    ))}
                  </div>
                )}
              </Rsec>

              <div className="mt-7 pt-5 border-t border-line flex items-center gap-4">
                <button onClick={() => openEdit(selected)} className="inline-flex items-center gap-2 text-[12px] text-ink2 hover:text-acc"><Pencil size={14} /> Edit registration</button>
                <button onClick={() => remove(selected.id)} className="inline-flex items-center gap-2 text-[12px] text-err/90 hover:text-err"><Trash2 size={14} /> Delete</button>
              </div>
            </div>
          )}
          </div>
        </div>
      )}

      {/* Register / edit modal */}
      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title={editingId ? 'Edit execution pack' : 'Register execution pack'} size="lg">
        <div className="space-y-4">
          <Input label="Name" className="font-mono text-sm" placeholder="docker-tools" hint="Must match the built artifact: <name>-linux-<arch>.tar.gz" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />
          <Input label="Description" placeholder="ansible-core + community.docker + docker SDK" value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} />
          <Textarea label="Spec (YAML)" rows={6} className="font-mono text-xs" hint="Provide a spec and Praetor builds the pack (status → building → ready). Leave empty to register a pre-built artifact."
            placeholder={'name: docker-tools\nansible: ansible-core\ncollections:\n  - community.docker'} value={form.spec} onChange={e => setForm({ ...form, spec: e.target.value })} />
          <div className="rounded-lg border border-line bg-panel2 p-3 space-y-3">
            <div className="flex items-center gap-2 text-sm font-medium text-ink2"><GitBranch size={14} className="text-acc" /> Git source (optional)</div>
            <p className="text-xs text-dim -mt-1">Point the pack at a repo + path to its YAML. A push webhook <b className="text-ink2">rebuilds the pack</b>.</p>
            <div className="grid grid-cols-3 gap-2">
              <Input wrapperClassName="col-span-2" label="Repo URL" className="font-mono text-xs" placeholder="https://gitea.local/me/packs.git" value={form.scm_url} onChange={e => setForm({ ...form, scm_url: e.target.value })} />
              <Input label="Branch" className="font-mono text-xs" placeholder="main" value={form.scm_branch} onChange={e => setForm({ ...form, scm_branch: e.target.value })} />
            </div>
            <Input label="Spec path" className="font-mono text-xs" placeholder="path/in/repo/docker.yml" value={form.spec_path} onChange={e => setForm({ ...form, spec_path: e.target.value })} />
            <Input label="Webhook secret" className="font-mono text-xs" placeholder={editingId ? 'leave blank to keep current' : 'token for the push trigger'} value={form.webhook_key} onChange={e => setForm({ ...form, webhook_key: e.target.value })} />
          </div>
          <div className="flex justify-end gap-2"><Button variant="secondary" onClick={() => setShowModal(false)}>Cancel</Button><Button onClick={save}>{editingId ? 'Save changes' : 'Register'}</Button></div>
        </div>
      </Modal>

      {/* Build log */}
      <Modal isOpen={showLog} onClose={() => setShowLog(false)} title={`${selected?.name} · build log`} size="xl">
        <pre className="max-h-[60vh] overflow-auto scroll-tint rounded-lg border border-line bg-[#070809] p-4 font-mono text-[12px] leading-relaxed text-ink2 whitespace-pre-wrap">{selected?.build_log || 'No build log.'}</pre>
      </Modal>
    </div>
  );
};

const ContentRow: React.FC<{ icon: React.ReactNode; name: string; ver?: string }> = ({ icon, name, ver }) => (
  <div className="flex items-center gap-2.5 h-[27px] px-2 rounded hover:bg-white/[0.03]">
    <span className="text-mut">{icon}</span>
    <span className="font-mono text-[12px] text-ink2">{name}</span>
    {ver && <span className="font-mono text-[10.5px] text-dim">{ver}</span>}
  </div>
);

const Rsec: React.FC<{ title: string; hint?: string; action?: React.ReactNode; children: React.ReactNode }> = ({ title, hint, action, children }) => (
  <div className="mt-7 first:mt-0">
    <div className="flex items-center gap-2.5 mb-4">
      <span className="font-mono text-[10px] tracking-[0.16em] uppercase text-mut">{title}</span>
      {hint && <span className="font-mono text-[9.5px] text-faint">{hint}</span>}
      {action && <span className="ml-auto">{action}</span>}
    </div>
    {children}
  </div>
);

const KV: React.FC<{ k: string; v: React.ReactNode }> = ({ k, v }) => (
  <div className="flex justify-between gap-3.5 py-2 border-b border-line last:border-0 font-mono text-[12px]">
    <span className="text-dim">{k}</span>
    <span className="text-ink2 text-right">{v}</span>
  </div>
);

export default ExecutionPacksPage;
