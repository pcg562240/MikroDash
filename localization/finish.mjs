import { readFileSync, writeFileSync } from 'node:fs';
import { resolve, join } from 'node:path';
const dist = resolve(process.argv[2] || '.localization-build/web/dist');
const css = `\n/* Local CJK fallbacks: no external fonts or translation services. */\nhtml[lang="zh-CN"] body, html[lang="zh-CN"] button, html[lang="zh-CN"] input, html[lang="zh-CN"] select { font-family: var(--font-ui, Inter), "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif; }\nhtml[lang="zh-CN"] .sform-label, html[lang="zh-CN"] .sbtn, html[lang="zh-CN"] .nav-text { letter-spacing: normal; }\n`;
for (const name of ['index.html', 'login.html']) {
  let source = readFileSync(join(dist, name), 'utf8');
  if (!/<html lang="(?:en|zh-CN)"/.test(source)) throw new Error(`${name}: upstream document language anchor moved`);
  source = source.replace(/<html lang="(?:en|zh-CN)"/, '<html lang="zh-CN"');
  if (!source.includes('</head>')) throw new Error(`${name}: no head element`);
  source = source.replace('</head>', `<style>${css}</style></head>`);
  writeFileSync(join(dist, name), source);
}
