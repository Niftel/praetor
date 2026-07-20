import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const app = readFileSync(resolve(process.cwd(), 'App.tsx'), 'utf8');
const shell = readFileSync(resolve(process.cwd(), 'components/Shell.tsx'), 'utf8');

describe('route delivery contract', () => {
  it('keeps heavy authenticated routes as lazy imports', () => {
    for (const route of ['JobDetailPage', 'WorkflowBuilderPage', 'WorkflowRunPage', 'InventoriesPage', 'SchedulesPage']) {
      expect(app, `${route} must remain lazy`).toContain(`const ${route} = lazy(`);
      expect(app, `${route} must not become an eager default import`).not.toMatch(new RegExp(`import ${route} from`));
    }
  });

  it('keeps suspense inside the persistent authenticated shell', () => {
    expect(shell).toContain('<Suspense fallback={<LoadingState label="Loading page" />}>');
    expect(shell).toContain('<Outlet />');
  });
});
