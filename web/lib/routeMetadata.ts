import type React from 'react';
import {
  Bot, Building2, CalendarClock, FileText, GitBranch, KeyRound, KeySquare,
  LayoutDashboard, Package, Play, ScrollText, Server, Settings, ShieldCheck,
  Users, UsersRound, Workflow,
} from 'lucide-react';

export type RouteGroup = 'Execute' | 'Automate' | 'Govern';
export type RouteIcon = React.ComponentType<{ size?: number | string }>;

export interface RouteMetadata {
  label: string;
  path: string;
  group: RouteGroup;
  icon: RouteIcon;
  keywords: string;
}

export interface Breadcrumb { label: string; path?: string }

/** Canonical catalog for authenticated, directly navigable Praetor surfaces. */
export const ROUTES: readonly RouteMetadata[] = [
  { label: 'Dashboard', path: '/', group: 'Execute', icon: LayoutDashboard, keywords: 'home overview' },
  { label: 'Jobs', path: '/jobs', group: 'Execute', icon: Play, keywords: 'runs history executions' },
  { label: 'Templates', path: '/templates', group: 'Execute', icon: FileText, keywords: 'job template launch' },
  { label: 'Workflows', path: '/workflows', group: 'Execute', icon: Workflow, keywords: 'dag pipeline' },
  { label: 'Approvals', path: '/approvals', group: 'Execute', icon: ShieldCheck, keywords: 'workflow gates approve deny pending' },
  { label: 'Inventories', path: '/inventories', group: 'Execute', icon: Server, keywords: 'hosts groups fleet' },
  { label: 'Schedules & Triggers', path: '/schedules', group: 'Automate', icon: CalendarClock, keywords: 'cron webhook event' },
  { label: 'Execution Packs', path: '/execution-packs', group: 'Automate', icon: Package, keywords: 'runtime pack' },
  { label: 'Projects', path: '/projects', group: 'Automate', icon: GitBranch, keywords: 'scm git playbooks' },
  { label: 'Organizations', path: '/organizations', group: 'Govern', icon: Building2, keywords: 'org rbac' },
  { label: 'Users', path: '/users', group: 'Govern', icon: Users, keywords: 'people accounts' },
  { label: 'Teams', path: '/teams', group: 'Govern', icon: UsersRound, keywords: 'groups' },
  { label: 'Credentials', path: '/credentials', group: 'Govern', icon: KeyRound, keywords: 'secrets ssh vault' },
  { label: 'API Tokens', path: '/tokens', group: 'Govern', icon: KeySquare, keywords: 'pat bearer' },
  { label: 'Service Principals', path: '/service-principals', group: 'Govern', icon: Bot, keywords: 'application api delegated grants credentials' },
  { label: 'Activity', path: '/activity', group: 'Govern', icon: ScrollText, keywords: 'audit log' },
  { label: 'Settings', path: '/settings', group: 'Govern', icon: Settings, keywords: 'auth ldap config' },
] as const;

export const ROUTE_GROUPS = (['Execute', 'Automate', 'Govern'] as const).map(group => ({
  group,
  items: ROUTES.filter(route => route.group === group),
}));

const humanize = (segment: string) => {
  const decoded = (() => { try { return decodeURIComponent(segment); } catch { return segment; } })();
  return decoded.split('-').filter(Boolean).map(word => word.charAt(0).toUpperCase() + word.slice(1)).join(' ') || 'Page';
};

export function breadcrumbsFor(pathname: string): Breadcrumb[] {
  const segments = pathname.split('?')[0].split('#')[0].split('/').filter(Boolean);
  if (segments.length === 0) return [{ label: 'Dashboard' }];

  const rootPath = `/${segments[0]}`;
  const root = ROUTES.find(route => route.path === rootPath);
  const crumbs: Breadcrumb[] = [{ label: 'Dashboard', path: '/' }, { label: root?.label ?? humanize(segments[0]), path: rootPath }];

  if (segments[0] === 'jobs' && segments[1]) return [...crumbs, { label: `Job #${humanize(segments[1])}` }];
  if (segments[0] === 'workflows' && segments[1] === 'runs' && segments[2]) return [...crumbs, { label: `Run #${humanize(segments[2])}` }];

  if (segments[1] === 'org' && segments[2]) {
    crumbs.push({ label: `Organization #${humanize(segments[2])}`, path: `/${segments[0]}/org/${segments[2]}` });
    if (segments[3] === 'builder') crumbs.push({ label: segments[4] ? `Workflow #${humanize(segments[4])}` : 'New workflow' });
    return crumbs;
  }

  if (segments[0] === 'settings' && segments[1] === 'auth-providers') return [...crumbs, { label: 'Authentication providers' }];

  segments.slice(1).forEach((segment, index) => {
    const last = index === segments.length - 2;
    crumbs.push({ label: humanize(segment), path: last ? undefined : `/${segments.slice(0, index + 2).join('/')}` });
  });
  return crumbs;
}

export function documentTitleFor(pathname: string) {
  const current = breadcrumbsFor(pathname).at(-1)?.label ?? 'Dashboard';
  return current === 'Dashboard' ? 'Praetor' : `${current} · Praetor`;
}
