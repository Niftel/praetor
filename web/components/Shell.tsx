import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { api, getCurrentUser } from '../services/api';
import {
  Search, LayoutDashboard, Play, FileText, Workflow, Server, CalendarClock,
  Package, GitBranch, Building2, Users, UsersRound, KeyRound, KeySquare,
  ScrollText, Settings as SettingsIcon, LogOut,
  ShieldCheck,
} from 'lucide-react';

// The command palette IS the control panel: no persistent sidebar. ⌘K opens one
// overlay with two tabs — History (recent runs) and All functions (the capability
// directory). This Shell is the frame every surface renders into via <Outlet/>.

type Fn = { label: string; path: string; icon: React.ComponentType<{ size?: number | string }>; keywords?: string };

const FUNCTIONS: { group: string; items: Fn[] }[] = [
  {
    group: 'Execute', items: [
      { label: 'Dashboard', path: '/', icon: LayoutDashboard, keywords: 'home overview' },
      { label: 'Jobs', path: '/jobs', icon: Play, keywords: 'runs history executions' },
      { label: 'Templates', path: '/templates', icon: FileText, keywords: 'job template launch' },
      { label: 'Workflows', path: '/workflows', icon: Workflow, keywords: 'dag pipeline' },
      { label: 'Approvals', path: '/approvals', icon: ShieldCheck, keywords: 'workflow gates approve deny pending' },
      { label: 'Inventories', path: '/inventories', icon: Server, keywords: 'hosts groups fleet' },
    ]
  },
  {
    group: 'Automate', items: [
      { label: 'Schedules & Triggers', path: '/schedules', icon: CalendarClock, keywords: 'cron webhook event' },
      { label: 'Execution Packs', path: '/execution-packs', icon: Package, keywords: 'runtime pack' },
      { label: 'Projects', path: '/projects', icon: GitBranch, keywords: 'scm git playbooks' },
    ]
  },
  {
    group: 'Govern', items: [
      { label: 'Organizations', path: '/organizations', icon: Building2, keywords: 'org rbac' },
      { label: 'Users', path: '/users', icon: Users, keywords: 'people accounts' },
      { label: 'Teams', path: '/teams', icon: UsersRound, keywords: 'groups' },
      { label: 'Credentials', path: '/credentials', icon: KeyRound, keywords: 'secrets ssh vault' },
      { label: 'API Tokens', path: '/tokens', icon: KeySquare, keywords: 'pat bearer' },
      { label: 'Activity', path: '/activity', icon: ScrollText, keywords: 'audit log' },
      { label: 'Settings', path: '/settings', icon: SettingsIcon, keywords: 'auth ldap config' },
    ]
  },
];
const ALL_FNS = FUNCTIONS.flatMap(g => g.items);

// Concrete example actions the omnibar rotates through — real capabilities, not
// fabricated data — so the closed bar advertises what the palette can do.
const OMNI_EXAMPLES = [
  'jump to Inventories',
  'launch a template',
  'open a recent run',
  'go to Workflows',
  'find a credential',
  'run a command…',
];

const STATUS_DOT: Record<string, string> = {
  successful: 'bg-ok', failed: 'bg-err', error: 'bg-err', running: 'bg-run',
  waiting: 'bg-run', pending: 'bg-changed', queued: 'bg-changed',
};

function relTime(iso?: string) {
  if (!iso) return '—';
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return `${Math.floor(s)}s ago`;
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return `${Math.floor(s / 86400)}d ago`;
}

