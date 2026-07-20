import { describe, expect, it } from 'vitest';
import { breadcrumbsFor, documentTitleFor, ROUTES, ROUTE_GROUPS } from './routeMetadata';

describe('route metadata', () => {
  it('drives the grouped navigation catalog without duplicates', () => {
    expect(ROUTE_GROUPS.flatMap(group => group.items)).toHaveLength(ROUTES.length);
    expect(new Set(ROUTES.map(route => route.path)).size).toBe(ROUTES.length);
  });

  it('creates semantic breadcrumbs for dynamic execution routes', () => {
    expect(breadcrumbsFor('/jobs/54').map(crumb => crumb.label)).toEqual(['Dashboard', 'Jobs', 'Job #54']);
    expect(breadcrumbsFor('/workflows/runs/11').map(crumb => crumb.label)).toEqual(['Dashboard', 'Workflows', 'Run #11']);
  });

  it('creates organization and builder context without exposing route syntax', () => {
    expect(breadcrumbsFor('/workflows/org/5/builder').map(crumb => crumb.label)).toEqual(['Dashboard', 'Workflows', 'Organization #5', 'New workflow']);
    expect(breadcrumbsFor('/workflows/org/5/builder/9').map(crumb => crumb.label)).toEqual(['Dashboard', 'Workflows', 'Organization #5', 'Workflow #9']);
  });

  it('uses safe human-readable labels for unknown routes', () => {
    expect(breadcrumbsFor('/future-resource/some-detail').map(crumb => crumb.label)).toEqual(['Dashboard', 'Future Resource', 'Some Detail']);
  });

  it('derives stable document titles', () => {
    expect(documentTitleFor('/')).toBe('Praetor');
    expect(documentTitleFor('/settings/auth-providers')).toBe('Authentication providers · Praetor');
  });
});
