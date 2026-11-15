// The board's rules. No drawing and no app calls, so they can be tested with `node --test`.

// One drum of letters, in the order a flap falls through them. It only ever runs forward.
export const DRUM = " ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789:-./'&+#?";

export const FLIP_TICK_MS = 50; // one half flap
export const MAX_FLIPS = 5; // letters a cell shows on its way, the new one included
export const COLS_PER_TICK = 2; // the wave crosses two cells a tick, left to right
export const ROW_LAG = 1; // ticks between one changing row and the next
export const PAGE_MS = 8000;
export const HEARTBEAT_MS = 10000; // the clock and clearing done rows
export const DONE_CLEAR_MS = 300000;
export const CLACK_GAP_MS = 1200;
export const FOLD_ROWS = 2; // a folded board keeps the top flight and its reason
const FOLD_SLACK = 3;

export const WIDTHS = { time: 5, flight: 6, gate: 8, remarks: 10 };
const GAP_CELLS = 2; // the four gaps between columns, each half a cell
const FIXED_CELLS = WIDTHS.time + WIDTHS.flight + WIDTHS.gate + WIDTHS.remarks + GAP_CELLS;
const DEST = { least: 6, usual: 16, most: 32 };
const PITCH = { least: 12, most: 38 };
export const SIDE = 16; // the board's left and right margin

const clamp = (n, low, high) => Math.min(high, Math.max(low, n));

// Sizes for a board of this size, in the app's units at this zoom. The letter count stays the
// same as a window grows, so the type grows instead, which is what keeps it readable as a
// thumbnail. Past the largest cell the extra width goes to DESTINATION.
//
// The grid is worked out in the window's own pixels and handed back in the app's units. The app
// rounds each element to whole pixels, so at a zoom of 1.2 a 22 unit cell drew 26 pixels, not
// 26.4, and anything placed by adding widths up (the sheen, the column labels, the right margin)
// drifted further from the cells with every column. Whole pixels add up exactly.
export function fitBoard(width, height, zoom = 1) {
  const z = zoom > 0 ? zoom : 1;
  const unit = (px) => (px + 0.001) / z; // a hair over, so the app's rounding lands on the pixel
  const usable = Math.max(Math.floor((width - 2 * SIDE) * z), 0);
  const pitchPx = clamp(Math.floor(usable / (FIXED_CELLS + DEST.usual)), Math.round(PITCH.least * z), Math.round(PITCH.most * z));
  const dest = clamp(Math.floor(usable / pitchPx) - FIXED_CELLS, DEST.least, DEST.most);
  const cellGapPx = Math.max(1, Math.round(2 * z));
  const rowGapPx = Math.max(1, Math.round(3 * z));
  const cellHPx = Math.round((pitchPx - cellGapPx) * 1.4);
  const pitch = unit(pitchPx);
  const cell = unit(pitchPx - cellGapPx);
  const cellH = unit(cellHPx);
  const rowPitch = unit(cellHPx + rowGapPx);
  const head = Math.max(36, Math.round(cellH * 1.1));
  const label = Math.max(10, Math.round(cell * 0.42));
  const top = head + label + 8;
  return {
    pitch, cell, cellH, rowPitch, head, label, top, dest,
    half: unit(Math.floor(cellHPx / 2)),
    cellGap: unit(cellGapPx),
    rowGap: unit(rowGapPx),
    gap: unit(Math.floor(pitchPx / 2)),
    font: Math.round(cell * 1.08),
    title: Math.round(cellH * 0.72),
    rows: Math.max(1, Math.floor((height - top - 8) / rowPitch)),
    cells: WIDTHS.time + WIDTHS.flight + dest + WIDTHS.gate + WIDTHS.remarks,
    // The block can come back a pixel short of what was asked (the divider under it), which lost the second row.
    foldHeight: top + FOLD_ROWS * rowPitch + 8 + FOLD_SLACK,
  };
}

// Where each column starts and how wide it is, in cells.
export function columnsOf(dest) {
  const widths = [["TIME", WIDTHS.time], ["FLIGHT", WIDTHS.flight], ["DESTINATION", dest], ["GATE", WIDTHS.gate], ["REMARKS", WIDTHS.remarks]];
  let start = 0;
  return widths.map(([name, width]) => {
    const column = { name, start, width };
    start += width;
    return column;
  });
}

