import { S, doc_, CHUNK, LH, OVERSCAN } from './state.js';
import { vp } from './ui.js';

const MAX_BYTES = 32 * 1024 * 1024;
const MAX_CHUNKS = 12;
let clock = 0;

export function touchChunk(d, c) {
  const entry = d.chunkCache?.get(c);
  if (entry) entry.used = ++clock;
}

export function storeChunk(d, j) {
  d.chunkCache ||= new Map();
  const c = Math.floor(j.start / CHUNK);
  for (let i = 0; i < j.lines.length; i++) d.lines[j.start + i] = j.lines[i];
  d.chunks.add(c);
  d.chunkCache.set(c, { start: j.start, count: j.lines.length,
    bytes: j.lines.reduce((n, line) => n + line.length * 2, 0), used: ++clock });
  const active = doc_();
  const first = Math.max(0, Math.floor(vp.scrollTop / LH) - OVERSCAN);
  const last = first + Math.ceil(vp.clientHeight / LH) + OVERSCAN * 2;
  const candidates = [];
  let bytes = 0;
  for (const tab of S.tabs) {
    for (const [chunk, entry] of tab.chunkCache || []) {
      bytes += entry.bytes;
      if (tab === active && entry.start < last && entry.start + entry.count > first) continue;
      candidates.push({ tab, chunk, entry });
    }
  }
  candidates.sort((a, b) => a.entry.used - b.entry.used);
  for (const { tab, chunk, entry } of candidates) {
    if (bytes <= MAX_BYTES && tab.chunkCache.size <= MAX_CHUNKS) continue;
    for (let i = 0; i < entry.count; i++) delete tab.lines[entry.start + i];
    tab.chunks.delete(chunk);
    tab.chunkCache.delete(chunk);
    bytes -= entry.bytes;
  }
}
