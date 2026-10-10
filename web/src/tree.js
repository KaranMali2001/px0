// web/src/tree.js
import { $, $$, esc, api, apiPost, apiPostJson, S } from './state.js';
import { openFile } from './tabs.js';
import { syncDiffView, focusDiffScope } from './diff.js';
import { showToast, copyToClipboard } from './ui.js';
import { closeSelMenu } from './selbar.js';
import { emit, on } from './bus.js';
import { isAutoRevealEnabled } from './settings.js';

export const treeEl = $('#tree');
const treeMenu = $('#tree-menu');
let activeTreeTarget = null;
export const openDirs = new Set();
// A newer tree action invalidates responses from older directory requests.
let expansionVersion = 0;

function setExpandBusy(busy) {
  const button = $('#btn-expand-tree');
  if (!button) return;
  button.classList.toggle('busy', busy);
  button.setAttribute('aria-busy', String(busy));
  button.setAttribute('aria-label', busy ? 'Expanding project folders' : 'Expand all project folders');
  button.title = busy ? 'Expanding project folders (Collapse all to stop)' : 'Expand all project folders (ignored folders stay closed)';
}

let saveDirsTimer = null;
function persistOpenDirs() {
  if (saveDirsTimer) clearTimeout(saveDirsTimer);
  saveDirsTimer = setTimeout(async () => {
    try {
      await apiPostJson('/api/session', { openDirs: Array.from(openDirs) });
    } catch {}
  }, 300);
}

/* git status letter -> CSS class + label. Empty/absent = clean, no badge. */
const GIT_STATUS = {
  M: ['git-M', 'modified'], A: ['git-A', 'added'], D: ['git-D', 'deleted'],
  U: ['git-untracked', 'untracked'], R: ['git-R', 'renamed'],
  C: ['git-A', 'copied'], '!': ['git-M', 'unmerged'],
};

export async function drawTree(dir, container, depth, isCurrent) {
  let j;
  try { j = await api('/api/tree', { dir }); } catch { return false; }
  if (isCurrent && !isCurrent()) return false;
  container.innerHTML = j.children.map(c => {
    const pad = 8 + depth * 12;
    // Ignored by .gitignore: still browsable, dimmed, and absent from search.
    const ig = c.ignored ? ' ignored' : '';
    const note = c.ignored ? ' (ignored by .gitignore, not searched)' : '';
    const menuBtn = '<button class="tr-menu-btn" title="Actions" aria-label="Actions" tabindex="-1"><svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><circle cx="2.5" cy="8" r="1.5"/><circle cx="8" cy="8" r="1.5"/><circle cx="13.5" cy="8" r="1.5"/></svg></button>';
    if (c.dir) {
      const dc = c.dirty ? ' dirty' : ''; // backend marks any ancestor of a change
      const ydc = c.yourDirty ? ' your-dirty' : '';
      return '<div class="tw"><div class="tr dir' + ig + dc + ydc + '" data-dir="' + esc(c.path) + '" style="padding-left:' + pad + 'px" title="Folder: ' + esc(c.path) + note + '">' +
        '<span class="ar"></span>' + icon('folder') + '<span class="nm">' + esc(c.name) + '</span>' + menuBtn + '</div>' +
        '<div class="kids" data-kids="' + esc(c.path) + '"></div></div>';
    }
    const g = GIT_STATUS[c.status];
    const gc = g ? ' dirty ' + g[0] : '';
    const isYou = !!c.yourStatus;
    const yc = (isYou ? ' your-change' : '') + (prFileMap()?.[c.path] ? ' pr-file' : '');
    const youBadge = isYou ? '<span class="gs-you-tag" title="Modified by you in this review session">YOU</span>' : '';
    const badge = g ? '<span class="gs' + (isYou ? ' gs-you' : '') + '" title="' + (isYou ? 'Your change (' + c.yourStatus + '), git: ' + g[1] : 'git: ' + g[1]) + '">' + esc(c.status) + '</span>' + youBadge : '';
    const showTick = S.meta?.pr ? isYou : !!g;
    const tick = showTick ? '<button class="stage-tick' + (c.staged ? ' staged' : '') + '" data-stage="' + esc(c.path) + '" title="' + (c.staged ? 'Unstage' : 'Stage') + '"></button>' : '';
    return '<div class="tr file' + ig + gc + yc + '" data-file="' + esc(c.path) + '"' + (c.status ? ' data-st="' + esc(c.status) + '"' : '') + (c.yourStatus ? ' data-yst="' + esc(c.yourStatus) + '"' : '') + ' style="padding-left:' + (pad + 12) + 'px" title="Open ' + esc(c.path) + note + '">' +
      icon(fileKind(c.name)) + '<span class="nm">' + esc(c.name) + '</span>' + badge + tick + menuBtn + '</div>';
  }).join('');
  relabelTreeBadges();
  return true;
}

