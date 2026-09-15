import { test } from 'node:test';
import assert from 'node:assert/strict';
import { translator, translateHTML, translateTS } from '../lib.mjs';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const t = translator({ 'Save': '保存', 'Name': '名称', 'Home': '首页', 'Delete': '删除',
  'Search devices': '搜索设备', 'Delete __MD_SLOT_0__?': '删除 __MD_SLOT_0__？' });

test('HTML: text and human-facing attributes, not values or identifiers', () => {
  const html = '<label for="Name">Name</label><input id="Name" value="Home" placeholder="Search devices"><button data-action="Save">Save</button>';
  assert.equal(translateHTML(html, t), '<label for="Name">名称</label><input id="Name" value="Home" placeholder="搜索设备"><button data-action="Save">保存</button>');
});
test('HTML: preserve option submission values and code', () => {
  assert.equal(translateHTML('<option>Home</option><option value="Home">Home</option><code>Save</code><textarea>Save</textarea>', t),
    '<option>Home</option><option value="Home">首页</option><code>Save</code><textarea>Save</textarea>');
});
test('HTML: protect translate=no data and script/style', () => {
  const html = '<span translate="no">Home</span><script>"Save"</script><style>Save</style>';
  assert.equal(translateHTML(html, t), html);
});
test('TS: translate presentation literals, not selectors, identifiers or input values', () => {
  const source = 'button.textContent = "Save"; input.value = "Home"; querySelector("Name"); const x = {key:"Home", value:"Home", label:"Home"};';
  assert.equal(translateTS(source, t), 'button.textContent = "保存"; input.value = "Home"; querySelector("Name"); const x = {key:"Home", value:"Home", label:"首页"};');
});
test('TS: leave router-owned dynamic values and escaping intact', () => {
  const source = 'return `<td>${esc(device.name)}</td><td title="Delete">Delete</td>`;';
  assert.equal(translateTS(source, t), 'return `<td>${esc(device.name)}</td><td title="删除">删除</td>`;');
});
test('TS: interpolate complete phrases without altering expressions', () => {
  assert.equal(translateTS('confirm(`Delete ${esc(name)}?`);', t), 'confirm(`删除 ${esc(name)}？`);');
});
test('TS: not a key, type, URL or tagged raw template', () => {
  const source = 'type X = "Home"; const x = {"Home": true}; fetch("/Save"); String.raw`<b>Save</b>`;';
  assert.equal(translateTS(source, t), source);
});
test('unknown/new text falls back to English and is reported', () => {
  const found = [];
  const t = translator({}, key => found.push(key));
  assert.equal(translateHTML('<button>New upstream feature</button>', t), '<button>New upstream feature</button>');
  assert.deepEqual(found, ['New upstream feature']);
});
test('placeholder loss fails loudly', () => {
  assert.throws(() => translator({ 'Delete __MD_SLOT_0__?': '删除？' })('Delete __MD_SLOT_0__?', ''), /Placeholder mismatch/);
});
test('TS: complete concatenated HTML preserves dynamic values and attributes', () => {
  const source = 'return "<button data-id=\\\"" + esc(id) + "\\\" title=\\\"Delete\\\">Delete</button>";';
  const out = translateTS(source, t);
  assert.match(out, /\$\{esc\(id\)\}/);
  assert.match(out, /title="删除"/);
  assert.match(out, />删除<\/button>/);
});
test('TS: HTML stored as configuration is not translated', () => {
  const source = 'input.value = "<b>Save</b>"; const body = {value:"<b>Save</b>", command:"<b>Delete</b>"};';
  assert.equal(translateTS(source, t), source);
});
test('no template placeholder may be duplicated', () => {
  assert.throws(() => translator({ 'Delete __MD_SLOT_0__?': '__MD_SLOT_0__ __MD_SLOT_0__' })('Delete __MD_SLOT_0__?', ''), /Placeholder mismatch/);
});
test('schema adapter only changes display metadata, never submitted values or permissions', async () => {
  const raw = readFileSync(new URL('../resource-adapter.ts', import.meta.url), 'utf8');
  let js = ts.transpileModule(raw, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ES2020 } }).outputText;
  js = js.replace("import { zhCatalog } from './zh-catalog';", 'const zhCatalog = {"Name":"名称","Save":"保存","DNS Entry":"DNS 条目"};');
  const {localizeResourceSchema} = await import('data:text/javascript;base64,' + Buffer.from(js).toString('base64'));
  const input = {key:'dnsStatic',page:'dns',label:'DNS Entry',title:'DNS Entry',identity:'name',permitted:false,unsupported:false,ordered:true,
    fields:[{name:'Name',label:'Name',help:'Save',placeholder:'server.lan',options:['Home','Save'],showIf:{field:'type',in:['FWD']},value:'example.net',comment:'User comment: example.net'}],
    actions:[{key:'Save',label:'Save'}]};
  const before = structuredClone(input);
  const out = localizeResourceSchema(input);
  assert.deepEqual(input,before);
  assert.equal(out.fields[0].label,'名称');
  assert.equal(out.actions[0].label,'保存');
  assert.equal(out.actions[0].key,'Save');
  assert.deepEqual(out.fields[0].options,input.fields[0].options);
  assert.deepEqual(out.fields[0].showIf,input.fields[0].showIf);
  assert.equal(out.fields[0].name,'Name');
  assert.equal(out.fields[0].value,'example.net');
  assert.equal(out.fields[0].comment,'User comment: example.net');
  assert.equal(out.permitted,false);
  assert.equal(out.ordered,true);
  assert.equal(localizeResourceSchema({...input,actions:null,fields:null}).actions,null);
});

test('catalog lookup cannot inherit Object prototype entries', () => {
  assert.equal(translator({})('constructor', ''), 'constructor');
});

test('HTML translation escapes markup supplied by a catalog', () => {
  const unsafe = translator({'Save': '<img src=x onerror=alert(1)>'});
  const result = translateHTML('<button title="Save">Save</button>', unsafe);
  assert.ok(!result.includes('<img'));
  assert.ok(result.includes('&lt;img'));
});

test('all upstream HTML preserves control identities and submitted values', async () => {
  const {parseFragment} = await import('parse5');
  const {readdirSync} = await import('node:fs');
  const {join} = await import('node:path');
  const root = new URL('../../web/src/', import.meta.url);
  const catalog = JSON.parse(readFileSync(new URL('../zh-CN.json', import.meta.url), 'utf8'));
  function files(dir) {
    return readdirSync(dir, {withFileTypes:true}).flatMap(e => e.isDirectory() ? files(join(dir,e.name)) : [join(dir,e.name)]);
  }
  function signatures(source) {
    const result=[];
    function walk(n) {
      if(n.tagName) {
        const attrs=(n.attrs || []).filter(a=>!['title','placeholder','aria-label','alt'].includes(a.name));
        result.push([n.tagName,attrs]);
        if(n.tagName==='option' && !n.attrs.some(a=>a.name==='value')) result.push(['implicit-value',n.childNodes.map(c=>c.value||'').join('')]);
        if(['script','style','code','pre','textarea'].includes(n.tagName)) result.push(['protected-content',n.childNodes.map(c=>c.value||'').join('')]);
      }
      for(const c of n.childNodes || []) walk(c);
      if(n.content) walk(n.content);
    }
    walk(parseFragment(source));
    return result;
  }
  for(const file of files(root.pathname).filter(p=>p.endsWith('.html'))) {
    const source=readFileSync(file,'utf8');
    assert.deepEqual(signatures(translateHTML(source,translator(catalog))),signatures(source),file);
  }
});