// What a flap can show: capitals and the drum's marks, one space between words.
export function plainText(text) {
  const folded = String(text ?? "").normalize("NFD").replace(/[̀-ͯ]/g, "").toUpperCase();
  return [...folded].map((ch) => (DRUM.includes(ch) ? ch : " ")).join("").replace(/ +/g, " ").trim();
}

const fitText = (text, width) => text.slice(0, width).trimEnd().padEnd(width);

// The remark for a status. Anyone blocked is DELAYED, a sub-agent included, because that's
// the one thing the person has to act on. Any other sub-agent is CONNECTING.
export function remarkOf(status, sub = false) {
  if (status === "blocked" || status === "error") return "DELAYED";
  if (sub) return "CONNECTING";
  return { working: "BOARDING", done: "DEPARTED", idle: "AT GATE" }[status] ?? "AT GATE";
}

// Most urgent first. The app ranks done above working, and a DEPARTED row is about to go.
const ORDER = ["DELAYED", "DEPARTED", "BOARDING", "CONNECTING", "AT GATE"];

// What a blocked terminal is waiting on, from its own report: its message, else its kind.
const KINDS = { permission: "needs permission", question: "has a question", auth: "needs a sign-in" };
export function blockedReason(term) {
  const report = term?.report;
  if (typeof report === "string" && report.trim()) return report.trim();
  if (typeof report?.msg === "string" && report.msg.trim()) return report.msg.trim();
  if (KINDS[report?.kind]) return KINDS[report.kind];
  return term?.status === "error" ? "stopped on an error" : "waiting on you";
}

// The first name that still says something once it's on flaps: a tab someone named, else
// the terminal's name, else its title, else the program.
export function destinationOf(agent, term) {
  const tab = /^\d+$/.test(agent?.tab_name ?? "") ? null : agent?.tab_name;
  return [tab, term?.name, agent?.title, agent?.program?.name].map(plainText).find(Boolean) ?? "AGENT";
}

// The nearest agent up a terminal's line of parents, if there is one. A terminal started from
// a plain shell has a parent too, and that doesn't make it a sub-agent.
function parentAgent(term, byAddress, listed) {
  const seen = new Set();
  for (let t = term; t?.parent && !seen.has(t.address); t = byAddress.get(t.parent)) {
    seen.add(t.address);
    if (listed.has(t.parent)) return t.parent;
  }
  return null;
}

// Every agent and live sub-agent as a flight, not yet timed, coded or sorted.
// `list`: agents(). `all`: terminals(). A sub-agent without a pane isn't in agents().
export function readFlights(list, all) {
  const byAddress = new Map(all.map((t) => [t.address, t]));
  const listed = new Map(list.map((a) => [a.address, a]));
  const flight = (address, pane, status, sub, name, space, term) => {
    const remark = remarkOf(status, sub);
    return { address, pane, sub, name, space, remark, reason: remark === "DELAYED" ? blockedReason(term) : null, stamp: term?.status_ms || null };
  };
  const flights = list.map((a) => {
    const term = byAddress.get(a.address);
    const sub = Boolean(term && parentAgent(term, byAddress, listed));
    return flight(a.address, a.pane, a.status, sub, destinationOf(a, term), a.space_name, term);
  });
  for (const t of all) {
    if (listed.has(t.address) || t.state !== "live") continue;
    const parent = listed.get(parentAgent(t, byAddress, listed));
    if (parent) flights.push(flight(t.address, null, t.status, true, destinationOf(null, t), parent.space_name, t));
  }
  return flights;
}

// Keeps when each flight took its remark. `seen`: address -> { remark, at }. The terminal's
// own stamp is used when it has one. An empty list says nothing about who's gone, because
// that's what a window that has only just started looks like.
export function noteChanges(seen, flights, now) {
  const next = {};
  let changed = false;
  for (const f of flights) {
    const old = seen[f.address];
    if (old?.remark === f.remark) next[f.address] = old;
    else {
      next[f.address] = { remark: f.remark, at: Math.min(f.stamp ?? now, now) };
      changed = true;
    }
  }
  if (!flights.length) return { seen, changed: false };
  if (Object.keys(seen).some((address) => !next[address])) changed = true;
  return { seen: next, changed };
}

