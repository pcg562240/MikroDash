import { execFileSync, spawnSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
const run = (cmd, args) => execFileSync(cmd, args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'] }).trim();
const repository = process.env.GITHUB_REPOSITORY || 'pcg562240/MikroDash';
const gh = args => run('gh', [...args, '--repo', repository]);
const meta = JSON.parse(readFileSync('localization/upstream.json', 'utf8'));
const release = JSON.parse(run('gh', ['api', `repos/${meta.repository}/releases/latest`]));
const tag = release.tag_name;
if (!/^v\d+\.\d+\.\d+$/.test(tag) || release.draft || release.prerelease) throw Error('Not a stable semantic-version release');
if (tag === meta.tag) { console.log(`Already tracking ${tag}`); process.exit(0); }
const branch = `update/upstream-${tag}`;
if (JSON.parse(gh(['pr', 'list', '--head', branch, '--state', 'open', '--json', 'number'])).length) {
  console.log('An update PR is already open. No changes made.'); process.exit(0);
}
run('git', ['config', 'user.name', 'github-actions[bot]']);
run('git', ['config', 'user.email', '41898282+github-actions[bot]@users.noreply.github.com']);
run('git', ['fetch', `https://github.com/${meta.repository}.git`, `refs/tags/${tag}:refs/tags/${tag}`]);
run('git', ['switch', '-c', branch]);
const merge = spawnSync('git', ['merge', '--no-ff', '-m', `Merge upstream ${tag} for Chinese review`, tag], { stdio: 'inherit' });
if (merge.status !== 0) {
  run('git', ['merge', '--abort']);
  const title = `Upstream ${tag}: manual merge required`;
  const issues = JSON.parse(gh(['issue', 'list', '--search', `${title} in:title`, '--state', 'open', '--json', 'number']));
  if (!issues.length) gh(['issue', 'create', '--title', title, '--body', `The merge from ${meta.tag} to ${tag} conflicts. Nothing was published or deployed. Review the workflow log and merge manually; never overwrite the Chinese layer.`]);
  throw Error('Upstream merge conflicts; an issue records the required review');
}
const commit = run('git', ['rev-parse', `${tag}^{commit}`]);
writeFileSync('localization/upstream.json', JSON.stringify({ ...meta, tag, commit, image: `ghcr.io/secops-7/mikrodash:${tag.slice(1)}` }, null, 2) + '\n');
for (const path of ['Dockerfile.zh-CN', 'docker-compose.zh-CN.yml', 'Dockerfile.traffic', 'docker-compose.traffic.yml']) {
  const old = readFileSync(path, 'utf8');
  if (!old.includes(meta.tag.slice(1))) throw Error(`Image version anchor moved in ${path}`);
  writeFileSync(path, old.replaceAll(meta.tag.slice(1), tag.slice(1)));
}
run('npm', ['ci', '--prefix', 'localization', '--no-audit', '--no-fund']);
run('node', ['localization/cli.mjs', 'extract']);
const check = spawnSync('node', ['localization/cli.mjs', 'check'], { stdio: 'inherit' });
const summary = JSON.parse(readFileSync('.localization-report/summary.json', 'utf8'));
run('git', ['add', 'localization/upstream.json', 'Dockerfile.zh-CN', 'docker-compose.zh-CN.yml', 'Dockerfile.traffic', 'docker-compose.traffic.yml']);
run('git', ['commit', '-m', `Track ${tag}; require translation and regression review`]);
run('git', ['push', 'origin', branch]);
const body = `## Upstream ${tag}\n\n${release.html_url}\n\nTranslation inventory: ${summary.translated}/${summary.strings}; ${summary.untranslated} preserved/untranslated.\n\n${check.status === 0 ? 'Existing coverage policy passes.' : '**New wording or obsolete exceptions require review. CI must pass before merge.**'}\n\n- [ ] Review new strings and safety adapter\n- [ ] Pass Chinese UI checks (explicitly dispatched below)\n- [ ] Review UI screenshots\n\nNo automatic merge, image release, or NAS deployment. Public source only; no production router credentials.`;
gh(['pr', 'create', '--base', 'zh-CN', '--head', branch, '--title', `Sync upstream ${tag} for Chinese review`, '--body', body]);
// Token-created PRs do not trigger pull_request CI. Dispatch explicitly rather
// than mistaking the absence of checks for a green result.
gh(['workflow', 'run', 'ci-zh.yml', '--ref', branch]);
