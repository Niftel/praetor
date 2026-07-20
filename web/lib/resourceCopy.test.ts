import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

describe('resource landing copy', () => {
  it('uses the correct irregular plural for inventories', () => {
    const landings = readFileSync(resolve(process.cwd(), 'pages/landings.tsx'), 'utf8');

    expect(landings).toContain('unit="inventory" pluralUnit="inventories"');
    expect(landings).not.toContain('inventorys');
  });
});
