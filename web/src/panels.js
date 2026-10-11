// web/src/panels.js
import { $, $$, S, api, persistPreference } from './state.js';
import { layout, render } from './renderer.js';
import { updateStatus } from './status.js';
import { loadOutline } from './outline.js';
import { treeEl, openDirs, drawTree } from './tree.js';
import { reloadOpenTabs } from './tabs.js';
import { showToast } from './ui.js';

export function showPanel(name) {
  setSidebarVisible(true);
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

export function setSidebarWidth(width, persist = false) {
  const schema = S.settingsSchema?.find(item => item.key === 'workbench.sideBar.width');
  const next = Math.max(schema?.min ?? 170, Math.min(schema?.max ?? 620, Math.round(width)));
  $('#side').style.width = next + 'px';
  if (persist) persistPreference('workbench.sideBar.width', next).catch(() => {});
}

export function initPanels() {
  $('#btn-reindex').addEventListener('click', async () => {
    const j = await api('/api/reindex');
    S.meta.files = j.files; S.meta.indexMs = j.indexMs;
    treeEl.innerHTML = ''; openDirs.clear();
    await drawTree('', treeEl, 0);
    // Reindex is a refresh: re-fetch open tabs quietly in place without tab switching.
    await reloadOpenTabs();
    updateStatus();
    showToast('✓', 'Workspace reindexed');
  });

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
