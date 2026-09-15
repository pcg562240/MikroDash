import { readdirSync, readFileSync, writeFileSync, mkdirSync, cpSync, existsSync } from 'node:fs';
import { resolve, relative, join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { translateHTML, translateTS, translator, schemaMessages } from './lib.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, '..');
const catalog = JSON.parse(readFileSync(join(here, 'zh-CN.json'), 'utf8'));
const command = process.argv[2] || 'check';
const out = resolve(process.argv[3] || join(root, '.localization-build'));
const inventory = new Map();
const scan = translator({}, (key, context) => {
  if (!inventory.has(key)) inventory.set(key, new Set());
  inventory.get(key).add(context);
});
const translate = translator(catalog);
for (const message of schemaMessages(readFileSync(join(root, 'internal/resource/resource.go'), 'utf8'))) {
  scan(message, 'internal/resource/resource.go:display-metadata');
}
function files(dir) {
  return readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name)).flatMap(e =>
    e.isDirectory() ? files(join(dir, e.name)) : [join(dir, e.name)]);
}
if (command === 'prepare') {
  if (out === root || out === join(root, 'web') || existsSync(out)) {
    throw new Error('Output must be a new isolated directory, never the source tree');
  }
  mkdirSync(out, { recursive: true });
  cpSync(join(root, 'web'), join(out, 'web'), {
    recursive: true,
    filter: p => !p.split('/').some(part => ['node_modules', 'dist', 'test-out'].includes(part)),
  });
}
let changed = 0;
for (const file of files(join(root, 'web/src')).filter(p => /\.(html|ts)$/.test(p))) {
  const source = readFileSync(file, 'utf8');
  const name = relative(root, file);
  if (file.endsWith('.html')) translateHTML(source, scan, name); else translateTS(source, scan, name);
  let next = file.endsWith('.html') ? translateHTML(source, translate, name) : translateTS(source, translate, name);
  if (name === 'web/src/ui/login.html') next = next.replace('<html lang="en">', '<html lang="zh-CN">');
  if (next !== source) changed++;
  if (command === 'prepare') writeFileSync(join(out, name), next);
}
if (command === 'prepare') {
  const resource = join(out, 'web/src/resource.ts');
  let source = readFileSync(resource, 'utf8');
  const anchor = "socket.on('res:schema', (d) => {\n    if (!d || !d.key) return;";
  if (source.split(anchor).length !== 2) throw new Error('Upstream resource adapter anchor moved: review before building');
  source = "import { localizeResourceSchema } from './zh-resource-adapter';\n" + source.replace(anchor, anchor + '\n    d = localizeResourceSchema(d);');
  writeFileSync(resource, source);
  cpSync(join(here, 'resource-adapter.ts'), join(out, 'web/src/zh-resource-adapter.ts'));
  writeFileSync(join(out, 'web/src/zh-catalog.ts'), 'export const zhCatalog: Readonly<Record<string, string>> = ' + JSON.stringify(catalog) + ';\n');
}
const entries = [...inventory].sort(([a], [b]) => a.localeCompare(b)).map(([key, contexts]) => ({
  source: key, translation: catalog[key] ?? null, contexts: [...contexts],
}));
const missing = entries.filter(e => e.translation === null);
const summary = { strings: entries.length, translated: entries.length - missing.length, untranslated: missing.length, changedFiles: changed };
console.log(JSON.stringify(summary, null, 2));
const reportDir = command === 'prepare' ? out : join(root, '.localization-report');
mkdirSync(reportDir, { recursive: true });
writeFileSync(join(reportDir, 'inventory.json'), JSON.stringify(entries, null, 2) + '\n');
writeFileSync(join(reportDir, 'missing.json'), JSON.stringify(missing, null, 2) + '\n');
writeFileSync(join(reportDir, 'summary.json'), JSON.stringify(summary, null, 2) + '\n');
if (command === 'check') {
  const baselineFile = join(here, 'english-baseline.json');
  const baseline = existsSync(baselineFile) ? JSON.parse(readFileSync(baselineFile, 'utf8')) : [];
  const allowed = new Set(baseline);
  const newKeys = missing.filter(e => !allowed.has(e.source));
  const obsolete = baseline.filter(k => !inventory.has(k) || Object.hasOwn(catalog, k));
  if (newKeys.length || obsolete.length) {
    console.error(`New/unreviewed English: ${newKeys.length}; obsolete baseline entries: ${obsolete.length}. Review .localization-report/missing.json.`);
    process.exitCode = 1;
  }
}
