import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const source = readFileSync(resolve(process.cwd(), 'pages/TemplatesPage.tsx'), 'utf8');

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