const Shell: React.FC<{ onLogout: () => void }> = ({ onLogout }) => {
  const navigate = useNavigate();
  const location = useLocation();
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<'history' | 'fns'>('history');
  const [query, setQuery] = useState('');
  const [sel, setSel] = useState(0);
  const [history, setHistory] = useState<any[]>([]);
  const [approvalCount, setApprovalCount] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const user = getCurrentUser();
  const initial = (user?.username || 'A').charAt(0).toUpperCase();

  useEffect(() => {
    let active = true;
    const loadApprovals = () => api.getWorkflowApprovals()
      .then(rows => { if (active) setApprovalCount((rows || []).length); })
      .catch(() => {});
    const refreshWhenVisible = () => {
      if (document.visibilityState === 'visible') loadApprovals();
    };
    loadApprovals();
    const timer = setInterval(loadApprovals, 3000);
    window.addEventListener('focus', loadApprovals);
    document.addEventListener('visibilitychange', refreshWhenVisible);
    return () => {
      active = false;
      clearInterval(timer);
      window.removeEventListener('focus', loadApprovals);
      document.removeEventListener('visibilitychange', refreshWhenVisible);
    };
  }, [location.pathname]);

  // Rotating example actions in the closed omnibar — teaches what's possible and
  // invites the user in. Holds still under prefers-reduced-motion.
  const [ex, setEx] = useState(0);
  useEffect(() => {
    if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;
    const h = setInterval(() => setEx(i => (i + 1) % OMNI_EXAMPLES.length), 3200);
    return () => clearInterval(h);
  }, []);

  // Section context for the breadcrumb, derived from the route.
  const section = useMemo(() => {
    const seg = location.pathname.split('/').filter(Boolean)[0] || 'dashboard';
    const f = ALL_FNS.find(x => x.path === '/' + seg);
    return (f?.label || seg).toLowerCase();
  }, [location.pathname]);

  const close = useCallback(() => { setOpen(false); setQuery(''); setSel(0); }, []);
  const openPalette = useCallback(() => { setOpen(true); setTab('fns'); setQuery(''); setSel(0); }, []);

  // ⌘K / Ctrl-K toggles; Esc closes.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); open ? close() : openPalette(); }
      else if (e.key === 'Escape' && open) close();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, close, openPalette]);

  // Load recent runs whenever the palette opens.
  useEffect(() => {
    if (!open) return;
    inputRef.current?.focus();
    api.getJobs().then(d => setHistory((d || []).slice(0, 12))).catch(() => setHistory([]));
  }, [open]);

  // The active result set for the current tab + query.
  const results = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (tab === 'history') {
      return history
        .filter(j => !q || (j.name || '').toLowerCase().includes(q) || String(j.id).includes(q))
        .map(j => ({ key: `h${j.id}`, kind: 'history' as const, job: j }));
    }
    return ALL_FNS
      .filter(f => !q || f.label.toLowerCase().includes(q) || (f.keywords || '').includes(q))
      .map(f => ({ key: f.path, kind: 'fn' as const, fn: f }));
  }, [tab, query, history]);

  useEffect(() => { setSel(0); }, [tab, query]);

  const choose = (i: number) => {
    const r = results[i];
    if (!r) return;
    if (r.kind === 'history') navigate(`/jobs/${r.job.id}`);
    else navigate(r.fn.path);
    close();
  };

  // Palette navigation is handled at the window level (not just on the input) so
  // ↑/↓/⏎/⇥ work regardless of which element currently has focus — otherwise Tab
  // falls back to the browser's focus traversal the moment focus leaves the box.
  useEffect(() => {
    if (!open) return;
    const nav = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') { e.preventDefault(); setSel(s => Math.min(results.length - 1, s + 1)); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); setSel(s => Math.max(0, s - 1)); }
      else if (e.key === 'Enter') { e.preventDefault(); choose(sel); }
      else if (e.key === 'Tab') { e.preventDefault(); setTab(t => (t === 'history' ? 'fns' : 'history')); }
    };
    window.addEventListener('keydown', nav);
    return () => window.removeEventListener('keydown', nav);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, results, sel]);

  return (
    <div className="h-screen flex flex-col overflow-hidden bg-bg text-ink">
      {/* top bar */}
      <div className="h-[52px] flex-none border-b border-line flex items-center gap-3.5 px-[18px]">
        <div className="flex items-center gap-2.5">
          <div className="w-6 h-6 rounded-md border border-acc grid place-items-center text-acc font-mono font-bold text-[13px]">P</div>
          <span className="font-semibold tracking-tight text-sm">Praetor</span>
        </div>
        <div className="font-mono text-[11px] text-mut"><span className="text-ink">{section}</span></div>

        <button
          onClick={openPalette}
          title="Search runs, jump to any page, or run an action"
          className="group mx-auto w-[min(520px,42vw)] h-[34px] flex items-center gap-2.5 px-3 border border-line2 rounded-[9px] bg-panel text-left transition-colors hover:border-acc/45 hover:bg-panel2"
        >
          <span className="text-acc font-mono font-semibold transition-transform group-hover:translate-x-0.5">&#10095;</span>
          <span className="text-dim text-[12.5px] flex-1 min-w-0 truncate">
            Search or <span key={ex} className="text-mut omni-ex">{OMNI_EXAMPLES[ex]}</span>
          </span>
          <span className="font-mono text-[10px] text-mut group-hover:text-ink border border-line group-hover:border-line2 rounded-[5px] px-1.5 py-px transition-colors">⌘K</span>
        </button>

        <div className="ml-auto flex items-center gap-3.5">
          <button onClick={() => navigate('/approvals')} title={`${approvalCount} pending approval${approvalCount === 1 ? '' : 's'}`} className="relative grid h-7 w-7 place-items-center rounded-lg text-dim hover:bg-panel hover:text-ink">
            <ShieldCheck size={15} />
            {approvalCount > 0 && <span className="absolute -right-1.5 -top-1.5 min-w-[17px] rounded-full bg-changed px-1 font-mono text-[9px] font-semibold leading-[17px] text-[#1a1203] tabular-nums">{approvalCount > 99 ? '99+' : approvalCount}</span>}
          </button>
          <button onClick={onLogout} title="Sign out" className="w-7 h-7 grid place-items-center rounded-lg text-dim hover:text-ink hover:bg-panel">
            <LogOut size={15} />
          </button>
          <div className="w-[27px] h-[27px] rounded-[7px] bg-[#161b24] grid place-items-center text-acc font-mono text-[11px] border border-line2">{initial}</div>
        </div>
      </div>

      {/* content */}
      <div className="flex-1 min-h-0 overflow-auto scroll-tint" style={{ overscrollBehavior: 'contain' }}>
        <Outlet />
      </div>

      {/* command palette */}
      {open && (
        <div className="fixed inset-0 z-50 flex items-start justify-center pt-[12vh] px-4" onMouseDown={close}>
          <div className="absolute inset-0 bg-black/55 backdrop-blur-[2px]" />
          <div
            className="relative w-full max-w-[620px] rounded-[14px] border border-line2 bg-[#0d0f15] shadow-[0_24px_60px_rgba(0,0,0,.6)] overflow-hidden"
            onMouseDown={e => e.stopPropagation()}
            style={{ overscrollBehavior: 'contain' }}
          >
            {/* search */}
            <div className="flex items-center gap-3 px-4 h-[52px] border-b border-line">
              <Search size={16} className="text-dim" />
              <input
                ref={inputRef}
                value={query}
                onChange={e => setQuery(e.target.value)}
                placeholder="Search runs, jump to a page, or run an action…"
                className="flex-1 bg-transparent outline-none text-ink text-sm placeholder:text-dim"
              />
            </div>

            {/* tabs */}
            <div className="flex items-center gap-1 px-3 h-[38px] border-b border-line">
              {(['fns', 'history'] as const).map(t => (
                <button key={t} onClick={() => setTab(t)}
                  className={`h-[28px] px-3 rounded-md text-[12px] font-medium ${tab === t ? 'bg-panel text-ink' : 'text-mut hover:text-ink'}`}>
                  {t === 'history' ? 'History' : 'All functions'}
                </button>
              ))}
              <span className="ml-auto font-mono text-[10px] text-dim">
                {tab === 'fns' ? 'Every page & action' : 'Your recent runs — jump straight in'}
              </span>
            </div>

            {/* results */}
            <div className="max-h-[52vh] overflow-auto p-1.5" style={{ overscrollBehavior: 'contain' }}>
              {tab === 'fns' && !query
                ? FUNCTIONS.map(g => (
                  <div key={g.group}>
                    <div className="font-mono text-[9px] tracking-[0.14em] uppercase text-dim px-3 pt-2.5 pb-1.5">{g.group}</div>
                    {g.items.map(f => {
                      const i = results.findIndex(r => r.key === f.path);
                      return <FnRow key={f.path} fn={f} active={i === sel} onClick={() => choose(i)} onHover={() => setSel(i)} />;
                    })}
                  </div>
                ))
                : results.map((r, i) => r.kind === 'history'
                  ? <HistoryRow key={r.key} job={r.job} active={i === sel} onClick={() => choose(i)} onHover={() => setSel(i)} />
                  : <FnRow key={r.key} fn={r.fn} active={i === sel} onClick={() => choose(i)} onHover={() => setSel(i)} />
                )}
              {results.length === 0 && (
                <div className="px-3 py-8 text-center text-dim text-sm">
                  {tab === 'history'
                    ? (query ? 'No matching runs.' : 'No recent runs yet — press ⇥ for All functions.')
                    : 'No matching action.'}
                </div>
              )}
            </div>

            {/* keyboard hints */}
            <div className="flex items-center gap-3.5 px-3.5 h-[34px] border-t border-line bg-panel2">
              <Hint keys={['↑', '↓']} label="Navigate" />
              <Hint keys={['↵']} label={tab === 'history' ? 'Open run' : 'Go'} />
              <Hint keys={['⇥']} label="Switch tab" />
              <Hint keys={['esc']} label="Close" />
              <span className="ml-auto font-mono text-[10px] text-faint">click to select · type to filter</span>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

const Hint: React.FC<{ keys: string[]; label: string }> = ({ keys, label }) => (
  <span className="flex items-center gap-1.5 font-mono text-[10px] text-mut">
    <span className="flex gap-1">
      {keys.map(k => (
        <kbd key={k} className="inline-flex items-center justify-center min-w-[17px] h-[17px] px-1 rounded border border-line2 text-dim text-[10px] leading-none">{k}</kbd>
      ))}
    </span>
    {label}
  </span>
);

const FnRow: React.FC<{ fn: Fn; active: boolean; onClick: () => void; onHover: () => void }> = ({ fn, active, onClick, onHover }) => (
  <button onClick={onClick} onMouseMove={onHover}
    className={`w-full flex items-center gap-3 h-[38px] px-3 rounded-lg text-left ${active ? 'bg-white/[.05]' : ''}`}>
    <fn.icon size={15} />
    <span className="text-[13px] text-ink2">{fn.label}</span>
  </button>
);

const HistoryRow: React.FC<{ job: any; active: boolean; onClick: () => void; onHover: () => void }> = ({ job, active, onClick, onHover }) => (
  <button onClick={onClick} onMouseMove={onHover}
    className={`w-full flex items-center gap-3 h-[42px] px-3 rounded-lg text-left ${active ? 'bg-white/[.05]' : ''}`}>
    <span className={`w-[7px] h-[7px] rounded-full flex-none ${STATUS_DOT[job.status] || 'bg-faint'}`} />
    <span className="font-mono text-[12.5px] text-ink truncate flex-1">{job.name || `job #${job.id}`}</span>
    <span className="font-mono text-[11px] text-dim">{job.status}</span>
    <span className="font-mono text-[11px] text-faint">{relTime(job.started_at || job.created_at)}</span>
  </button>
);

export default Shell;
