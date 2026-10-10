// web/src/tree.js
import { $, $$, esc, api } from './state.js';
import { openFile } from './tabs.js';

export const treeEl = $('#tree');
export const openDirs = new Set();

/* git status letter -> CSS class + label. Empty/absent = clean, no badge. */
const GIT_STATUS = {
  M: ['git-M', 'modified'], A: ['git-A', 'added'], D: ['git-D', 'deleted'],
  U: ['git-untracked', 'untracked'], R: ['git-R', 'renamed'],
  C: ['git-A', 'copied'], '!': ['git-M', 'unmerged'],
};

export async function drawTree(dir, container, depth) {
  let j;
  try { j = await api('/api/tree', { dir }); } catch { return; }
  container.innerHTML = j.children.map(c => {
    const pad = 8 + depth * 12;
    // Ignored by .gitignore: still browsable, dimmed, and absent from search.
    const ig = c.ignored ? ' ignored' : '';
    const note = c.ignored ? ' (ignored by .gitignore, not searched)' : '';
    if (c.dir) {
      const dc = c.dirty ? ' dirty' : ''; // backend marks any ancestor of a change
      return '<div class="tw"><div class="tr dir' + ig + dc + '" data-dir="' + esc(c.path) + '" style="padding-left:' + pad + 'px" title="Folder: ' + esc(c.path) + note + '">' +
        '<span class="ar"></span>' + icon('folder') + '<span class="nm">' + esc(c.name) + '</span></div>' +
        '<div class="kids" data-kids="' + esc(c.path) + '"></div></div>';
    }
    const g = GIT_STATUS[c.status];
    const gc = g ? ' dirty ' + g[0] : '';
    const badge = g ? '<span class="gs" title="git: ' + g[1] + '">' + esc(c.status) + '</span>' : '';
    return '<div class="tr file' + ig + gc + '" data-file="' + esc(c.path) + '" style="padding-left:' + (pad + 12) + 'px" title="Open ' + esc(c.path) + note + '">' +
      icon(fileKind(c.name)) + '<span class="nm">' + esc(c.name) + '</span>' + badge + '</div>';
  }).join('');
}

/* A colour family per file kind, drawn in CSS. Emoji or icon fonts would be at
   the mercy of whatever the viewer has installed. */
export const FILE_KIND = {
  go: 'code', js: 'code', mjs: 'code', cjs: 'code', ts: 'code', tsx: 'code', jsx: 'code',
  py: 'code', rb: 'code', rs: 'code', java: 'code', kt: 'code', c: 'code', h: 'code',
  cc: 'code', cpp: 'code', hpp: 'code', cs: 'code', php: 'code', swift: 'code',
  lua: 'code', ex: 'code', exs: 'code', scala: 'code', dart: 'code', sh: 'code',
  bash: 'code', zsh: 'code', sql: 'code',
  json: 'data', yaml: 'data', yml: 'data', toml: 'data', ini: 'data', xml: 'data',
  csv: 'data', env: 'data', lock: 'data', mod: 'data', sum: 'data',
  md: 'doc', markdown: 'doc', txt: 'doc', rst: 'doc', adoc: 'doc',
  html: 'web', htm: 'web', css: 'web', scss: 'web', less: 'web', svg: 'web', vue: 'web',
  png: 'img', jpg: 'img', jpeg: 'img', gif: 'img', webp: 'img', ico: 'img', avif: 'img',
};

const SPECIAL_KIND = {
  'package.json': 'package', 'package-lock.json': 'lock', 'pnpm-lock.yaml': 'lock', 'yarn.lock': 'lock',
  'dockerfile': 'container', 'compose.yml': 'container', 'docker-compose.yml': 'container',
  'readme': 'doc', 'readme.md': 'doc', '.gitignore': 'config', '.gitattributes': 'config',
  '.env': 'config', 'makefile': 'code',
};

