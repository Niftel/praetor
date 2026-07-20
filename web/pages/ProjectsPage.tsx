import React, { useState, useEffect, useMemo } from 'react';
import { useParams, Link } from 'react-router-dom';
import { api, unwrap } from '../services/api';
import { Project } from '../types';
import Button from '../components/ui/Button';
import Modal from '../components/ui/Modal';
import { Input } from '../components/ui/Input';
import { RefreshCw, Plus, ArrowLeft, GitBranch, ChevronDown, GitFork, Trash2 } from 'lucide-react';
import { toast, confirmDialog } from '../components/ui/toast';
import { EmptyState, FormActions, FormErrorSummary, FormSection, LoadingState, Page, PageHeader, useDirtyFormGuard } from '../components/ui';

const ago = (iso?: string): string => {
  if (!iso) return '—';
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (!Number.isFinite(s)) return '—';
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60); if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60); if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24); return d < 30 ? `${d}d ago` : new Date(iso).toLocaleDateString();
};

const ProjectsPage = () => {
  const { orgId: orgIdStr } = useParams();
  const orgId = Number(orgIdStr);
  const [projects, setProjects] = useState<Project[]>([]);
  const [templates, setTemplates] = useState<any[]>([]);
  const [orgName, setOrgName] = useState('');
  const [loading, setLoading] = useState(true);
  const [syncing, setSyncing] = useState<number | null>(null);
  const [openId, setOpenId] = useState<number | null>(null);

  const [modalOpen, setModalOpen] = useState(false);
  const [newName, setNewName] = useState('');
  const [newUrl, setNewUrl] = useState('');
  const [newBranch, setNewBranch] = useState('main');
  const [creating, setCreating] = useState(false);
  const [formErrors, setFormErrors] = useState<string[]>([]);

  const fetchProjects = async () => {
    try {
      const [d, tpls] = await Promise.all([api.getProjects(), api.getTemplates().catch(() => [])]);
      setProjects(unwrap<Project>(d).filter(p => p.organization_id === orgId));
      setTemplates(unwrap(tpls));
    } catch (err) { console.error('Failed to load projects', err); }
    finally { setLoading(false); }
  };

  useEffect(() => {
    fetchProjects();
    api.getOrganizations().then(d => setOrgName(unwrap<{ id: number; name: string }>(d).find(o => o.id === orgId)?.name ?? `Org ${orgId}`)).catch(() => setOrgName(`Org ${orgId}`));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orgId]);

  const sync = async (e: React.MouseEvent, id: number) => {
    e.stopPropagation();
    setSyncing(id);
    try { await api.syncProject(id); toast.info('Sync started'); setTimeout(fetchProjects, 3000); }
    catch { toast.error('Sync failed'); }
    finally { setTimeout(() => setSyncing(null), 1200); }
  };

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (creating) return;
    const errors: string[] = [];
    if (!newName.trim()) errors.push('Name is required.');
    if (!newUrl.trim()) errors.push('SCM URL is required.');
    setFormErrors(errors);
    if (errors.length) return;
    setCreating(true);
    try {
      await api.createProject({ name: newName.trim(), scm_url: newUrl.trim(), scm_type: 'git', scm_branch: newBranch.trim() || 'main', organization_id: orgId });
      setNewName(''); setNewUrl(''); setNewBranch('main'); setFormErrors([]); setModalOpen(false); fetchProjects();
    } catch { setFormErrors(['Praetor could not create this project. No changes were saved.']); }
    finally { setCreating(false); }
  };

  const dirty = modalOpen && Boolean(newName || newUrl || (newBranch && newBranch !== 'main'));
  const canDiscard = useDirtyFormGuard(dirty);
  const closeForm = async () => { if (creating || !(await canDiscard())) return; setModalOpen(false); setFormErrors([]); setNewName(''); setNewUrl(''); setNewBranch('main'); };

  const remove = async (e: React.MouseEvent, id: number) => {
    e.stopPropagation();
    if (!(await confirmDialog('Delete this project?', { destructive: true, confirmText: 'Delete' }))) return;
    try { await (api as any).deleteProject?.(id); fetchProjects(); } catch { toast.error('Failed to delete project'); }
  };

  const usedBy = useMemo(() => (id: number) => templates.filter(t => t.project_id === id), [templates]);
  const shortUrl = (u: string) => u.replace(/^https?:\/\//, '').replace(/\.git$/, '');

  if (loading) return <Page><LoadingState label="Loading projects" /></Page>;

  return (
    <Page>
      <PageHeader
        title={`${orgName} · Projects`}
        description="Git repositories Praetor syncs — templates draw their playbooks from a project at a branch."
        meta={<Link to="/projects" className="inline-flex items-center gap-1.5 rounded-sm text-mut hover:text-acc"><ArrowLeft size={12} /> Organizations</Link>}
        actions={<Button icon={<Plus size={15} />} onClick={() => { setFormErrors([]); setModalOpen(true); }}>Add project</Button>}
      />

      {projects.length > 0 && (
        <div className="rounded-2xl border border-line overflow-hidden">
          {projects.map(p => {
            const open = openId === p.id;
            const templatesUsing = usedBy(p.id);
            return (
              <div key={p.id} className="border-b border-line last:border-0">
                <div onClick={() => setOpenId(open ? null : p.id)}
                  className={`grid grid-cols-[minmax(0,1.4fr)_minmax(0,1.3fr)_240px] items-center gap-5 px-5 py-4 cursor-pointer ${open ? 'bg-white/[0.022]' : 'hover:bg-white/[0.02]'} max-[820px]:grid-cols-[1fr_auto]`}>
                  <div className="flex items-center gap-3 min-w-0">
                    <span className="w-[34px] h-[34px] rounded-lg border border-line2 grid place-items-center text-mut shrink-0"><GitFork size={17} /></span>
                    <div className="min-w-0">
                      <div className="text-[14px] font-semibold tracking-tight truncate">{p.name}</div>
                      <div className="font-mono text-[10.5px] text-dim truncate">{shortUrl(p.scm_url)}</div>
                    </div>
                  </div>
                  <div className="min-w-0 max-[820px]:hidden">
                    <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-ink2 border border-line rounded-md px-2 py-0.5"><GitBranch size={11} className="text-mut" />{p.scm_branch || 'main'}</span>
                    <div className="font-mono text-[11px] text-dim mt-1.5 truncate">{p.scm_type} · {p.description || 'no description'}</div>
                  </div>
                  <div className="flex items-center gap-4 justify-end">
                    <span className="font-mono text-[11px] text-dim whitespace-nowrap">updated {ago(p.modified_at)}</span>
                    <button onClick={e => sync(e, p.id)} disabled={syncing === p.id} className="w-8 h-8 rounded-lg border border-line2 grid place-items-center text-mut hover:text-acc2 hover:border-acc/40 disabled:opacity-50" title="Sync now">
                      <RefreshCw size={15} className={syncing === p.id ? 'animate-spin' : ''} />
                    </button>
                    <ChevronDown size={14} className={`text-dim transition-transform ${open ? 'rotate-180' : ''}`} />
                  </div>
                </div>
                {open && (
                  <div className="bg-panel2 px-5 pl-[67px] py-6 grid grid-cols-[1.3fr_1fr] gap-10 border-t border-line max-[820px]:grid-cols-1 max-[820px]:pl-5">
                    <div>
                      <div className="font-mono text-[9px] tracking-[0.15em] uppercase text-dim mb-3.5">Source</div>
                      <KV k="repository" v={<a href={p.scm_url} target="_blank" rel="noreferrer" className="text-acc2 hover:underline break-all">{shortUrl(p.scm_url)}</a>} />
                      <KV k="branch" v={p.scm_branch || 'main'} />
                      <KV k="type" v={p.scm_type} />
                      <KV k="added" v={new Date(p.created_at).toLocaleDateString()} />
                      <KV k="last updated" v={ago(p.modified_at)} />
                    </div>
                    <div>
                      <div className="font-mono text-[9px] tracking-[0.15em] uppercase text-dim mb-3.5 flex items-baseline gap-2">Used by <span className="text-faint tracking-normal">{templatesUsing.length} template{templatesUsing.length === 1 ? '' : 's'}</span></div>
                      {templatesUsing.length === 0 ? <p className="font-mono text-[11.5px] text-dim">No templates draw from this project.</p> : (
                        <div className="flex flex-wrap gap-2">{templatesUsing.map(t => <span key={t.id} className="font-mono text-[11.5px] text-ink2 border border-line rounded-lg px-2.5 py-1">{t.name}{t.playbook ? <span className="text-dim"> · {t.playbook}</span> : ''}</span>)}</div>
                      )}
                      <button onClick={e => remove(e, p.id)} className="mt-6 inline-flex items-center gap-2 text-[12px] text-err/90 hover:text-err"><Trash2 size={14} /> Delete project</button>
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
      {projects.length === 0 && <EmptyState title="No projects yet" description="Add a Git repository so templates can select playbooks from it." action={<Button icon={<Plus size={15} />} onClick={() => setModalOpen(true)}>Add project</Button>} />}

      <Modal isOpen={modalOpen} onClose={() => { void closeForm(); }} title={`New project in ${orgName}`}>
        <form onSubmit={add} className="space-y-4">
          <FormErrorSummary errors={formErrors} />
          <FormSection title="Source control" description="Praetor clones this repository when synchronizing project content.">
            <Input label="Name" placeholder="core-infra" value={newName} error={formErrors.includes('Name is required.') ? 'Enter a project name.' : undefined} onChange={e => setNewName(e.target.value)} required />
            <Input label="SCM URL" className="font-mono text-sm" placeholder="https://github.com/acme/core-infra.git" value={newUrl} error={formErrors.includes('SCM URL is required.') ? 'Enter the Git repository URL.' : undefined} onChange={e => setNewUrl(e.target.value)} required />
            <Input label="Branch" className="font-mono text-sm" placeholder="main" value={newBranch} onChange={e => setNewBranch(e.target.value)} />
          </FormSection>
          <FormActions onCancel={() => { void closeForm(); }} submitting={creating} submitLabel="Add project" />
        </form>
      </Modal>
    </Page>
  );
};

const KV: React.FC<{ k: string; v: React.ReactNode }> = ({ k, v }) => (
  <div className="flex items-center gap-2.5 py-2 border-b border-line last:border-0 font-mono text-[12px]">
    <span className="text-dim min-w-[92px]">{k}</span><span className="text-ink2 min-w-0 truncate">{v}</span>
  </div>
);

export default ProjectsPage;