/* The paths the PR itself changes (server: diffBase..PR head), as a Set. Rows in
   the sidebar's "PR changes" scope are limited to these; a file only you have
   touched belongs to "Yours". Null outside PR review. */
const prFileMap = () => S.meta?.pr?.files || null;

/* The badge letter follows the sidebar scope. "PR changes" keeps the letter the
   PR itself gave the file (A, M, D...) however you have edited it since; "Yours"
   shows your change measured from the PR head; otherwise git's combined status.
   Rows remember both letters (data-st / data-yst) so a scope switch can relabel
   them without another fetch. */
function scopedCode(path, st, yst) {
  if (treeEl.classList.contains('scope-pr')) return prFileMap()?.[path] || st;
  if (treeEl.classList.contains('scope-yours')) return yst || st;
  return st;
}

export function relabelTreeBadges() {
  for (const row of treeEl.querySelectorAll('.tr.file[data-st]')) {
    const badge = row.querySelector('.gs');
    if (!badge) continue;
    const code = scopedCode(row.dataset.file, row.dataset.st, row.dataset.yst);
    const g = GIT_STATUS[code];
    if (!g) continue;
    row.classList.remove('git-M', 'git-A', 'git-D', 'git-untracked', 'git-R');
    row.classList.add(g[0]);
    badge.textContent = code;
    const you = !!row.dataset.yst;
    badge.title = you && !treeEl.classList.contains('scope-pr')
      ? 'Your change (' + row.dataset.yst + '), git: ' + g[1] : 'git: ' + g[1];
  }
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

export async function refreshTree() {
  const version = ++expansionVersion;
  const isCurrent = () => version === expansionVersion;
  setExpandBusy(false);
  if (!(await drawTree('', treeEl, 0, isCurrent))) return;
  const dirs = Array.from(openDirs).sort((a, b) => a.split('/').length - b.split('/').length);
  for (const path of dirs) {
    if (!isCurrent()) return;
    const dirRow = treeEl.querySelector('[data-dir="' + CSS.escape(path) + '"]');
    const kids = treeEl.querySelector('[data-kids="' + CSS.escape(path) + '"]');
    if (kids && dirRow) {
      dirRow.classList.add('open');
      kids.classList.add('open');
      const loaded = await drawTree(path, kids, path.split('/').length, isCurrent);
      if (!isCurrent()) return;
      if (loaded) kids.dataset.loaded = '1';
      else {
        dirRow.classList.remove('open');
        kids.classList.remove('open');
        openDirs.delete(path);
      }
    } else {
      openDirs.delete(path);
    }
  }
  if (isCurrent() && treeEl.classList.contains('changed-only')) {
    await expandDirtyDirs();
  }
  markTreeCleanState();
}

export function restoreOpenDirs(dirs) {
  if (Array.isArray(dirs)) {
    for (const d of dirs) {
      if (typeof d === 'string') openDirs.add(d);
    }
  }
}

export function collapseAllDirs() {
  expansionVersion++;
  openDirs.clear();

  treeEl.querySelectorAll('.tr.dir.open').forEach(row => {
    row.classList.remove('open');
  });

  treeEl.querySelectorAll('.kids.open').forEach(kids => {
    kids.classList.remove('open');
  });

  setExpandBusy(false);
  persistOpenDirs();
}

export async function expandAllDirs() {
  if (treeEl.classList.contains('changed-only') || $('#btn-expand-tree')?.classList.contains('busy')) return;
  const version = ++expansionVersion;
  setExpandBusy(true);
  const queue = Array.from(treeEl.querySelectorAll('.tr.dir'), row => row.dataset.dir);
  const seen = new Set();
  let failed = 0;

  try {
    while (queue.length && version === expansionVersion && !treeEl.classList.contains('changed-only')) {
      const batch = queue.splice(0, 4);
      const descendants = await Promise.all(batch.map(async path => {
        if (seen.has(path)) return [];
        seen.add(path);
        const row = treeEl.querySelector('[data-dir="' + CSS.escape(path) + '"]');
        const kids = treeEl.querySelector('[data-kids="' + CSS.escape(path) + '"]');
        if (!row || !kids) return [];
        if (row.classList.contains('ignored')) return [];

        row.classList.add('open');
        kids.classList.add('open');
        openDirs.add(path);
        if (!kids.dataset.loaded) {
          const loaded = await drawTree(path, kids, path.split('/').length, () => version === expansionVersion);
          if (!loaded) {
            if (version === expansionVersion) {
              row.classList.remove('open');
              kids.classList.remove('open');
              openDirs.delete(path);
              failed++;
            }
            return [];
          }
          kids.dataset.loaded = '1';
        }
        if (version !== expansionVersion) return [];
        return Array.from(kids.querySelectorAll(':scope > .tw > .tr.dir'), child => child.dataset.dir);
      }));
      for (const paths of descendants) queue.push(...paths);
    }
  } finally {
    if (version === expansionVersion) {
      persistOpenDirs();
      setExpandBusy(false);
      if (failed) showToast('!', failed + (failed === 1 ? ' folder could not be loaded' : ' folders could not be loaded'));
    }
  }
}

/* Expand the tree down to dir and scroll it into view. */
export async function revealDir(dir) {
  const version = ++expansionVersion;
  const isCurrent = () => version === expansionVersion;
  setExpandBusy(false);
  const parts = dir.split('/');
  for (let i = 0; i < parts.length; i++) {
    if (!isCurrent()) return;
    const p = parts.slice(0, i + 1).join('/');
    const row = treeEl.querySelector('[data-dir="' + CSS.escape(p) + '"]');
    if (!row) break;
    if (!row.classList.contains('open')) {
      row.classList.add('open');
      const kids = treeEl.querySelector('[data-kids="' + CSS.escape(p) + '"]');
      if (kids) {
        kids.classList.add('open');
        openDirs.add(p);
        const loaded = await drawTree(p, kids, p.split('/').length, isCurrent);
        if (!isCurrent()) return;
        if (loaded) kids.dataset.loaded = '1';
        else {
          row.classList.remove('open');
          kids.classList.remove('open');
          openDirs.delete(p);
          break;
        }
      }
    }
  }
  if (!isCurrent()) return;
  const last = treeEl.querySelector('[data-dir="' + CSS.escape(dir) + '"]');
  if (last) last.scrollIntoView({ block: 'center' });
  persistOpenDirs();
}

export async function revealFile(path) {
  if (!path) return;
  path = path.replace(/^\/+/, '');
  const idx = path.lastIndexOf('/');
  if (idx > 0) await revealDir(path.slice(0, idx));
  const row = treeEl.querySelector('[data-file="' + CSS.escape(path) + '"]');
  if (row) {
    $$('.tr.sel', treeEl).forEach(x => x.classList.remove('sel'));
    row.classList.add('sel');
    row.scrollIntoView({ block: 'center' });
  }
}

export async function expandDirtyDirs(container = treeEl, version = expansionVersion) {
  const isCurrent = () => version === expansionVersion && treeEl.classList.contains('changed-only');
  if (!isCurrent()) return;
  const dirtyRows = Array.from(container.querySelectorAll('.tr.dir.dirty:not(.open)'));
  for (const dirRow of dirtyRows) {
    if (!isCurrent()) return;
    const path = dirRow.dataset.dir;
    const kids = container.querySelector('[data-kids="' + CSS.escape(path) + '"]');
    if (kids) {
      dirRow.classList.add('open');
      kids.classList.add('open');
      openDirs.add(path);
      const loaded = await drawTree(path, kids, path.split('/').length, isCurrent);
      if (!isCurrent()) return;
      if (loaded) {
        kids.dataset.loaded = '1';
        await expandDirtyDirs(kids, version);
      } else {
        dirRow.classList.remove('open');
        kids.classList.remove('open');
        openDirs.delete(path);
      }
    }
  }
  if (isCurrent()) persistOpenDirs();
}

export async function patchTreeGitStatus(statuses = {}, dirtyDirs = {}, staged = {}, yourStatuses = {}, yourDirtyDirs = {}) {
  // Check if any changed files belong to an open directory (or root) but don't have a DOM element yet,
  // or if any previously deleted files are now clean (so they should disappear from the tree).
  let needRefresh = false;
  for (const [p, code] of Object.entries(statuses)) {
    const fileRow = treeEl.querySelector('[data-file="' + CSS.escape(p) + '"]');
    if (!fileRow) {
      const lastSlash = p.lastIndexOf('/');
      const parentDir = lastSlash >= 0 ? p.slice(0, lastSlash) : '';
      if (parentDir === '' || openDirs.has(parentDir)) {
        needRefresh = true;
        break;
      }
    }
  }

  if (!needRefresh) {
    const dirtyFiles = treeEl.querySelectorAll('.tr.file.dirty');
    for (const fileRow of dirtyFiles) {
      const p = fileRow.dataset.file;
      if (!statuses[p] && fileRow.classList.contains('git-D')) {
        needRefresh = true;
        break;
      }
    }
  }

  if (needRefresh) {
    await refreshTree();
    markTreeCleanState();
    return;
  }

  // 1. Update folder dirty and your-dirty classes
  const dirRows = treeEl.querySelectorAll('.tr.dir');
  for (const dirRow of dirRows) {
    const p = dirRow.dataset.dir;
    dirRow.classList.toggle('dirty', !!dirtyDirs[p]);
    dirRow.classList.toggle('your-dirty', !!yourDirtyDirs[p]);
  }

  // 2. Clear stale dirty/status markers on files that are now clean
  const dirtyFiles = treeEl.querySelectorAll('.tr.file.dirty');
  for (const fileRow of dirtyFiles) {
    const p = fileRow.dataset.file;
    if (!statuses[p]) {
      fileRow.classList.remove('dirty', 'git-M', 'git-A', 'git-D', 'git-untracked', 'git-R', 'your-change');
      const badge = fileRow.querySelector('.gs');
      if (badge) badge.remove();
      const youTag = fileRow.querySelector('.gs-you-tag');
      if (youTag) youTag.remove();
      const tick = fileRow.querySelector('.stage-tick');
      if (tick) tick.remove();
    }
  }

  // 3. Update or apply badges + stage tick for changed files
  for (const [p, code] of Object.entries(statuses)) {
    const fileRow = treeEl.querySelector('[data-file="' + CSS.escape(p) + '"]');
    if (!fileRow) continue;
    const g = GIT_STATUS[code];
    const isYou = !!yourStatuses[p];
    fileRow.classList.remove('git-M', 'git-A', 'git-D', 'git-untracked', 'git-R');
    fileRow.classList.toggle('your-change', isYou);
    if (code) fileRow.dataset.st = code; else delete fileRow.dataset.st;
    if (yourStatuses[p]) fileRow.dataset.yst = yourStatuses[p]; else delete fileRow.dataset.yst;
    if (g) {
      fileRow.classList.add('dirty', g[0]);
      let badge = fileRow.querySelector('.gs');
      if (!badge) {
        badge = document.createElement('span');
        badge.className = 'gs';
        fileRow.appendChild(badge);
      }
      badge.classList.toggle('gs-you', isYou);
      badge.title = isYou ? 'Your change (' + yourStatuses[p] + '), git: ' + g[1] : 'git: ' + g[1];
      badge.textContent = code;

      let youTag = fileRow.querySelector('.gs-you-tag');
      if (isYou) {
        if (!youTag) {
          youTag = document.createElement('span');
          youTag.className = 'gs-you-tag';
          youTag.title = 'Modified by you in this review session';
          youTag.textContent = 'YOU';
          badge.after(youTag);
        }
      } else if (youTag) {
        youTag.remove();
      }

      const showTick = S.meta?.pr ? isYou : true;
      let tick = fileRow.querySelector('.stage-tick');
      if (showTick) {
        if (!tick) {
          tick = document.createElement('button');
          tick.className = 'stage-tick';
          tick.dataset.stage = p;
          fileRow.appendChild(tick);
        }
        tick.classList.toggle('staged', !!staged[p]);
        tick.title = staged[p] ? 'Unstage' : 'Stage';
      } else if (tick) {
        tick.remove();
      }
    } else {
      fileRow.classList.remove('dirty', 'your-change');
      const badge = fileRow.querySelector('.gs');
      if (badge) badge.remove();
      const youTag = fileRow.querySelector('.gs-you-tag');
      if (youTag) youTag.remove();
      const tick = fileRow.querySelector('.stage-tick');
      if (tick) tick.remove();
    }
    const menuBtn = fileRow.querySelector('.tr-menu-btn');
    if (menuBtn) fileRow.appendChild(menuBtn);
  }

  // Clean up any remaining .your-change on files no longer in yourStatuses
  const yourFiles = treeEl.querySelectorAll('.tr.file.your-change');
  for (const fileRow of yourFiles) {
    const p = fileRow.dataset.file;
    if (!yourStatuses[p]) {
      fileRow.classList.remove('your-change');
      fileRow.querySelector('.gs')?.classList.remove('gs-you');
      fileRow.querySelector('.gs-you-tag')?.remove();
      if (S.meta?.pr) {
        fileRow.querySelector('.stage-tick')?.remove();
      }
    }
  }

  relabelTreeBadges();

  // If in changed-only mode, auto-expand any newly dirty directories
  markTreeCleanState();
  if (treeEl.classList.contains('changed-only')) {
    await expandDirtyDirs();
  }
}

/* Whether the Git view has anything to show. Unpushed commits count: once you
   commit, the working tree goes clean but the commits are still unreviewed, and
   the Unpushed section under the tree (unpushed.js, which owns S.unpushedCount)
   is exactly where they show up. */
/* In Git view with a clean working tree the tree renders nothing (every row is
   filtered out), so it stops claiming the sidebar's spare height and says why.
   The Unpushed section below it is then the whole of the view. */
export function markTreeCleanState() {
  const hasDirtyRows = !!treeEl?.querySelector('.tr.file.dirty');
  const hasChanges = (S.meta?.gitChanges > 0) || hasDirtyRows;
  treeEl?.classList.toggle('no-changes',
    treeEl?.classList.contains('changed-only') && !hasChanges);
}

export function hasGitView() {
  return !!(S.meta?.git && ((S.meta.gitChanges > 0) || (S.unpushedCount > 0)));
}

export function inGitMode() {
  return !!treeEl?.classList.contains('changed-only');
}

export function updateSidebarToggleState() {
  const btnChanged = $('#btn-changed');
  if (!btnChanged) return;
  const available = hasGitView();
  btnChanged.disabled = !available;
  btnChanged.classList.toggle('disabled', !available);
  if (!S.meta?.git) {
    btnChanged.title = 'Git not available in workspace';
  } else if (!available) {
    btnChanged.title = 'There are no git modified files.';
  } else if (!(S.meta.gitChanges > 0)) {
    btnChanged.title = 'Git changes (working tree clean — showing unpushed commits)';
  } else {
    btnChanged.title = 'Git changes (show changed files only)';
  }
}

export async function setSidebarMode(mode) {
  const btnChanged = $('#btn-changed');
  const btnFiles = $('#btn-files');
  const btnCollapse = $('#btn-collapse-tree');
  const btnExpand = $('#btn-expand-tree');
  updateSidebarToggleState();

  if (mode === 'git' && hasGitView()) {
    treeEl.classList.add('changed-only');
    btnChanged?.classList.add('active');
    btnFiles?.classList.remove('active');
    if (btnCollapse) btnCollapse.hidden = true;
    if (btnExpand) btnExpand.hidden = true;
    expansionVersion++;
    setExpandBusy(false);
    await expandDirtyDirs();
  } else {
    treeEl.classList.remove('changed-only');
    btnFiles?.classList.add('active');
    btnChanged?.classList.remove('active');
    if (btnCollapse) btnCollapse.hidden = false;
    if (btnExpand) btnExpand.hidden = false;
  }
  markTreeCleanState();
  // unpushed.js hangs its section off this; tree.js doesn't import it.
  emit('sidebar:mode', { mode: treeEl.classList.contains('changed-only') ? 'git' : 'files' });
}

export function closeTreeMenu() {
  activeTreeTarget = null;
  if (treeMenu && !treeMenu.hidden) treeMenu.hidden = true;
}

export function openTreeMenu(row, x, y) {
  closeTreeMenu();
  closeSelMenu();

  const isDir = row.classList.contains('dir') || row.dataset.dir !== undefined;
  const path = row.dataset.dir || row.dataset.file || '';
  if (!path) return;
  const name = path.split('/').pop() || path;
  activeTreeTarget = { path, isDir, name, row };

  if (!treeMenu) return;
  treeMenu.replaceChildren();

  const items = [
    { action: 'copy-relative', label: 'Copy Relative Path' },
    { action: 'copy-name', label: isDir ? 'Copy Folder Name' : 'Copy File Name' },
    { action: 'copy-absolute', label: 'Copy Absolute Path' },
  ];

  for (const item of items) {
    const btn = document.createElement('button');
    btn.className = 'sel-menu-item';
    btn.dataset.action = item.action;
    btn.setAttribute('role', 'menuitem');
    const label = document.createElement('span');
    label.textContent = item.label;
    btn.append(label);
    treeMenu.append(btn);
  }

  treeMenu.hidden = false;
  const w = treeMenu.offsetWidth, h = treeMenu.offsetHeight;
  treeMenu.style.left = Math.max(4, x + w > innerWidth - 4 ? x - w : x) + 'px';
  treeMenu.style.top = Math.max(4, y + h > innerHeight - 4 ? y - h : y) + 'px';
}

export function initTree() {
  updateSidebarToggleState();

  if (treeMenu) {
    treeMenu.addEventListener('mousedown', e => e.preventDefault());
    treeMenu.addEventListener('click', e => {
      const btn = e.target.closest('[data-action]');
      if (!btn || !activeTreeTarget) return;
      const { path, name } = activeTreeTarget;
      const act = btn.dataset.action;
      closeTreeMenu();

      if (act === 'copy-relative') {
        copyToClipboard(path, 'Copied ' + path);
      } else if (act === 'copy-name') {
        copyToClipboard(name, 'Copied ' + name);
      } else if (act === 'copy-absolute') {
        const root = S.meta?.root ? S.meta.root.replace(/\/+$/, '') : '';
        const fullPath = root ? (path.startsWith('/') ? path : root + '/' + path) : path;
        copyToClipboard(fullPath, 'Copied ' + fullPath);
      }
    });

    document.addEventListener('mousedown', e => {
      if (!treeMenu.hidden && !treeMenu.contains(e.target)) closeTreeMenu();
    }, true);
    addEventListener('keydown', e => { if (e.key === 'Escape') closeTreeMenu(); });
    addEventListener('resize', closeTreeMenu);
    addEventListener('blur', closeTreeMenu);
    document.addEventListener('scroll', closeTreeMenu, true);
  }

  $('#btn-collapse-tree')?.addEventListener('click', () => {
    if (treeEl.classList.contains('changed-only')) return;
    collapseAllDirs();
  });

  $('#btn-expand-tree')?.addEventListener('click', () => {
    expandAllDirs();
  });

  $('#btn-changed')?.addEventListener('click', async () => {
    if (!hasGitView()) return;
    await setSidebarMode('git');
  });

  $('#btn-files')?.addEventListener('click', async () => {
    await setSidebarMode('files');
  });

  treeEl.addEventListener('contextmenu', e => {
    const row = e.target.closest('[data-file], [data-dir]');
    if (!row) return;
    e.preventDefault();
    e.stopPropagation();
    openTreeMenu(row, e.clientX, e.clientY);
  });

  treeEl.addEventListener('click', async e => {
    const menuBtn = e.target.closest('.tr-menu-btn');
    if (menuBtn) {
      e.stopPropagation();
      e.preventDefault();
      const row = menuBtn.closest('[data-file], [data-dir]');
      if (row) {
        const at = menuBtn.getBoundingClientRect();
        openTreeMenu(row, at.right + 2, at.top);
      }
      return;
    }
    const tick = e.target.closest('.stage-tick');
    if (tick) {
      e.stopPropagation();
      const path = tick.dataset.stage;
      const staged = tick.classList.toggle('staged'); // optimistic; the git-status stream reconciles it
      tick.title = staged ? 'Unstage' : 'Stage';
      try {
        await apiPostJson(staged ? '/api/git/stage' : '/api/git/unstage', { path });
      } catch (err) {
        tick.classList.toggle('staged', !staged);
        tick.title = staged ? 'Stage' : 'Unstage';
        showToast('!', err.message || 'Could not update staging');
      }
      return;
    }
    const dirRow = e.target.closest('[data-dir]');
    if (dirRow) {
      expansionVersion++;
      setExpandBusy(false);
      const path = dirRow.dataset.dir;
      const kids = treeEl.querySelector('[data-kids="' + CSS.escape(path) + '"]');
      if (!kids) return;
      const open = dirRow.classList.toggle('open');
      kids.classList.toggle('open', open);
      if (open) {
        openDirs.add(path);
        if (!kids.dataset.loaded) {
          const loaded = await drawTree(path, kids, path.split('/').length);
          if (loaded) kids.dataset.loaded = '1';
          else {
            dirRow.classList.remove('open');
            kids.classList.remove('open');
            openDirs.delete(path);
          }
        }
      } else openDirs.delete(path);
      persistOpenDirs();
      return;
    }
    const f = e.target.closest('[data-file]');
    if (f) {
      // The scope picks the section: "Yours" opens Your changes with the PR's
      // folded, "PR changes" the reverse -- also for a tab that is already open
      // and has been toggled the other way.
      const t = S.tabs.find(x => x.path === f.dataset.file);
      if (t) focusDiffScope(t);
      f.classList.add('loading');
      const opened = await openFile(f.dataset.file);
      f.classList.remove('loading');
      if (!opened) return;
      if (t) syncDiffView();
      $$('.tr.sel', treeEl).forEach(x => x.classList.remove('sel'));
      f.classList.add('sel');
    }
  });

  on('tab:activated', ({ doc }) => {
    if (!doc || !doc.path || !isAutoRevealEnabled()) return;
    if (document.body.classList.contains('side-hidden')) return;
    revealFile(doc.path);
  });
}
