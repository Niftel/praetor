import { readFileSync, statSync } from 'node:fs';
import { resolve } from 'node:path';

const root = process.cwd();
const html = readFileSync(resolve(root, 'dist/index.html'), 'utf8');
const entry = html.match(/<script[^>]+src="\/assets\/([^\"]+\.js)"/i)?.[1];
if (!entry) throw new Error('Could not identify the JavaScript entry chunk in dist/index.html');

const bytes = statSync(resolve(root, 'dist/assets', entry)).size;
const budget = 250 * 1024;
if (bytes > budget) {
  throw new Error(`Initial JavaScript entry ${entry} is ${(bytes / 1024).toFixed(1)} KiB; budget is ${(budget / 1024).toFixed(0)} KiB`);
}
console.log(`Initial JavaScript entry: ${entry} ${(bytes / 1024).toFixed(1)} KiB / ${(budget / 1024).toFixed(0)} KiB budget`);
