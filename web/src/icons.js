// web/src/icons.js
// File and folder icons from Material Icon Theme (MIT, web/static/icons/LICENSE),
// vendored by scripts/vendor-icons.js. Names resolve the way VS Code does: an
// exact file name first, then the longest dotted extension ("spec.ts" before
// "ts"). Until the map loads, or with workbench.iconTheme "minimal", rows keep
// px0's colour dots.
import { on } from './bus.js';

const BASE = 'static/static/icons/';
let map = null;
let light = false;

const checkScheme = () => {
  light = getComputedStyle(document.documentElement).colorScheme.includes('light');
};

export async function loadIcons() {
  try {
    const res = await fetch(new URL(BASE + 'map.json', document.baseURI));
    if (res.ok) map = await res.json();
  } catch {}
  checkScheme();
  on('theme:changed', checkScheme);
}

const url = id => BASE + id + '.svg';

export function fileIconURL(name) {
  if (!map) return '';
  const n = name.toLowerCase();
  const L = light ? map.light : null;
  let id = L?.names[n] || map.names[n];
  for (let i = n.indexOf('.'); !id && i >= 0; i = n.indexOf('.', i + 1)) {
    const ext = n.slice(i + 1);
    id = L?.ext[ext] || map.ext[ext];
  }
  return url(id || map.file);
}

export function folderIconURLs(name) {
  if (!map) return null;
  const id = map.folders[name.toLowerCase()];
  return id ? [url(id), url(id + '-open')] : [url(map.folder), url(map.folderOpen)];
}

export const fileIconHTML = name => {
  const src = fileIconURL(name);
  return src ? '<img class="fi" src="' + src + '" alt="" draggable="false">' : '';
};

export const folderIconHTML = name => {
  const srcs = folderIconURLs(name);
  return srcs
    ? '<img class="fi c" src="' + srcs[0] + '" alt="" draggable="false"><img class="fi o" src="' + srcs[1] + '" alt="" draggable="false" loading="lazy">'
    : '';
};