// What the board shows, in order: done rows drop off after a while, and the rest sort by
// remark, then by when they took it. `seen`: noteChanges'.
export function boardOrder(flights, seen, now, clearAfter = DONE_CLEAR_MS) {
  return flights
    .map((f) => ({ ...f, since: seen[f.address]?.at ?? now }))
    .filter((f) => !(f.remark === "DEPARTED" && now - f.since >= clearAfter))
    .sort((x, y) => ORDER.indexOf(x.remark) - ORDER.indexOf(y.remark) || x.since - y.since || x.address.localeCompare(y.address));
}

// Two letters for a space: the first of its first two words, or a word's first letter and
// next consonant, so `lantern` is LN.
export function spaceCode(name) {
  const words = plainText(name).split(/[^A-Z0-9]+/).filter(Boolean);
  if (!words.length) return "XX";
  if (words.length > 1) return words[0][0] + words[1][0];
  const [first, ...rest] = words[0];
  return first + (rest.find((ch) => !"AEIOU".includes(ch)) ?? rest[0] ?? "X");
}

// A number from 100 to 999 that only depends on the agent's address.
export function flightNumber(address) {
  let hash = 2166136261;
  for (const ch of String(address)) hash = Math.imul(hash ^ ch.codePointAt(0), 16777619);
  return 100 + ((hash >>> 0) % 900);
}

// Gives each flight its code. An agent keeps the code it first got, even if its space is
// renamed. `codes`: address -> code, added to here. Says whether it added any.
export function assignCodes(flights, codes) {
  const taken = new Set(Object.values(codes));
  let added = false;
  for (const f of flights) {
    if (!codes[f.address]) {
      const prefix = spaceCode(f.space);
      let n = flightNumber(f.address);
      while (taken.has(`${prefix} ${n}`)) n = n >= 999 ? 100 : n + 1;
      codes[f.address] = `${prefix} ${n}`;
      taken.add(codes[f.address]);
      added = true;
    }
    f.code = codes[f.address];
  }
  return added;
}

export const clockText = (ms) => {
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
};

// A flight with a reason takes two rows of the board.
const rowsOf = (f) => (f.reason ? 2 : 1);

function chunks(flights, room) {
  const pages = [[]];
  let left = room;
  for (const f of flights) {
    if (rowsOf(f) > left && pages.at(-1).length) {
      pages.push([]);
      left = room;
    }
    pages.at(-1).push(f);
    left -= rowsOf(f);
  }
  return pages;
}

// Pages of flights. DELAYED rows stay on every page, so one never waits a page turn to be
// seen. Only when they'd fill the board do they take turns with everyone else.
export function pagesOf(flights, perPage) {
  const delayed = flights.filter((f) => f.remark === "DELAYED");
  const others = flights.filter((f) => f.remark !== "DELAYED");
  const room = perPage - delayed.reduce((n, f) => n + rowsOf(f), 0);
  if (!delayed.length || room < 1) return chunks(flights, perPage);
  return chunks(others, room).map((page) => [...delayed, ...page]);
}

export function flightLine(f, dest) {
  return fitText(clockText(f.since), WIDTHS.time) + fitText(f.code ?? "", WIDTHS.flight) + fitText(plainText(f.name), dest)
    + fitText(plainText(f.space), WIDTHS.gate) + fitText(f.remark, WIDTHS.remarks);
}

// The second row of a DELAYED flight: its reason, from under DESTINATION to the board's end.
export function reasonLine(f, dest) {
  const lead = WIDTHS.time + WIDTHS.flight;
  return " ".repeat(lead) + fitText(plainText(f.reason), dest + WIDTHS.gate + WIDTHS.remarks);
}

