import React, { useState } from 'react';
import { api, setAuthToken } from '../services/api';
import { toast } from '../components/ui/toast';
import { GitFork, ChevronsRight, ShieldCheck, LogIn, Lock } from 'lucide-react';

interface LoginPageProps { onLogin: () => void; }

const DIFFS = [
  { icon: GitFork, node: <>Orchestrate playbooks into <b className="text-ink font-semibold">multi-step workflows</b> — with gates, branches, and triggers.</> },
  { icon: ChevronsRight, node: <>Push a <b className="text-ink font-semibold">self-contained runtime</b> to any host at run time — nothing pre-installed.</> },
  { icon: ShieldCheck, node: <>Recovery that <b className="text-ink font-semibold">survives any outage</b> — no run is ever lost to a control-plane restart.</> },
];

const LoginPage: React.FC<LoginPageProps> = ({ onLogin }) => {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsLoading(true); setError('');
    try {
      const data = await api.login({ username, password });
      setAuthToken(data.token);
      onLogin();
    } catch (err) {
      console.error(err);
      setError('Those credentials did not match. Check your username and password.');
      toast.error('Sign in failed. Check your credentials.');
    } finally { setIsLoading(false); }
  };

  return (
    <div className="h-[100dvh] grid lg:grid-cols-[1.12fr_0.88fr] bg-bg text-ink">
      {/* Identity panel */}
      <div className="relative hidden lg:flex flex-col justify-between p-14 border-r border-line overflow-hidden bg-tree"
        style={{ backgroundImage: 'radial-gradient(rgba(255,255,255,.045) 1px, transparent 1px)', backgroundSize: '23px 23px' }}>
        <div className="absolute -top-40 -left-28 w-[520px] h-[520px] rounded-full blur-2xl pointer-events-none"
          style={{ background: 'radial-gradient(circle, rgba(77,224,200,.16), transparent 68%)' }} aria-hidden />
        <div className="absolute -bottom-44 -right-32 w-[460px] h-[460px] rounded-full blur-2xl pointer-events-none"
          style={{ background: 'radial-gradient(circle, rgba(90,162,255,.09), transparent 70%)' }} aria-hidden />

        <div className="relative flex items-center gap-3">
          <div className="w-[38px] h-[38px] border border-acc rounded-[10px] grid place-items-center text-acc font-mono font-bold text-[19px]" style={{ boxShadow: '0 0 24px rgba(77,224,200,.14)' }}>P</div>
          <div><div className="text-[16px] font-semibold tracking-tight">Praetor</div><div className="font-mono text-[9.5px] tracking-[0.22em] uppercase text-dim mt-0.5">Control Plane</div></div>
        </div>

        <div className="relative">
          <h1 className="text-[35px] leading-[1.14] font-semibold tracking-[-0.025em] max-w-[12ch] text-balance">The automation controller that <span className="text-acc2">keeps running.</span></h1>
          <div className="mt-8 flex flex-col gap-4">
            {DIFFS.map(({ icon: Icon, node }, i) => (
              <div key={i} className="flex items-start gap-3.5 max-w-[400px]">
                <span className="w-8 h-8 rounded-[9px] border border-line bg-white/[0.02] grid place-items-center text-acc2 shrink-0"><Icon size={16} /></span>
                <span className="text-[13.5px] text-ink2 leading-snug pt-1.5">{node}</span>
              </div>
            ))}
          </div>
        </div>

        <div className="relative flex items-center gap-2.5 font-mono text-[11px] text-dim">
          <span className="w-[7px] h-[7px] rounded-full bg-ok animate-pulse" style={{ boxShadow: '0 0 0 3px rgba(58,208,127,.14)' }} />
          control plane · operational<span className="text-faint">·</span>Praetor Automation Platform
        </div>
      </div>

      {/* Form panel */}
      <div className="flex items-center justify-center p-10 bg-bg">
        <div className="w-full max-w-[352px]">
          <div className="lg:hidden flex items-center gap-3 mb-8">
            <div className="w-9 h-9 border border-acc rounded-[10px] grid place-items-center text-acc font-mono font-bold text-[17px]">P</div>
            <span className="text-lg font-semibold tracking-tight">Praetor</span>
          </div>

          <h2 className="text-[24px] font-semibold tracking-[-0.02em]">Sign in</h2>
          <p className="mt-2 text-[13px] text-mut">Access your automation controller.</p>

          <form onSubmit={handleSubmit} className="mt-8 flex flex-col gap-4">
            <div>
              <label htmlFor="login-user" className="block text-[12px] text-mut mb-2 font-medium">Username</label>
              <input id="login-user" type="text" required autoComplete="username" placeholder="you@acme.corp"
                value={username} onChange={e => { setUsername(e.target.value); setError(''); }}
                className="w-full h-11 rounded-[10px] border border-line2 bg-panel px-3.5 text-[14px] text-ink placeholder:text-dim outline-none focus:border-acc/60 transition-colors" />
            </div>
            <div>
              <label htmlFor="login-pass" className="block text-[12px] text-mut mb-2 font-medium">Password</label>
              <input id="login-pass" type="password" required autoComplete="current-password" placeholder="••••••••••••"
                value={password} onChange={e => { setPassword(e.target.value); setError(''); }}
                className="w-full h-11 rounded-[10px] border border-line2 bg-panel px-3.5 text-[14px] text-ink placeholder:text-dim outline-none focus:border-acc/60 transition-colors" />
            </div>

            {error && <p role="alert" className="text-[13px] text-err bg-err/10 border border-err/30 rounded-lg px-3 py-2">{error}</p>}

            <button type="submit" disabled={isLoading}
              className="w-full h-[46px] rounded-[10px] bg-acc text-[#06231e] font-bold text-[14px] flex items-center justify-center gap-2.5 hover:bg-acc2 disabled:opacity-60 transition-colors mt-1.5">
              <LogIn size={15} strokeWidth={2.4} /> {isLoading ? 'Signing in…' : 'Sign in'}
            </button>
          </form>

          <div className="mt-5 flex items-center justify-center gap-2 font-mono text-[11px] text-dim"><Lock size={12} /> Local or LDAP directory account</div>
        </div>
      </div>
    </div>
  );
};

export default LoginPage;
