import React, { useEffect, useState } from 'react';
import { api } from '../services/api';
import { Users, Lock, FileText, Sliders, Bell, Server, Shield } from 'lucide-react';
import { PageSpinner } from '../components/ui/PageSpinner';

interface LdapConfig {
  configured: boolean;
  config_path: string;
  config_error?: string;
  server?: { url: string; bind_dn: string; start_tls: boolean; timeout: string };
  users?: { search_base: string; search_filter: string; search_scope: string };
  group_type?: { type: string; search_base: string };
  user_flags_by_group?: { is_superuser: string[] | null; is_system_auditor: string[] | null };
  organization_map?: Record<string, Record<string, string | string[] | boolean>>;
  team_map?: Record<string, Record<string, string | string[] | boolean>>;
}

const NAV = [
  { id: 'auth', label: 'Authentication', icon: Lock, active: true },
  { id: 'exec', label: 'Execution defaults', icon: Sliders, soon: true },
  { id: 'notif', label: 'Notifications', icon: Bell, soon: true },
  { id: 'system', label: 'System', icon: Server, soon: true },
  { id: 'license', label: 'License', icon: Shield, soon: true },
];

const SettingsPage: React.FC = () => {
  const [cfg, setCfg] = useState<LdapConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [provider, setProvider] = useState<'ldap' | 'saml'>('ldap');

  useEffect(() => { api.getLdapConfig().then(setCfg).catch(() => setCfg(null)).finally(() => setLoading(false)); }, []);

  const orgMap = cfg?.organization_map || {};
  const teamMap = cfg?.team_map || {};

  return (
    <div className="flex h-full min-h-0 bg-bg text-ink">
      {/* Settings nav */}
      <div className="w-[236px] shrink-0 border-r border-line bg-tree p-3 overflow-auto scroll-tint max-[820px]:hidden">
        <div className="font-mono text-[9px] tracking-[0.16em] uppercase text-dim px-2.5 pt-2 pb-1.5">Settings</div>
        {NAV.map(n => {
          const Icon = n.icon;
          return (
            <div key={n.id} className={`flex items-center gap-3 h-9 px-2.5 rounded-lg ${n.active ? 'bg-acc/[0.09] text-ink' : n.soon ? 'text-faint cursor-default' : 'text-mut hover:bg-white/[0.028] cursor-pointer'}`}>
              <Icon size={15} className={n.active ? 'text-acc2' : ''} />
              <span className="text-[12.5px]">{n.label}</span>
              {n.soon && <span className="ml-auto font-mono text-[8.5px] uppercase tracking-[0.08em] text-faint border border-line rounded px-1.5 py-px">soon</span>}
            </div>
          );
        })}
      </div>

      {/* Content */}
      <div className="flex-1 min-h-0 overflow-auto scroll-tint">
        <div className="px-9 pt-6 pb-5 border-b border-line">
          <h1 className="text-[20px] font-semibold tracking-tight">Authentication</h1>
          <p className="mt-1.5 text-[12.5px] text-mut">How users sign in, and how directory groups map to Praetor roles.</p>
        </div>

        {loading ? <PageSpinner /> : (
          <div className="px-9 py-6 max-w-[800px]">
            {/* Providers */}
            <div className="grid grid-cols-[repeat(2,214px)] gap-2.5 mb-6 max-[560px]:grid-cols-1">
              <ProviderCard icon={<Users size={16} />} name="LDAP" on={!!cfg?.configured} sel={provider === 'ldap'} onClick={() => setProvider('ldap')} status={cfg?.configured ? 'connected' : 'not configured'} />
              <ProviderCard icon={<Lock size={16} />} name="SAML" on={false} sel={provider === 'saml'} onClick={() => setProvider('saml')} status="not configured" />
            </div>

            {provider === 'saml' ? (
              <div className="rounded-xl border border-line bg-panel2 p-6 text-center">
                <Lock size={36} className="mx-auto mb-3 text-dim opacity-50" />
                <h3 className="text-[15px] font-medium mb-1">SAML / SSO</h3>
                <p className="text-[12.5px] text-mut max-w-md mx-auto">Available in the backend; configure via <span className="font-mono text-ink2">SAML_IDP_METADATA_URL</span>, <span className="font-mono text-ink2">SAML_SP_ENTITY_ID</span>, <span className="font-mono text-ink2">SAML_SP_ACS_URL</span>. UI configuration is coming.</p>
              </div>
            ) : !cfg?.configured ? (
              <div className="rounded-xl border border-line bg-panel2 p-6 text-center">
                <FileText size={36} className="mx-auto mb-3 text-changed opacity-70" />
                <h3 className="text-[15px] font-medium mb-1">LDAP not configured</h3>
                <p className="text-[12.5px] text-mut">Create a config at <span className="font-mono text-ink2">{cfg?.config_path}</span> — see <span className="font-mono text-ink2">deployments/ldap/ldap-config.yaml</span>.</p>
              </div>
            ) : cfg.config_error ? (
              <div className="rounded-xl border border-err/30 bg-err/10 p-4">
                <h4 className="font-medium text-err mb-2">Configuration error</h4>
                <pre className="text-[12px] text-err/90 whitespace-pre-wrap font-mono">{cfg.config_error}</pre>
              </div>
            ) : (
              <>
                {/* Read-only banner */}
                <div className="flex items-center gap-3.5 px-4 py-3 rounded-xl border border-line bg-panel2 mb-6">
                  <FileText size={15} className="text-mut shrink-0" />
                  <span className="flex-1 font-mono text-[11px] text-mut">Configured from <b className="text-ink2 font-medium">{cfg.config_path}</b> · applied at login · read-only here</span>
                  <span className="font-mono text-[10.5px] text-ok flex items-center gap-1.5"><span className="w-1.5 h-1.5 rounded-full bg-ok" /> connected</span>
                </div>

                <Sec title="Connection">
                  <KV k="url" v={cfg.server?.url} />
                  <KV k="bind dn" v={cfg.server?.bind_dn} />
                  <KV k="start tls" v={cfg.server?.start_tls ? 'yes' : 'no'} />
                  {cfg.server?.timeout && <KV k="timeout" v={cfg.server.timeout} />}
                </Sec>

                <Sec title="Directory">
                  <KV k="user search base" v={cfg.users?.search_base} />
                  <KV k="user filter" v={cfg.users?.search_filter} />
                  <KV k="group type" v={cfg.group_type?.type || '—'} />
                  <KV k="group base" v={cfg.group_type?.search_base || '—'} />
                </Sec>

                <Sec title="Platform flags by group" hint="user_flags_by_group">
                  <FlagRow k="is_superuser" dns={cfg.user_flags_by_group?.is_superuser} />
                  <FlagRow k="is_system_auditor" dns={cfg.user_flags_by_group?.is_system_auditor} />
                </Sec>

                <Sec title="Organization map" hint="AUTH_LDAP_ORGANIZATION_MAP · as loaded">
                  {Object.keys(orgMap).length === 0 ? <p className="font-mono text-[11.5px] text-faint">No organizations mapped.</p> : <CodeBlock data={orgMap} />}
                </Sec>

                <Sec title="Team map" hint="AUTH_LDAP_TEAM_MAP · as loaded">
                  {Object.keys(teamMap).length === 0 ? <p className="font-mono text-[11.5px] text-faint">No teams mapped.</p> : <CodeBlock data={teamMap} />}
                </Sec>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

const ProviderCard: React.FC<{ icon: React.ReactNode; name: string; on: boolean; sel: boolean; status: string; onClick: () => void }> = ({ icon, name, on, sel, status, onClick }) => (
  <button onClick={onClick} className={`text-left rounded-xl p-3.5 border ${sel ? 'border-transparent shadow-[inset_0_0_0_1.5px_rgba(77,224,200,0.5)] bg-acc/[0.05]' : 'border-line bg-panel hover:border-line2'}`}>
    <div className="flex items-center gap-2.5"><span className={sel ? 'text-acc2' : 'text-mut'}>{icon}</span><span className={`font-mono text-[12.5px] font-medium ${sel ? 'text-ink' : 'text-ink2'}`}>{name}</span></div>
    <div className={`mt-2.5 font-mono text-[10px] flex items-center gap-1.5 ${on ? 'text-ok' : 'text-dim'}`}><span className={`w-1.5 h-1.5 rounded-full ${on ? 'bg-ok' : 'bg-faint'}`} />{status}</div>
  </button>
);

const Sec: React.FC<{ title: string; hint?: string; children: React.ReactNode }> = ({ title, hint, children }) => (
  <div className="py-6 border-t border-line first:border-t-0 first:pt-0">
    <div className="flex items-baseline gap-2.5 mb-3.5"><span className="font-mono text-[10px] tracking-[0.16em] uppercase text-mut">{title}</span>{hint && <span className="font-mono text-[9.5px] text-faint">{hint}</span>}</div>
    {children}
  </div>
);

const KV: React.FC<{ k: string; v?: React.ReactNode }> = ({ k, v }) => (
  <div className="flex justify-between gap-4 py-2 border-b border-line last:border-0 font-mono text-[12px]"><span className="text-dim">{k}</span><span className="text-ink2 text-right break-all">{v}</span></div>
);

const FlagRow: React.FC<{ k: string; dns?: string[] | null }> = ({ k, dns }) => (
  <div className="flex justify-between gap-4 py-2 border-b border-line last:border-0 font-mono text-[11.5px]">
    <span className="text-violet">{k}</span>
    <span className="text-ink2 text-right break-all">{dns && dns.length ? dns.join(', ') : '—'}</span>
  </div>
);

const CodeBlock: React.FC<{ data: any }> = ({ data }) => (
  <pre className="rounded-lg border border-line bg-[#070809] p-4 font-mono text-[11.5px] leading-relaxed text-mut overflow-auto scroll-tint whitespace-pre">{JSON.stringify(data, null, 2)}</pre>
);

export default SettingsPage;