// One page as the board's rows: `lines` for the flaps, and for each row what it belongs to,
// which is what a press and its colors go by. `shape`: fitBoard's.
export function pageRows(page, shape, empty = false) {
  const lines = [], rows = [];
  const blank = " ".repeat(shape.cells);
  for (const f of page) {
    lines.push(flightLine(f, shape.dest));
    rows.push({ kind: "flight", flight: f });
    if (f.reason) {
      lines.push(reasonLine(f, shape.dest));
      rows.push({ kind: "reason", flight: f });
    }
  }
  lines.length = rows.length = Math.min(lines.length, shape.rows);
  while (lines.length < shape.rows) {
    lines.push(blank);
    rows.push({ kind: "blank", flight: null });
  }
  if (empty) {
    const text = "NO DEPARTURES";
    const middle = Math.floor((shape.rows - 1) / 2);
    // Inside DESTINATION, so a column gap doesn't split the words.
    const start = WIDTHS.time + WIDTHS.flight + Math.max(0, Math.floor((shape.dest - text.length) / 2));
    lines[middle] = (" ".repeat(start) + text).padEnd(shape.cells).slice(0, shape.cells);
    rows[middle] = { kind: "notice", flight: null };
  }
  return { lines, rows };
}

// The letters a cell shows on its way from one to another, ending on the new one. The drum
// only runs forward, and a long way round shows just its last few letters.
export function flipPath(from, to, most = MAX_FLIPS) {
  const size = DRUM.length;
  const start = Math.max(DRUM.indexOf(from), 0), end = Math.max(DRUM.indexOf(to), 0);
  const steps = Math.min((end - start + size) % size, most);
  return Array.from({ length: steps }, (_, i) => DRUM[(end - steps + 1 + i + size) % size]);
}

// A grid of flaps. `aim` says what it should read, `tick` moves it half a flap, and a cell
// goes through a falling half (the new letter's top over the old one's bottom) and a landed
// half for each letter on its way. Nothing here runs by itself.
export class Flaps {
  constructor(rows, cells, lines = null) {
    this.rows = rows;
    this.cells = cells;
    this.grid = Array.from({ length: rows }, (_, r) => Array.from({ length: cells }, (_, c) => {
      const ch = lines?.[r]?.[c] ?? " ";
      return { shown: ch, want: ch, path: [], wait: 0, half: false };
    }));
    this.moving = [];
    this.aimed = lines ? lines.join("\n") : null;
  }

  fits(rows, cells) {
    return rows === this.rows && cells === this.cells;
  }

  get busy() {
    return this.moving.length > 0;
  }

  // Returns how many cells have somewhere new to go. A cell already moving turns at once;
  // the others wait for the wave to reach them.
  aim(lines) {
    const key = lines.join("\n");
    if (key === this.aimed) return 0;
    this.aimed = key;
    let moved = 0, order = 0;
    this.grid.forEach((row, r) => {
      let touched = false;
      row.forEach((cell, c) => {
        const want = lines[r]?.[c] ?? " ";
        if (cell.want === want) return;
        cell.want = want;
        const already = cell.path.length > 0;
        if (cell.half) {
          cell.shown = cell.path[0];
          cell.half = false;
        }
        cell.path = flipPath(cell.shown, want);
        cell.wait = already ? 0 : order * ROW_LAG + Math.floor(c / COLS_PER_TICK);
        if (!already && cell.path.length) this.moving.push(cell);
        moved += 1;
        touched = true;
      });
      if (touched) order += 1;
    });
    return moved;
  }

  // Moves every moving cell on. Says whether any still have further to go.
  tick() {
    for (const cell of this.moving) {
      if (cell.wait > 0) cell.wait -= 1;
      else if (cell.path.length && !cell.half) cell.half = true;
      else if (cell.path.length) {
        cell.shown = cell.path.shift();
        cell.half = false;
      }
    }
    this.moving = this.moving.filter((cell) => cell.path.length > 0);
    return this.busy;
  }

  // Whether a cell has stopped on its letter, with nothing still to fall.
  settled(r, c) {
    return this.grid[r][c].path.length === 0;
  }

  // What a cell shows now: its top half and its bottom half, the same when it's at rest.
  at(r, c) {
    const cell = this.grid[r][c];
    return { top: cell.half ? cell.path[0] : cell.shown, bottom: cell.shown };
  }
}
