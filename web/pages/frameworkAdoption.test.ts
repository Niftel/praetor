import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const source = readFileSync(resolve(process.cwd(), 'pages/TemplatesPage.tsx'), 'utf8');
const projectsSource = readFileSync(resolve(process.cwd(), 'pages/ProjectsPage.tsx'), 'utf8');
const inventoriesSource = readFileSync(resolve(process.cwd(), 'pages/InventoriesPage.tsx'), 'utf8');
const accessSources = ['OrganizationsPage.tsx', 'TeamsPage.tsx', 'UsersPage.tsx'].map(file => ({
  file,
  source: readFileSync(resolve(process.cwd(), `pages/${file}`), 'utf8'),
}));

describe('Templates framework adoption', () => {
  it('keeps the route on shared page, toolbar, data, form, and loading primitives', () => {
    for (const primitive of ['Page', 'PageHeader', 'PageToolbar', 'DataTable', 'FormSection', 'FormErrorSummary', 'LoadingState']) {
      expect(source, `TemplatesPage must use ${primitive}`).toContain(primitive);
    }
  });

  it('does not restore superseded page-local loading or table structures', () => {
    expect(source).not.toContain('PageSpinner');
    expect(source).not.toContain('grid-cols-[minmax(260px,1fr)_130px_160px_190px_170px]');
  });
});

describe('Access-management framework adoption', () => {
  it('keeps every access route on shared page and state primitives', () => {
    for (const { file, source } of accessSources) {
      for (const primitive of ['Page', 'PageHeader', 'LoadingState', 'EmptyState']) {
        expect(source, `${file} must use ${primitive}`).toContain(primitive);
      }
      expect(source, `${file} must not restore PageSpinner`).not.toContain('PageSpinner');
    }
  });

  it('keeps organization and team creation on the shared form contract', () => {
    for (const { file, source } of accessSources.filter(entry => entry.file !== 'UsersPage.tsx')) {
      for (const primitive of ['FormSection', 'FormErrorSummary', 'FormActions']) {
        expect(source, `${file} must use ${primitive}`).toContain(primitive);
      }
    }
  });
});

describe('Projects framework adoption', () => {
  it('keeps the route on shared page, form, and state primitives', () => {
    for (const primitive of ['Page', 'PageHeader', 'LoadingState', 'EmptyState', 'FormSection', 'FormErrorSummary', 'FormActions', 'useDirtyFormGuard']) {
      expect(projectsSource, `ProjectsPage must use ${primitive}`).toContain(primitive);
    }
  });

  it('does not restore the superseded local page frame or spinner', () => {
    expect(projectsSource).not.toContain('PageSpinner');
    expect(projectsSource).not.toContain('max-w-[1060px] w-full mx-auto px-8 pt-7 pb-16');
  });
});

describe('Bulk-operation framework adoption', () => {
  it('keeps templates and inventories on the shared selection and result primitives', () => {
    for (const [file, pageSource] of [['TemplatesPage.tsx', source], ['InventoriesPage.tsx', inventoriesSource]] as const) {
      for (const primitive of ['useBulkSelection', 'BulkActionBar', 'BulkResultPanel']) {
        expect(pageSource, `${file} must use ${primitive}`).toContain(primitive);
      }
    }
  });

  it('opens inventory creation dialogs from normal click actions', () => {
    expect(inventoriesSource).toContain('onClick={() => { setAddMenu(false); setShowBulkHostModal(true); }}');
    expect(inventoriesSource).not.toContain('onMouseDown={() => setShowBulkHostModal(true)}');
  });
});
