import ts from 'typescript';
import { parseFragment } from 'parse5';

// Translate source-owned presentation literals only. Never walk the live DOM,
// translate API responses, or rewrite selectors, keys, form values or URLs.
export const normalize = text => text.replace(/\s+/g, ' ').trim();
const attributes = new Set(['title', 'placeholder', 'aria-label', 'alt']);
const protectedTags = new Set(['script', 'style', 'code', 'pre', 'textarea']);
const escapeHTML = text => text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
export function editsApplied(source, edits) {
  let end = source.length;
  for (const e of edits.sort((a, b) => b.start - a.start)) {
    if (e.end > end) throw new Error('Overlapping localization edits');
    source = source.slice(0, e.start) + e.text + source.slice(e.end);
    end = e.start;
  }
  return source;
}

export function translator(catalog, record = () => {}) {
  return (text, context) => {
    const key = normalize(text);
    if (!/[a-zA-Z]/.test(key.replace(/__MD_SLOT_\d+__/g, ''))) return text;
    // Fragments split across concatenations are not complete human text. Never
    // treat dangling markup/attributes, template tokens or technical values as prose.
    if (/[<>]/.test(key) || /(?:class|style|stroke|colspan|data-[\w-]+)=/.test(key) || /^\{\{/.test(key)) return text;
    record(key, context);
    const translated = Object.hasOwn(catalog, key) ? catalog[key] : undefined;
    if (translated === undefined) return text;
    const slots = s => [...s.matchAll(/__MD_SLOT_\d+__/g)].map(m => m[0]).sort().join(',');
    if (slots(key) !== slots(translated)) throw new Error(`Placeholder mismatch: ${key}`);
    return text.match(/^\s*/)[0] + translated + text.match(/\s*$/)[0];
  };
}

export function translateHTML(source, translate, context = '') {
  const edits = [];
  const tree = parseFragment(source, { sourceCodeLocationInfo: true });
  function walk(node, protectedParent = false) {
    // An option without an explicit value submits its text as the value.
    // Leave it untouched, rather than changing the configuration sent to ROS.
    const protect = protectedParent || protectedTags.has(node.tagName) ||
      (node.tagName === 'option' && !node.attrs?.some(a => a.name === 'value')) ||
      node.attrs?.some(a => a.name === 'translate' && a.value === 'no');
    const loc = node.sourceCodeLocation;
    if (!protect && node.nodeName === '#text' && loc) {
      const next = translate(node.value, `${context}:text`);
      if (next !== node.value) edits.push({ start: loc.startOffset, end: loc.endOffset, text: escapeHTML(next) });
    }
    if (!protect && loc?.attrs) {
      for (const a of node.attrs || []) {
        if (!attributes.has(a.name)) continue;
        const al = loc.attrs[a.name];
        if (!al) continue;
        const next = translate(a.value, `${context}:@${a.name}`);
        if (next !== a.value) edits.push({ start: al.startOffset, end: al.endOffset, text: `${a.name}="${escapeHTML(next)}"` });
      }
    }
    for (const child of node.childNodes || []) walk(child, protect);
    if (node.content) walk(node.content, protect);
  }
  walk(tree);
  return editsApplied(source, edits);
}

const propName = node => ts.isIdentifier(node) || ts.isStringLiteral(node) ? node.text : '';
const presentationProperties = new Set(['label', 'title', 'placeholder', 'passPlaceholder', 'hint', 'description', 'help', 'emptyText']);
const presentationAssignments = new Set(['textContent', 'innerText', 'title', 'placeholder']);
// Explicit argument positions, not a fuzzy name match. The first showError
// argument is a DOM id; translating it would disconnect the error from its UI.
const presentationCalls = new Map([
  ['alert', [0]], ['confirm', [0]], ['showError', [1]], ['loadBranding', [0]],
]);
const dataProperties = new Set(['value', 'defaultValue', 'key', 'id', 'name', 'host', 'address', 'url', 'path', 'method', 'body', 'command', 'script', 'comment', 'password', 'username']);
function dataLiteral(node) {
  let child = node;
  for (let p = node.parent; p; child = p, p = p.parent) {
    if (ts.isBinaryExpression(p) && p.operatorToken.kind === ts.SyntaxKind.EqualsToken && child === p.right && ts.isPropertyAccessExpression(p.left)) return dataProperties.has(p.left.name.text);
    if (ts.isPropertyAssignment(p) && child === p.initializer) return dataProperties.has(propName(p.name));
    if (ts.isBinaryExpression(p) && [ts.SyntaxKind.PlusToken, ts.SyntaxKind.BarBarToken, ts.SyntaxKind.QuestionQuestionToken].includes(p.operatorToken.kind)) continue;
    if (ts.isConditionalExpression(p) && child !== p.condition) continue;
    if (ts.isParenthesizedExpression(p)) continue;
    return false;
  }
  return false;
}
export function isPresentationLiteral(node) {
  let child = node;
  for (let p = node.parent; p; child = p, p = p.parent) {
    if (ts.isConditionalExpression(p) && child !== p.condition) continue;
    if (ts.isParenthesizedExpression(p)) continue;
    if (ts.isBinaryExpression(p)) {
      if (p.operatorToken.kind === ts.SyntaxKind.EqualsToken && child === p.right && ts.isPropertyAccessExpression(p.left)) {
        return presentationAssignments.has(p.left.name.text);
      }
      if ([ts.SyntaxKind.PlusToken, ts.SyntaxKind.BarBarToken, ts.SyntaxKind.QuestionQuestionToken].includes(p.operatorToken.kind)) continue;
      return false;
    }
    if (ts.isPropertyAssignment(p) && child === p.initializer) return presentationProperties.has(propName(p.name));
    if (ts.isCallExpression(p)) {
      const name = ts.isIdentifier(p.expression) ? p.expression.text : '';
      return presentationCalls.get(name)?.includes(p.arguments.indexOf(child)) || false;
    }
    if (ts.isReturnStatement(p)) {
      for (let f = p.parent; f; f = f.parent) {
        if (ts.isFunctionDeclaration(f)) return new Set(['emptyText', 'setupTestResultText']).has(f.name?.text);
      }
    }
    return false;
  }
  return false;
}

// Go schema metadata is owned by the application, not by a user's router.
// Scan it for coverage, but never rewrite Go, API field names or option values.
export function schemaMessages(source) {
  const entries = [];
  for (const match of source.matchAll(/\b(?:Label|Title|Help|Placeholder):\s*("(?:[^"\\]|\\.)*")/g)) {
    entries.push(JSON.parse(match[1]));
  }
  return entries;
}

export function translateTS(source, translate, filename = 'source.ts') {
  const sf = ts.createSourceFile(filename, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const edits = [];
  const templateEscape = text => text.replace(/\\/g, '\\\\').replace(/`/g, '\\`').replace(/\$\{/g, '\\${');
  function visit(node) {
    if (dataLiteral(node)) return;
    // Process a complete HTML concatenation before its pieces, so a chunk like
    // '">Delete</button>' never gets mistaken for prose containing attributes.
    if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.PlusToken) {
      const leaves = [];
      function flatten(n) {
        if (ts.isBinaryExpression(n) && n.operatorToken.kind === ts.SyntaxKind.PlusToken) { flatten(n.left); flatten(n.right); }
        else leaves.push(n);
      }
      flatten(node);
      if (ts.isStringLiteral(leaves[0]) || ts.isNoSubstitutionTemplateLiteral(leaves[0])) {
        const expressions = [];
        const text = leaves.map(n => {
          if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) return n.text;
          expressions.push(n);
          return `__MD_SLOT_${expressions.length - 1}__`;
        }).join('');
        if (/<\/?[a-zA-Z][^>]*>/.test(text)) {
          const next = translateHTML(text, translate, `${filename}:${sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1}`);
          if (next !== text) {
            const rendered = templateEscape(next).replace(/__MD_SLOT_(\d+)__/g, (_, i) => '${' + translateTS(expressions[Number(i)].getText(sf), translate, filename) + '}');
            edits.push({ start: node.getStart(sf), end: node.end, text: '`' + rendered + '`' });
            return;
          }
          for (const expression of expressions) visit(expression);
          return;
        }
      }
    }
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
      // Never touch property names, imports, types or tagged raw templates.
      if (node.parent && (ts.isTaggedTemplateExpression(node.parent) || ts.isLiteralTypeNode(node.parent) ||
          (ts.isPropertyAssignment(node.parent) && node.parent.name === node))) return;
      const html = /<\/?[a-zA-Z][^>]*>/.test(node.text);
      const line = sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1;
      const next = html ? translateHTML(node.text, translate, `${filename}:${line}`) :
        isPresentationLiteral(node) ? translate(node.text, `${filename}:${line}`) : node.text;
      if (next !== node.text) edits.push({ start: node.getStart(sf), end: node.end, text: JSON.stringify(next) });
      return;
    }
    if (ts.isTemplateExpression(node) && !ts.isTaggedTemplateExpression(node.parent)) {
      const expressions = node.templateSpans.map(s => s.expression);
      const text = node.head.text + node.templateSpans.map((s, i) => `__MD_SLOT_${i}__` + s.literal.text).join('');
      const line = sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1;
      const html = /<\/?[a-zA-Z][^>]*>/.test(text);
      const next = html ? translateHTML(text, translate, `${filename}:${line}`) :
        isPresentationLiteral(node) ? translate(text, `${filename}:${line}`) : text;
      if (next !== text) {
        // Expressions keep their source semantics, including esc(), values,
        // event names and API payloads. Only their presentation literals recurse.
        const rendered = templateEscape(next).replace(/__MD_SLOT_(\d+)__/g, (_, i) =>
          '${' + translateTS(expressions[Number(i)].getText(sf), translate, filename) + '}');
        edits.push({ start: node.getStart(sf), end: node.end, text: '`' + rendered + '`' });
        return;
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(sf);
  return editsApplied(source, edits);
}