const ICON_PATH = {
  folder: '<path d="M1.8 4.5h4l1.5 1.7h6.9v6.7a1.2 1.2 0 0 1-1.2 1.2H3a1.2 1.2 0 0 1-1.2-1.2z"/><path d="M1.8 6.2h12.4"/>',
  code: '<path d="m5.8 4.2-3 3.8 3 3.8M10.2 4.2l3 3.8-3 3.8"/>',
  data: '<ellipse cx="8" cy="3.7" rx="4.6" ry="2"/><path d="M3.4 3.7v4.1c0 1.1 2.1 2 4.6 2s4.6-.9 4.6-2V3.7M3.4 7.8v4.1c0 1.1 2.1 2 4.6 2s4.6-.9 4.6-2V7.8"/>',
  doc: '<path d="M4 1.8h5l3 3v9.4H4z"/><path d="M9 1.8v3h3M5.8 8h4.6M5.8 10.4h4.6"/>',
  web: '<circle cx="8" cy="8" r="5.7"/><path d="M2.3 8h11.4M8 2.3c1.4 1.5 2.1 3.4 2.1 5.7S9.4 12.2 8 13.7M8 2.3C6.6 3.8 5.9 5.7 5.9 8s.7 4.2 2.1 5.7"/>',
  img: '<rect x="2" y="2.4" width="12" height="11.2" rx="1.4"/><circle cx="5.5" cy="5.6" r="1"/><path d="m3.3 11 3.3-3.1 2.2 2 1.7-1.5 2.2 2.6"/>',
  package: '<path d="m8 1.8 5 2.8v6.8l-5 2.8-5-2.8V4.6z"/><path d="m3 4.6 5 2.8 5-2.8M8 7.4v6.8"/>',
  lock: '<rect x="3.1" y="7" width="9.8" height="6.2" rx="1.3"/><path d="M5.2 7V5.2a2.8 2.8 0 0 1 5.6 0V7M8 9.4v1.5"/>',
  config: '<path d="M3 4h10M3 8h10M3 12h10"/><circle cx="5.5" cy="4" r="1.2"/><circle cx="10.5" cy="8" r="1.2"/><circle cx="7.5" cy="12" r="1.2"/>',
  container: '<rect x="2" y="3" width="12" height="10" rx="1.5"/><path d="M5 6h2M9 6h2M5 9h2M9 9h2"/>',
  other: '<path d="M4 1.8h5l3 3v9.4H4z"/><path d="M9 1.8v3h3"/>',
};

function icon(kind) {
  return '<svg class="ic" data-t="' + kind + '" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round">' + (ICON_PATH[kind] || ICON_PATH.other) + '</svg>';
}

export function fileKind(name) {
  const special = SPECIAL_KIND[name.toLowerCase()];
  if (special) return special;
  const i = name.lastIndexOf('.');
  return (i > 0 && FILE_KIND[name.slice(i + 1).toLowerCase()]) || 'other';
}

/* Expand the tree down to dir and scroll it into view. */
export async function revealDir(dir) {
  const parts = dir.split('/');
  for (let i = 0; i < parts.length; i++) {
    const p = parts.slice(0, i + 1).join('/');
    const row = treeEl.querySelector('[data-dir="' + CSS.escape(p) + '"]');
    if (!row) break;
    if (!row.classList.contains('open')) row.click();
    await new Promise(r => setTimeout(r, 30));
  }
  const last = treeEl.querySelector('[data-dir="' + CSS.escape(dir) + '"]');
  if (last) last.scrollIntoView({ block: 'center' });
}

export async function revealFile(path) {
  const idx = path.lastIndexOf('/');
  if (idx > 0) await revealDir(path.slice(0, idx));
  const row = treeEl.querySelector('[data-file="' + CSS.escape(path) + '"]');
  if (row) {
    $$('.tr.sel', treeEl).forEach(x => x.classList.remove('sel'));
    row.classList.add('sel');
    row.scrollIntoView({ block: 'center' });
  }
}

export function initTree() {
  // "Changed only" filter: hide clean files and known-clean folders (CSS-driven).
  $('#btn-changed')?.addEventListener('click', e => {
    const on = treeEl.classList.toggle('changed-only');
    e.currentTarget.classList.toggle('active', on);
  });

  treeEl.addEventListener('click', async e => {
    const dirRow = e.target.closest('[data-dir]');
    if (dirRow) {
      const path = dirRow.dataset.dir;
      const kids = treeEl.querySelector('[data-kids="' + CSS.escape(path) + '"]');
      const open = dirRow.classList.toggle('open');
      kids.classList.toggle('open', open);
      if (open) {
        openDirs.add(path);
        if (!kids.dataset.loaded) {
          kids.dataset.loaded = '1';
          await drawTree(path, kids, path.split('/').length);
        }
      } else openDirs.delete(path);
      return;
    }
    const f = e.target.closest('[data-file]');
    if (f) {
      f.classList.add('loading');
      const opened = await openFile(f.dataset.file);
      f.classList.remove('loading');
      if (!opened) return;
      $$('.tr.sel', treeEl).forEach(x => x.classList.remove('sel'));
      f.classList.add('sel');
    }
  });
}
