#!/usr/bin/env node
// Vendors Material Icon Theme (MIT) into web/static/icons: the file and folder
// SVGs plus a compact map.json that web/src/icons.js resolves names against.
//
//   npm pack material-icon-theme && tar -xzf material-icon-theme-*.tgz
//   node scripts/vendor-icons.js ./package
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const pkg = process.argv[2];
if (!pkg) {
  console.error('usage: node scripts/vendor-icons.js <material-icon-theme package dir>');
  process.exit(2);
}
const manifest = JSON.parse(fs.readFileSync(path.join(pkg, 'dist', 'material-icons.json'), 'utf8'));
const version = JSON.parse(fs.readFileSync(path.join(pkg, 'package.json'), 'utf8')).version;
const out = path.join(__dirname, '..', 'web', 'static', 'icons');

const used = new Set([manifest.file, manifest.folder, manifest.folderExpanded]);
const take = obj => {
  for (const id of Object.values(obj || {})) used.add(id);
  return obj || {};
};
const map = {
  version,
  file: manifest.file,
  folder: manifest.folder,
  folderOpen: manifest.folderExpanded,
  ext: take(manifest.fileExtensions),
  names: take(manifest.fileNames),
  // Every expanded folder icon is the closed one plus "-open".
  folders: take(manifest.folderNames),
  light: {
    ext: take(manifest.light?.fileExtensions),
    names: take(manifest.light?.fileNames),
  },
};
for (const id of Object.values(map.folders)) used.add(id + '-open');

fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(out, { recursive: true });
let bytes = 0;
for (const id of used) {
  const def = manifest.iconDefinitions[id];
  if (!def) continue;
  const src = path.join(pkg, 'dist', def.iconPath);
  const svg = fs.readFileSync(src);
  bytes += svg.length;
  fs.writeFileSync(path.join(out, id + '.svg'), svg);
}
fs.writeFileSync(path.join(out, 'map.json'), JSON.stringify(map));
fs.copyFileSync(path.join(pkg, 'LICENSE'), path.join(out, 'LICENSE'));
console.log(`vendored ${used.size} icons (${Math.round(bytes / 1024)} KB) from material-icon-theme ${version}`);
