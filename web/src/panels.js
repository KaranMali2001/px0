// web/src/panels.js
import { $, $$, S, api, persistPreference } from './state.js';
import { layout, render } from './renderer.js';
import { updateStatus } from './status.js';
import { loadOutline } from './outline.js';
import { treeEl, refreshTree, setSidebarMode, hasGitView } from './tree.js';
import { reloadOpenTabs } from './tabs.js';
import { showToast } from './ui.js';
import { refreshUnpushed } from './unpushed.js';

export async function showPanel(name) {
  setSidebarVisible(true);
  if (name === 'files') {
    await setSidebarMode('files');
  }
}

export function setSidebarLocation(location) {
  document.body.classList.toggle('side-right', location === 'right');
  layout();
  render();
}

export function setSidebarVisible(visible, persist = false) {
  document.body.classList.toggle('side-hidden', !visible);
  if (persist) persistPreference('workbench.sideBar.visible', visible).catch(() => {});
  layout();
  render();
}

export function toggleSidebar() {
  setSidebarVisible(document.body.classList.contains('side-hidden'), true);
}

function setSidebarWidth(width, persist = false) {
  const next = Math.max(170, Math.min(620, Math.round(width)));
  $('#side').style.width = next + 'px';
  if (persist) persistPreference('workbench.sideBar.width', next).catch(() => {});
}

export async function reindexWorkspace() {
  const btn = $('#btn-reindex');
  const svg = btn?.querySelector('svg');
  if (svg) svg.classList.add('spin');
  try {
    const j = await api('/api/reindex');
    S.meta.files = j.files; S.meta.indexMs = j.indexMs;
    if (j.gitChanges !== undefined) S.meta.gitChanges = j.gitChanges;
    if (j.gitFiles !== undefined) S.meta.gitFiles = j.gitFiles;
    // A refresh re-reads the unpushed list too: commits may have been made,
    // amended or pushed from a terminal since the last git-status tick, and
    // hasGitView() below counts them.
    await refreshUnpushed();
    await setSidebarMode(hasGitView() ? 'git' : 'files');
    await refreshTree();
    await reloadOpenTabs();
    updateStatus();
    showToast('✓', 'Workspace refreshed');
  } catch (e) {
    showToast('!', 'Refresh failed: ' + e.message);
  } finally {
    if (svg) svg.classList.remove('spin');
  }
}

export function initPanels() {
  $('#btn-reindex').addEventListener('click', reindexWorkspace);

  /* sidebar resize */
  (() => {
    const rz = $('#resizer'); let dragging = false;
    rz.addEventListener('mousedown', e => { dragging = true; rz.classList.add('drag'); e.preventDefault(); });
    addEventListener('mousemove', e => {
      if (!dragging) return;
      setSidebarWidth(document.body.classList.contains('side-right') ? window.innerWidth - e.clientX : e.clientX);
    });
    addEventListener('mouseup', () => {
      if (!dragging) return;
      dragging = false;
      rz.classList.remove('drag');
      setSidebarWidth(parseFloat($('#side').style.width), true);
      layout();
      render();
    });
  })();
}
