// Lays a repo out as a treemap: folders as blocks, files inside them. No Herdr imports, so `node --test` can load this file.
//
// A file's area follows the cube root of its bytes, so one lockfile can't take half the map. Whatever would come out too
// small to press goes into one block per folder that says how many files it holds, and a folder too small to open up is
// a single block of its own. `maxTiles` bounds what a render has to draw, however big the repo is.

export const MAX_TILES = 140;

const FLOOR_BYTES = 256;
const MIN_AREA = 22 * 20;
const FRAME_W = 64;
const FRAME_H = 46;
const FRAME_SHARE = 4; // a folder opens up when it has room for at least this many of the smallest blocks
const HEAD = 16; // a folder's name
const GAP = 2;
const MAX_PASSES = 8;

export const weightOf = (bytes) => Math.cbrt(Math.max(bytes, FLOOR_BYTES));

const sortKids = (a, b) => b.weight - a.weight || (a.name < b.name ? -1 : 1);

function folder(name, path) {
  return { name, path, kids: new Map(), list: [], rest: null, weight: 0, count: 0, bytes: 0 };
}

function folderAt(root, parts) {
  let at = root;
  for (const part of parts) {
    if (!at.kids.has(part)) at.kids.set(part, folder(part, at.path ? `${at.path}/${part}` : part));
    at = at.kids.get(part);
  }
  return at;
}

function total(node) {
  node.list = [...node.kids.values()];
  node.weight = 0; node.count = 0; node.bytes = 0;
  for (const kid of node.list) {
    if (kid.kids) total(kid);
    node.weight += kid.weight; node.count += kid.count; node.bytes += kid.bytes;
  }
  if (node.rest) {
    node.weight += node.rest.weight; node.count += node.rest.count; node.bytes += node.rest.bytes;
  }
  node.list.sort(sortKids);
}

// `src/main/java/com/acme` with nothing else on the way reads as one folder.
function squeeze(node) {
  for (const kid of node.list) if (kid.kids) squeeze(kid);
  if (!node.path) return;
  while (node.list.length === 1 && node.list[0].kids && !node.rest) {
    const only = node.list[0];
    node.name = `${node.name}/${only.name}`;
    node.path = only.path; node.list = only.list; node.rest = only.rest;
  }
}

// `files` is [[path, bytes], ...]. `rest` is [[folder, count, bytes], ...]: what the helper left out of a very big repo.
export function buildTree(files, rest = []) {
  const root = folder("", "");
  for (const [path, bytes] of files) {
    const parts = path.split("/"), name = parts.pop();
    folderAt(root, parts).kids.set(name, { name, path, bytes, count: 1, weight: weightOf(bytes) });
  }
  for (const [dir, count, bytes] of rest) {
    if (count <= 0) continue;
    const at = folderAt(root, dir ? dir.split("/") : []);
    at.rest = { count, bytes, weight: count * weightOf(bytes / count) };
  }
  total(root);
  squeeze(root);
  return root;
}

const worst = (sum, side, least, most) => Math.max((side * side * most) / (sum * sum), (sum * sum) / (side * side * least));

// Squarified treemap: rows of near-square rectangles. `items` carry `weight`, biggest first. Gives [{ item, x, y, w, h }].
export function squarify(items, rect) {
  const out = [], all = items.reduce((sum, i) => sum + i.weight, 0);
  let { x, y, w, h } = rect;
  if (!all || w <= 0 || h <= 0) return out;
  const scale = (w * h) / all;
  let i = 0;
  while (i < items.length) {
    const side = Math.min(w, h), row = [];
    let sum = 0, best = Infinity, least = Infinity, most = 0;
    for (; i < items.length; i += 1) {
      const area = items[i].weight * scale, nextLeast = Math.min(least, area), nextMost = Math.max(most, area);
      const ratio = worst(sum + area, side, nextLeast, nextMost);
      if (row.length && ratio > best) break;
      row.push(items[i]); sum += area; best = ratio; least = nextLeast; most = nextMost;
    }
    const thick = sum / side;
    let along = 0;
    for (const item of row) {
      const length = (item.weight * scale) / thick;
      if (w >= h) out.push({ item, x, y: y + along, w: thick, h: length });
      else out.push({ item, x: x + along, y, w: length, h: thick });
      along += length;
    }
    if (w >= h) { x += thick; w -= thick; } else { y += thick; h -= thick; }
  }
  return out;
}

