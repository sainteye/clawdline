// Works out one picture of the map: what's hot, who is where, what's colliding. No Herdr imports, so `node --test` can load
// this file. view.js only draws what this says, and `sig` lets main.js skip the redraw when nothing here has changed.
import { ago, beat, collisionLine, collisions, heatByFile, lastTouch, trailOf, visitors } from "./activity.js";
import { MAX_DOTS } from "./crew.js";

// 48211 as 48,211. Herdr's script engine has no locale data, so toLocaleString leaves the commas out.
export const thousands = (n) => String(Math.round(n)).replace(/\B(?=(\d{3})+(?!\d))/g, ",");

export const HEADER_H = 44;
export const DOCK_H = 28;
export const MIN_MAP_H = 100; // shorter than this, only the header shows
export const HIT = 32;
export const CARD_W = 280;

const DOCK_SLOT = 158;
const LABEL_W = 132;
const LABEL_STEP = 15;
const LABEL_H = 16;
const FAN = 18;
const EDGE = 12; // the largest core's radius and its ring, so a dot on an edge block stays whole
const CARD_HEAD = 58;
const CARD_ROW = 28;
const MAX_ROWS = 6;
const clamp = (value, least, most) => Math.max(least, Math.min(most, value));

export const zoneOf = (height) => Math.max(0, height - HEADER_H - DOCK_H);
export const hasMap = (height) => height - HEADER_H >= MIN_MAP_H;
const middle = (r) => ({ x: r.x + r.w / 2, y: r.y + r.h / 2 });

export function sizeText(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

const dockWord = (agent, last) => {
  if (!agent.readable) return "no transcript";
  return last && !last.path ? "outside the repo" : "no file yet";
};

// Dots on one block fan out round its middle from the left, so two agents on a file sit side by side. Their names stack, under the dots, or
// over them near the foot of the map, and stay inside the map's sides.
function fanOut(rect, agents, width, zone) {
  const at = middle(rect), radius = agents.length > 1 ? Math.min(FAN, Math.max(9, Math.min(rect.w, rect.h) / 3)) : 0;
  return agents.map((agent, i) => {
    const angle = Math.PI + (2 * Math.PI * i) / agents.length;
    const x = clamp(at.x + Math.cos(angle) * radius, EDGE, width - EDGE), y = clamp(at.y + Math.sin(angle) * radius, EDGE, zone - EDGE);
    const below = y + HIT / 2 + LABEL_H * (i + 1) < zone;
    const labelTop = below ? HIT - 4 + i * LABEL_STEP : -LABEL_H + 4 - i * LABEL_STEP;
    const labelLeft = clamp(x - LABEL_W / 2, 0, Math.max(0, width - LABEL_W)) - (x - HIT / 2);
    return { ...agent, x, y, where: "map", labelLeft, labelTop, labelWidth: LABEL_W };
  });
}

export function placeDots({ map, crew, touches, now, width, zone }) {
  const shown = crew.slice(0, MAX_DOTS), places = new Map(), docked = [];
  for (const agent of shown) {
    const last = lastTouch(touches, agent.key, now), rect = last?.path ? map?.locate(last.path) : null;
    if (!rect) { docked.push({ ...agent, word: dockWord(agent, last) }); continue; }
    if (!places.has(rect)) places.set(rect, []);
    places.get(rect).push({ ...agent, word: null });
  }
  const dots = [...places].flatMap(([rect, agents]) => fanOut(rect, agents, width, zone));
  const room = Math.max(1, Math.floor((width * 0.62) / DOCK_SLOT));
  docked.slice(0, room).forEach((agent, i) => {
    dots.push({ ...agent, x: 14 + i * DOCK_SLOT, y: zone + DOCK_H / 2, where: "dock", labelLeft: HIT - 8, labelTop: 8, labelWidth: DOCK_SLOT - HIT });
  });
  return { dots, more: Math.max(0, docked.length - room) + crew.length - shown.length };
}

function trailsOf({ map, crew, touches, now, dots }) {
  const out = [], seen = new Map();
  if (!map) return { trails: out, seen };
  for (const agent of crew) {
    const dot = dots.find((d) => d.key === agent.key);
    if (!dot) continue;
    const spots = trailOf(touches, agent.key, now).map((step) => map.locate(step.path)).filter(Boolean);
    for (const spot of spots) if (spot.key) seen.set(spot.key, agent.key);
    const points = spots.map(middle).filter((p, i, all) => !i || p.x !== all[i - 1].x || p.y !== all[i - 1].y);
    if (dot.where === "map") points.push({ x: dot.x, y: dot.y });
    if (points.length > 1) out.push({ agent: agent.key, points });
  }
  return { trails: out, seen };
}

// What a press on a block shows, and where it goes: beside the block if there's room, else the other side.
function cardFor({ picked, map, touches, nameOf, now, width, zone, repoName }) {
  const tile = map?.tiles.find((b) => b.key === picked);
  if (!tile) return null;
  const rows = visitors(touches, (path) => map.locate(path) === tile, now).slice(0, MAX_ROWS)
    .map((row) => ({ ...row, name: nameOf(row.agent), when: `${row.kind === "write" ? "wrote" : "read"} ${ago(now - row.at)}` }));
  const height = CARD_HEAD + Math.max(1, rows.length) * CARD_ROW;
  const right = tile.x + tile.w + 8, left = tile.x - CARD_W - 8;
  const x = right + CARD_W <= width ? right : Math.max(4, left >= 0 ? left : width - CARD_W - 4);
  const note = tile.kind === "file" ? sizeText(tile.bytes) : `${tile.count} file${tile.count === 1 ? "" : "s"}, ${sizeText(tile.bytes)}`;
  return { tile, title: tile.path || repoName, note, rows, x, y: clamp(tile.y, 4, Math.max(4, zone - height - 4)), height };
}

// Heat, trails and a card's ages run on the beat, so only an agent moving or a collision starting or ending can change the
// picture between beats. `nameOf` and `ownerOf` also know agents that have gone.
export function buildFrame({ map, crew, touches, now, width, height, picked = null, repoName = "", nameOf = (key) => key, ownerOf = () => null }) {
  const zone = zoneOf(height), fade = beat(now);
  const hits = collisions(touches, now, ownerOf);
  const heat = new Map(), clash = new Map();
  if (map) {
    for (const [path, entry] of heatByFile(touches, fade)) {
      const tile = map.locate(path);
      if (tile?.key && entry.level > (heat.get(tile.key)?.level ?? 0)) heat.set(tile.key, entry);
    }
    for (const hit of hits) {
      const tile = map.locate(hit.path);
      if (tile?.key && !clash.has(tile.key)) clash.set(tile.key, hit);
    }
  }
  const { dots, more } = placeDots({ map, crew, touches, now, width, zone });
  const { trails, seen } = trailsOf({ map, crew, touches, now: fade, dots });
  const card = cardFor({ picked, map, touches, nameOf, now: fade, width, zone, repoName });
  const line = collisionLine(hits, nameOf);
  const sig = JSON.stringify([
    [...heat].map(([key, e]) => [key, e.level, e.agent]), [...clash.keys()], line, more,
    dots.map((d) => [d.key, d.name, d.status, d.where, d.word, Math.round(d.x), Math.round(d.y)]),
    trails.map((tr) => [tr.agent, tr.points.length, tr.points[0]]), [...seen],
    card && [card.tile.key, card.rows.map((r) => [r.agent, r.when])],
  ]);
  return { map, zone, heat, clash, seen, trails, dots, more, line, firstClash: hits[0] ? map?.locate(hits[0].path)?.key ?? null : null, card, sig };
}
