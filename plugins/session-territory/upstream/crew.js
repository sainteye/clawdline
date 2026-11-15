// Works out which folder to map and who to put on it. No Herdr imports, so `node --test` can load this file.
import { repoPath, touchesFrom } from "./calls.js";

export const READABLE_APP = "claude-code";
export const MAX_DOTS = 12;
export const SUB_QUIET_MS = 90000; // a subagent's log quiet for this long means it has stopped, whatever its meta file says

const MAX_DEPTH = 6;
const NAME_LIMIT = 22;
const LEADING_MARKS = /^[^\p{L}\p{N}]+/u;

const local = (address) => typeof address === "string" && !address.includes("/");
const cut = (text) => (text.length > NAME_LIMIT ? `${text.slice(0, NAME_LIMIT - 3).trimEnd()}...` : text);
const plain = (text) => (typeof text === "string" ? text.replace(LEADING_MARKS, "").trim() : "");

// Where the focused space is now: its focused pane's folder, else its first terminal's, else the folder it was made in.
// A space on another machine can't be read from here. `key` changes when the answer does.
export function placeOf({ focused, spaces, panes, home }) {
  const space = spaces.find((s) => s.id === focused?.space) ?? spaces.find((s) => s.active);
  if (!space) return { state: "nofocus", key: "nofocus", space: null, folder: null };
  if (space.machine) return { state: "remote", key: `remote:${space.machine}`, space: space.id, machine: space.machine, folder: null };
  const tabs = new Set(space.tabs.map((t) => t.id));
  const here = panes.filter((p) => tabs.has(p.tab) && p.cwd);
  const folder = (focused?.space === space.id && focused?.cwd) || here.find((p) => p.focused)?.cwd || here[0]?.cwd || space.folder || home;
  return { state: "ok", key: `ok:${folder}`, space: space.id, folder };
}

const wordOf = (status) => (typeof status === "string" ? status : status?.state ?? null);

// The name Herdr shows for it: the terminal's, a named tab's, or its space's, as the Agents list has it. `null` when Herdr
// has none, and then the session's name or the title stands in.
function herdrName(t, info) {
  const tab = info?.tab_name && !/^\d+$/.test(info.tab_name) ? info.tab_name : "";
  return t.name || tab || info?.space_name || null;
}

const nameFor = (t, info) => cut(herdrName(t, info) || plain(info?.title) || info?.program?.name || t.address);

// The agents on this repo: every agent in the focused space, every agent in another space whose folder is in the repo, and
// whatever those started. `list` is `hc ls --all --json`, `agents` is the live list. Shells and finished terminals stay off.
export function crewOf({ list, agents, space, root }) {
  const info = new Map(agents.map((a) => [a.address, a])), kids = new Map();
  for (const t of list) {
    if (!t.parent) continue;
    if (!kids.has(t.parent)) kids.set(t.parent, []);
    kids.get(t.parent).push(t);
  }
  const agentLike = (t) => t.state === "live" && local(t.address) && (t.agent === READABLE_APP || info.has(t.address));
  const inScope = (t) => (space && info.get(t.address)?.space === space) || (root && repoPath(t.cwd, root) !== null);
  const found = new Map();
  const add = (t, depth) => {
    if (found.has(t.address) || depth > MAX_DEPTH) return;
    if (agentLike(t)) {
      const about = info.get(t.address);
      found.set(t.address, {
        key: t.address, address: t.address, pane: about?.pane ?? null, name: nameFor(t, about), named: Boolean(herdrName(t, about)), status: about?.status ?? wordOf(t.status) ?? "idle",
        app: t.agent === READABLE_APP || about?.app === READABLE_APP ? READABLE_APP : about?.app ?? t.agent ?? null, owner: null, sub: false, readable: false,
      });
    }
    for (const kid of kids.get(t.address) ?? []) add(kid, depth + 1);
  };
  for (const t of list) if (t.state === "live" && inScope(t)) add(t, 0);
  return [...found.values()];
}

const uniqueNames = (crew) => {
  const seen = new Map();
  for (const c of crew) seen.set(c.name, (seen.get(c.name) ?? 0) + 1);
  return crew.map((c) => (seen.get(c.name) > 1 && c.address ? { ...c, name: cut(`${c.name.slice(0, NAME_LIMIT - 5)} ${c.address}`) } : c));
};

const rank = { blocked: 0, working: 1, done: 2, idle: 3 };
const byUrgency = (a, b) => (rank[a.status] ?? 3) - (rank[b.status] ?? 3);

// Adds what territory.py printed to the crew: who it could read, their subagents as agents of their own, and every new call
// as touches. `sessions` is keyed by terminal address. `known` also has subagents that have stopped, whose calls still count.
export function withTranscripts({ crew, sessions, root, now }) {
  const out = [], touches = [], ended = [];
  for (const c of crew) {
    const session = sessions?.[c.key];
    out.push({ ...c, name: !c.named && session?.name ? cut(session.name) : c.name, readable: Boolean(session) });
    if (!session) continue;
    touches.push(...touchesFrom(c.key, session.events, root));
    for (const a of session.agents ?? []) {
      const key = `sub:${a.id}`;
      touches.push(...touchesFrom(key, a.events, root));
      const sub = { key, address: null, pane: null, name: cut(a.task || "Subagent"), status: "working", app: READABLE_APP, owner: c.key, sub: true, readable: true };
      if (a.finished === true || now - a.last >= SUB_QUIET_MS) ended.push(sub);
      else out.push(sub);
    }
  }
  const named = uniqueNames(out).sort(byUrgency);
  return { crew: named, touches, known: [...named, ...ended] };
}

// Turns an agent's status as the live list has it now, between reads.
export function withStatus(crew, agents) {
  const live = new Map(agents.map((a) => [a.address, a.status]));
  return crew.map((c) => (live.has(c.address) && live.get(c.address) !== c.status ? { ...c, status: live.get(c.address) } : c));
}

// Each agent keeps the colour it first got. A new one takes the first slot nobody on the crew holds.
export function assignSlots(slots, crew, size) {
  const taken = new Set(crew.filter((c) => slots.has(c.key)).map((c) => slots.get(c.key)));
  let next = 0;
  for (const c of crew) {
    if (slots.has(c.key)) continue;
    while (taken.has(next % size) && taken.size < size) next += 1;
    slots.set(c.key, next % size);
    taken.add(next % size);
    next += 1;
  }
  return slots;
}