// Whole pixels, with the gap taken off the far edges so neighbours meet at one gap.
function snap({ x, y, w, h }) {
  const left = Math.round(x), top = Math.round(y);
  return { x: left, y: top, w: Math.max(1, Math.round(x + w) - left - GAP), h: Math.max(1, Math.round(y + h) - top - GAP) };
}

function place(node, rect, out, minArea) {
  const inner = node.path ? { x: rect.x + 1, y: rect.y + HEAD, w: rect.w - 2, h: rect.h - HEAD - 1 } : rect;
  const scale = (inner.w * inner.h) / node.weight, kept = [];
  let looseWeight = node.rest?.weight ?? 0, looseCount = node.rest?.count ?? 0, looseBytes = node.rest?.bytes ?? 0;
  for (const kid of node.list) {
    if (kid.weight * scale >= minArea) kept.push(kid);
    else { looseWeight += kid.weight; looseCount += kid.count; looseBytes += kid.bytes; }
  }
  const items = [...kept];
  if (looseCount) items.push({ loose: true, weight: looseWeight, count: looseCount, bytes: looseBytes });
  for (const spot of squarify(items, inner)) {
    const { item } = spot;
    if (item.loose) {
      out.tiles.push({ kind: "group", path: node.path, label: `${item.count} small file${item.count === 1 ? "" : "s"}`, count: item.count, bytes: item.bytes, ...snap(spot) });
    } else if (!item.kids) {
      out.tiles.push({ kind: "file", path: item.path, label: item.name, count: 1, bytes: item.bytes, ...snap(spot) });
    } else if (spot.w >= FRAME_W && spot.h >= FRAME_H && spot.w * spot.h >= minArea * FRAME_SHARE) {
      out.folders.push({ path: item.path, label: item.name, count: item.count, ...snap(spot) });
      place(item, spot, out, minArea);
    } else {
      out.tiles.push({ kind: "group", path: item.path, label: `${item.name}/`, count: item.count, bytes: item.bytes, ...snap(spot) });
    }
  }
}

// `{ tiles, folders, count, grouped, locate(path) }`. Tiles are in drawing order and each has a `key`. A path that has no
// block of its own lands on the nearest block or folder above it, which is how a new file shows before the next list.
export function layoutMap({ files, rest = [], width, height, maxTiles = MAX_TILES }) {
  const empty = { tiles: [], folders: [], width, height, count: 0, grouped: 0, locate: () => null };
  if (width < 1 || height < 1) return empty;
  const root = buildTree(files, rest);
  if (!root.count) return empty;
  const rect = { x: 0, y: 0, w: width, h: height };
  let minArea = Math.max(MIN_AREA, (width * height) / (maxTiles * 3)), out;
  for (let pass = 0; pass < MAX_PASSES; pass += 1) {
    out = { tiles: [], folders: [] };
    place(root, rect, out, minArea);
    if (out.tiles.length <= maxTiles) break;
    minArea *= 1.5;
  }
  if (out.tiles.length > maxTiles) out = { tiles: [{ kind: "group", path: "", label: `${root.count} files`, count: root.count, bytes: root.bytes, ...snap(rect) }], folders: [] };
  out.tiles.forEach((tile, i) => { tile.key = `b${i}`; });
  const exact = new Map(), groups = new Map(), frames = new Map(out.folders.map((f) => [f.path, f]));
  for (const tile of out.tiles) (tile.kind === "file" ? exact : groups).set(tile.path, tile);
  const locate = (path) => {
    if (exact.has(path)) return exact.get(path);
    let at = path;
    for (;;) {
      if (groups.has(at)) return groups.get(at);
      if (frames.has(at)) return frames.get(at);
      if (!at) return null;
      const cut = at.lastIndexOf("/");
      at = cut < 0 ? "" : at.slice(0, cut);
    }
  };
  const grouped = out.tiles.reduce((sum, tile) => sum + (tile.kind === "group" ? tile.count : 0), 0);
  return { tiles: out.tiles, folders: out.folders, width, height, count: root.count, grouped, locate };
}
