// What the touches add up to: where each agent is, how hot each file is, and who is on the same file. No Herdr imports, so
// `node --test` can load this file. Nothing here reads a clock: `now` comes in, in ms.
//
// A touch is `{ at, agent, tool, path, kind }`, with `kind` "read", "write" or "scan" and `path` from the top of the repo,
// or null for somewhere outside it.

export const COLLISION_MS = 2 * 60000; // two agents on one file this close together, with a write among them
export const HEAT_MS = 5 * 60000; // a write fades out over this long
export const TRAIL_MS = 2 * 60000; // a read's trail stays this long
export const RECENT_MS = 15 * 60000; // what a block remembers
export const ACTIVE_MS = 10 * 60000; // an agent quiet for longer goes back to the dock
export const FADE_BEAT_MS = 15000; // fades only change on this beat, so they cost one redraw a beat at most
export const HEAT_STEPS = 10;

const MAX_TOUCHES = 2000;
const TRAIL_POINTS = 6;
const WRITE_LIFT = 0.15;
const WRITE_BASE = 0.7;

// The same call turns up again each time a read overlaps the last one, so a call counts once. Oldest first.
export function addTouches(list, fresh, now) {
  const seen = new Set(list.map(touchKey)), out = list.filter((touch) => now - touch.at < RECENT_MS);
  for (const touch of fresh) {
    const key = touchKey(touch);
    if (seen.has(key) || now - touch.at >= RECENT_MS) continue;
    seen.add(key);
    out.push(touch);
  }
  out.sort((a, b) => a.at - b.at);
  return out.slice(-MAX_TOUCHES);
}

const touchKey = (t) => `${t.at}|${t.agent}|${t.tool}|${t.path}`;

// Whichever beat `now` falls in. Everything that fades uses this rather than `now`.
export const beat = (now) => Math.floor(now / FADE_BEAT_MS) * FADE_BEAT_MS;

// How lit a file is, from 0 to 1. One write starts at 0.7, more writes lift it, and it runs down to nothing in HEAT_MS.
export function heatLevel(writes, now) {
  if (!writes.length) return 0;
  const last = Math.max(...writes), fresh = writes.filter((at) => now - at < HEAT_MS).length;
  const left = 1 - (now - last) / HEAT_MS;
  if (left <= 0) return 0;
  return Math.min(1, WRITE_BASE + WRITE_LIFT * (fresh - 1)) * Math.min(1, left);
}

// Heat in whole steps, so a small change in the clock doesn't count as a change to the picture.
export const heatStep = (level) => Math.ceil(level * HEAT_STEPS) / HEAT_STEPS;

// path -> { level, agent }, where `agent` wrote it last.
export function heatByFile(touches, now) {
  const writes = new Map();
  for (const t of touches) {
    if (t.kind !== "write" || !t.path || now - t.at >= HEAT_MS) continue;
    if (!writes.has(t.path)) writes.set(t.path, { times: [], agent: t.agent });
    const entry = writes.get(t.path);
    entry.times.push(t.at);
    entry.agent = t.agent;
  }
  const out = new Map();
  for (const [path, { times, agent }] of writes) {
    const level = heatStep(heatLevel(times, now));
    if (level > 0) out.set(path, { level, agent });
  }
  return out;
}

// One agent's own subagent works for it and doesn't run beside it, so those two never count as a collision.
const sameTeam = (a, b, ownerOf) => ownerOf(a) === b || ownerOf(b) === a;

// Files that two agents have in hand: both read or wrote it inside COLLISION_MS, and at least one of those was a write.
// A scan isn't holding a file. Newest first, as [{ path, at, agents: [{ agent, kind, at }] }], writers before readers.
export function collisions(touches, now, ownerOf = () => null) {
  const files = new Map();
  for (const t of touches) {
    if (!t.path || t.kind === "scan" || now - t.at >= COLLISION_MS) continue;
    if (!files.has(t.path)) files.set(t.path, new Map());
    const held = files.get(t.path), had = held.get(t.agent);
    held.set(t.agent, { agent: t.agent, kind: had?.kind === "write" ? "write" : t.kind, at: Math.max(had?.at ?? 0, t.at) });
  }
  const out = [];
  for (const [path, held] of files) {
    const list = [...held.values()], involved = new Set();
    for (const a of list) {
      for (const b of list) {
        if (a === b || sameTeam(a.agent, b.agent, ownerOf) || (a.kind !== "write" && b.kind !== "write")) continue;
        involved.add(a); involved.add(b);
      }
    }
    if (involved.size < 2) continue;
    const agents = [...involved].sort((a, b) => (b.kind === "write") - (a.kind === "write") || b.at - a.at);
    out.push({ path, at: Math.max(...agents.map((a) => a.at)), agents });
  }
  return out.sort((a, b) => b.at - a.at || (a.path < b.path ? -1 : 1));
}

// The line under the map's name. It has no clock in it, so it costs no redraws while it shows.
export function collisionLine(list, nameOf) {
  if (!list.length) return null;
  const [first] = list, doing = (a) => `${nameOf(a.agent)} ${a.kind === "write" ? "writing" : "reading"}`;
  const who = first.agents.map(doing);
  const names = who.length > 1 ? `${who.slice(0, -1).join(", ")} and ${who.at(-1)}` : who[0];
  return `${first.path}: ${names}${list.length > 1 ? ` (+${list.length - 1} more)` : ""}`;
}

// The latest touch by this agent, if it isn't too old to stand for where the agent is.
export function lastTouch(touches, agent, now) {
  for (let i = touches.length - 1; i >= 0; i -= 1) {
    const t = touches[i];
    if (t.agent === agent) return now - t.at < ACTIVE_MS ? t : null;
  }
  return null;
}

// The places an agent has read or scanned lately, oldest first, without the same place twice in a row. Writes leave heat
// instead, so they're not here.
export function trailOf(touches, agent, now) {
  const out = [];
  for (const t of touches) {
    if (t.agent !== agent || !t.path || now - t.at >= TRAIL_MS || t.kind === "write") continue;
    if (out.at(-1)?.path !== t.path) out.push({ path: t.path, at: t.at });
    else out.at(-1).at = t.at;
  }
  return out.slice(-TRAIL_POINTS);
}

// Who has been at `belongs`'s paths lately, newest first: one row an agent, with what it did last and how many times.
export function visitors(touches, belongs, now) {
  const rows = new Map();
  for (const t of touches) {
    if (!t.path || t.kind === "scan" || now - t.at >= RECENT_MS || !belongs(t.path)) continue;
    const row = rows.get(t.agent) ?? { agent: t.agent, at: 0, kind: t.kind, reads: 0, writes: 0 };
    row[t.kind === "write" ? "writes" : "reads"] += 1;
    if (t.at >= row.at) { row.at = t.at; row.kind = t.kind; }
    rows.set(t.agent, row);
  }
  return [...rows.values()].sort((a, b) => b.at - a.at);
}

export function ago(ms) {
  const minutes = Math.floor(ms / 60000);
  return minutes < 1 ? "just now" : `${minutes} min ago`;
}
