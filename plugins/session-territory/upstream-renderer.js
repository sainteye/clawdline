/* Source-derived browser study: herdr-territory b53947d6b9ebe10208d1b7edf2948d3e68dd3086.
MIT License

Copyright (c) 2026 Adam Entwistle

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

*/
import { layoutMap } from "./upstream/treemap.js";
import { addTouches } from "./upstream/activity.js";
// Works out one picture of the map: what's hot, who is where, what's colliding. No Herdr imports, so `node --test` can load
// this file. view.js only draws what this says, and `sig` lets main.js skip the redraw when nothing here has changed.
import { ago, beat, collisionLine, collisions, heatByFile, lastTouch, trailOf, visitors } from "./upstream/activity.js";
const MAX_DOTS = 12; // Upstream crew.js; no transcript/terminal adapter is loaded.

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

// A made-up repo and crew for screenshots and GIFs. Nothing here touches a real repo, agent or file. The story goes through
// the same rules as real calls. No imports, so `node --test` can load this file.
//
// A step is 1.5 s on the screen and 15 s on the story's clock, so heat fades while you watch. Calls from before the first
// step are drawn from the same 24 steps, which means the loop has no seam and step 0 is already a lived-in map.

export const DEMO_STEP_MS = 1500;
const STORY_STEP_MS = 15000;
const START = 1893456000000;
const LOOKBACK = 60; // 15 minutes of story, as far back as a block remembers

export const DEMO_ROOT = "/work/harbour";

const SIZES = {
  "src/api/router.ts": 18400, "src/api/handlers/orders.ts": 15200, "src/api/handlers/users.ts": 9100, "src/api/handlers/search.ts": 7300,
  "src/api/middleware.ts": 6200, "src/api/errors.ts": 2100,
  "src/auth/session.ts": 12100, "src/auth/tokens.ts": 8300, "src/auth/passwords.ts": 4100, "src/auth/oauth.ts": 11000,
  "src/db/schema.ts": 21500, "src/db/queries.ts": 16800, "src/db/pool.ts": 3200, "src/db/migrations/001_start.sql": 5100,
  "src/db/migrations/002_orders.sql": 3900, "src/db/migrations/003_search.sql": 2800, "src/db/migrations/004_tokens.sql": 1500,
  "src/jobs/queue.ts": 9900, "src/jobs/mailer.ts": 5400, "src/jobs/cleanup.ts": 2200, "src/jobs/reports.ts": 6600,
  "src/config.ts": 3400, "src/index.ts": 1800, "src/log.ts": 1400,
  "web/app/page.tsx": 8800, "web/app/layout.tsx": 3100, "web/app/orders/page.tsx": 14200, "web/app/orders/detail.tsx": 10100,
  "web/app/account/page.tsx": 7700, "web/app/account/billing.tsx": 9400, "web/app/search/page.tsx": 6500,
  "web/components/table.tsx": 13300, "web/components/nav.tsx": 5900, "web/components/form.tsx": 11800, "web/components/modal.tsx": 4300,
  "web/components/chart.tsx": 12700, "web/components/button.tsx": 1900, "web/components/badge.tsx": 900, "web/components/toast.tsx": 2400,
  "web/styles/theme.css": 7200, "web/styles/reset.css": 1300, "web/lib/fetcher.ts": 3700, "web/lib/format.ts": 2600,
  "tests/api/orders.test.ts": 13900, "tests/api/users.test.ts": 8200, "tests/auth/session.test.ts": 10500, "tests/auth/tokens.test.ts": 6100,
  "tests/db/queries.test.ts": 9600, "tests/web/table.test.tsx": 5200, "tests/helpers.ts": 2900,
  "docs/api.md": 16200, "docs/setup.md": 4800, "docs/decisions.md": 7900,
  "scripts/seed.ts": 4500, "scripts/release.sh": 2300,
  "package.json": 2600, "tsconfig.json": 700, "README.md": 5600, ".gitignore": 120, "pnpm-lock.yaml": 310000,
};

const R = "Read", E = "Edit", W = "Write", G = "Grep";

// Each step: [agent, tool, path] calls. Wren and Otter meet on the router at step 7, and it clears after step 15.
const STORY = [
  [["wren", R, "src/api/middleware.ts"], ["otter", R, "src/db/queries.ts"], ["finch", R, "web/components/table.tsx"], ["heron", R, "docs/api.md"]],
  [["wren", R, "src/auth/session.ts"], ["finch", E, "web/components/table.tsx"], ["swift", R, "tests/api/orders.test.ts"]],
  [["otter", E, "src/db/queries.ts"], ["heron", R, "src/api/handlers/orders.ts"], ["swift", R, "tests/helpers.ts"]],
  [["wren", E, "src/auth/session.ts"], ["finch", R, "web/app/orders/page.tsx"], ["swift", E, "tests/api/orders.test.ts"]],
  [["otter", R, "src/db/schema.ts"], ["heron", R, "src/api/handlers/users.ts"], ["finch", E, "web/app/orders/page.tsx"]],
  [["wren", R, "src/api/router.ts"], ["otter", E, "src/db/schema.ts"], ["swift", R, "tests/auth/session.test.ts"]],
  [["wren", E, "src/api/router.ts"], ["finch", R, "web/components/form.tsx"], ["heron", E, "docs/api.md"]],
  [["otter", R, "src/api/router.ts"], ["swift", E, "tests/auth/session.test.ts"], ["finch", E, "web/components/form.tsx"]],
  [["wren", E, "src/api/router.ts"], ["heron", R, "docs/decisions.md"]],
  [["finch", R, "web/app/orders/detail.tsx"], ["swift", R, "tests/db/queries.test.ts"]],
  [["otter", E, "src/api/router.ts"], ["finch", E, "web/app/orders/detail.tsx"], ["heron", E, "docs/decisions.md"]],
  [["wren", R, "src/api/handlers/orders.ts"], ["swift", E, "tests/db/queries.test.ts"]],
  [["otter", R, "src/api/handlers/search.ts"], ["finch", R, "web/components/nav.tsx"]],
  [["wren", E, "src/api/handlers/orders.ts"], ["otter", E, "src/api/handlers/search.ts"], ["heron", R, "scripts/release.sh"]],
  [["finch", E, "web/components/nav.tsx"], ["swift", R, "tests/web/table.test.tsx"]],
  [["wren", R, "src/auth/tokens.ts"], ["otter", R, "src/db/migrations/003_search.sql"]],
  [["wren", E, "src/auth/tokens.ts"], ["otter", W, "src/db/migrations/005_index.sql"], ["finch", R, "web/styles/theme.css"]],
  [["heron", G, "src"], ["swift", E, "tests/web/table.test.tsx"], ["finch", E, "web/styles/theme.css"]],
  [["wren", R, "src/config.ts"], ["otter", R, "src/jobs/queue.ts"]],
  [["otter", E, "src/jobs/queue.ts"], ["finch", R, "web/app/account/billing.tsx"]],
  [["wren", E, "src/config.ts"], ["finch", E, "web/app/account/billing.tsx"]],
  [["otter", R, "src/jobs/reports.ts"], ["swift", R, "tests/auth/tokens.test.ts"]],
  [["wren", R, "src/index.ts"], ["otter", E, "src/jobs/reports.ts"], ["finch", R, "web/lib/fetcher.ts"]],
  [["heron", R, "README.md"], ["swift", E, "tests/auth/tokens.test.ts"], ["finch", E, "web/lib/fetcher.ts"]],
];

const CREW = [
  { key: "wren", name: "Wren", readable: true },
  { key: "otter", name: "Otter", readable: true },
  { key: "finch", name: "Finch", readable: true },
  { key: "heron", name: "Heron", readable: true },
  { key: "swift", name: "Swift", readable: true, owner: "wren", sub: true },
  { key: "lark", name: "Lark", readable: false },
];

const blockedAt = (at) => at >= 19 && at <= 22;
const wrap = (step) => ((step % STORY.length) + STORY.length) % STORY.length;

export function demoRepo() {
  const files = Object.entries(SIZES).map(([path, bytes]) => [path, bytes]);
  return { state: "ok", root: DEMO_ROOT, name: "harbour", stamp: "demo", total: files.length, guessed: 0, files, rest: [] };
}

// `{ now, crew, events }` at a step. `events` is keyed by agent, in the form territory.py prints.
export function demoWorld(step) {
  const now = START + step * STORY_STEP_MS, events = {};
  for (let back = LOOKBACK; back >= 0; back -= 1) {
    const at = now - back * STORY_STEP_MS;
    for (const [agent, tool, path] of STORY[wrap(step - back)]) (events[agent] ??= []).push([at, tool, `${DEMO_ROOT}/${path}`, null]);
  }
  const crew = CREW.map((c) => ({
    ...c, address: null, pane: null, owner: c.owner ?? null, sub: c.sub ?? false,
    status: c.key === "heron" && blockedAt(wrap(step)) ? "blocked" : c.key === "lark" ? "idle" : "working",
  }));
  return { now, crew, events };
}

// Only the upstream made-up story is accepted here. This is not a host adapter.
export function syntheticFixture(step = 7) {
  const repo = demoRepo(), world = demoWorld(step);
  const kinds = { Read: 'read', Edit: 'write', Write: 'write', Grep: 'scan' };
  const touches = Object.entries(world.events).flatMap(([agent, events]) => events.map(([at, tool, path]) => ({
    at, agent, tool, path: path.slice(DEMO_ROOT.length + 1), kind: kinds[tool],
  })));
  return { synthetic: true, repo, crew: world.crew, now: world.now, touches: addTouches([], touches, world.now) };
}

// view.js uses host theme tokens. These are a documented approximation of the
// supplied preview's dark theme, not an invented categorical Session palette.
export const previewTheme = {
  canvas: '#121416', surface: '#181a1c', raised: '#2b2d2f', hover: '#35383b',
  foreground: '#e6e6e6', muted: '#a0a0a0', dim: '#737373', blocked: '#f16c5b',
  border: '#2a2c2f', popover: '#202225', input: '#42464b', done: '#a3be8c',
  colors: ['#94b4e3', '#a3be8c', '#b4a0ce', '#88c0c9', '#e2c782', '#adc6e9', '#b3cba0', '#c8b6df', '#a0d6db', '#eedaa2'],
};
export const stateWords = {
  en: { ready:'Ready', working:'Working', holding:'On hold', waiting_you:'Waiting for you', waiting_session:'Waiting for a Session', unknown:'State unknown', milestone_complete:'Milestone reported complete', work_complete:'Work reported complete' },
  zh: { ready:'準備就緒', working:'工作中', holding:'暫停中', waiting_you:'等待你回覆', waiting_session:'等待其他 Session', unknown:'狀態不明', milestone_complete:'階段回報完成', work_complete:'工作回報完成' },
};
const copy = {
  en: { synthetic:'SYNTHETIC REFERENCE · no live files or transcripts', title:'harbour', fold:'Terminals unavailable', map:'Map', still:'Still mode', list:'Readable list', files:'files', empty:'No synthetic files', stale:'Stale / disconnected · activity withheld', dock:'Synthetic story · 5 of 6 readable agents', noTranscript:'no transcript', noFile:'no file yet', outside:'outside the repo', terminal:'Native terminals are unavailable. No terminal or transcript is simulated.', states:'State vocabulary only · no file locations inferred', unknown:'Unknown freshness · activity withheld', fresh:'Fresh synthetic fixture', select:'Select a synthetic agent', open:'Open Session unavailable · synthetic reference', done:'Completion states are reports, not acceptance.', license:'Source: Adam Entwistle · MIT · pinned b53947d', visitor:'No agent here in the last 15 minutes', reads:'read', writes:'wrote', ago:'min ago', now:'just now', reading:'reading', writing:'writing' },
  zh: { synthetic:'合成參考資料 · 非即時檔案或逐字紀錄', title:'harbour', fold:'終端畫面不可用', map:'地圖', still:'靜止模式', list:'易讀清單', files:'個檔案', empty:'沒有合成檔案', stale:'資料過時／連線中斷 · 不顯示活動', dock:'合成故事 · 6 個代理中 5 個可讀', noTranscript:'無逐字紀錄', noFile:'尚無檔案', outside:'位於儲存庫外', terminal:'原生終端畫面不可用。此處不模擬終端或對話內容。', states:'僅展示狀態詞彙 · 不推測檔案位置', unknown:'新鮮度不明 · 不顯示活動', fresh:'新鮮合成樣本', select:'選取合成代理', open:'無法開啟 Session · 僅為合成參考', done:'完成狀態是回報，不表示已驗收。', license:'原作：Adam Entwistle · MIT · 固定版本 b53947d', visitor:'最近 15 分鐘沒有代理到訪', reads:'讀取', writes:'寫入', ago:'分鐘前', now:'剛剛', reading:'讀取中', writing:'寫入中' },
};
function element(tag, cls, text) {
  const n = document.createElement(tag); if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}
function box(node, rect) { Object.assign(node.style, { left:rect.x+'px', top:rect.y+'px', width:rect.w+'px', height:rect.h+'px' }); return node; }
const visualState = s => s === 'working' ? 'working' : s === 'waiting_you' ? 'blocked' : ['milestone_complete','work_complete'].includes(s) ? 'done' : 'idle';

export function mountTerritory(container, initial = {}) {
  let context = { language:'en', visible:true, reducedMotion:false, connection:'connected', freshness:'fresh', ...initial };
  let fixture = syntheticFixture(), picked = null, selected = null, still = true, folded = false, listMode = false, disposed = false;
  let lastFrame, awaiting = context.connection !== 'connected';
  const safe = () => !awaiting && context.connection === 'connected' && context.freshness === 'fresh';
  function render() {
    if (disposed) return;
    const zh = context.language.startsWith('zh'), lang = zh ? 'zh' : 'en', w = copy[lang], t = previewTheme;
    container.replaceChildren(); container.lang = zh ? 'zh-Hant-TW' : 'en';
    const motion = !still && context.visible && !context.reducedMotion && safe();
    container.dataset.motion = motion ? 'on' : 'off';
    container.dataset.synthetic = 'true';
    const toolbar = element('div','study-toolbar');
    toolbar.append(element('strong','synthetic',w.synthetic));
    const stillButton = element('button','',w.still); stillButton.setAttribute('aria-pressed', String(still)); stillButton.onclick = () => { still = !still; render(); };
    const listButton = element('button','',w.list); listButton.setAttribute('aria-pressed', String(listMode)); listButton.onclick = () => { listMode = !listMode; render(); };
    toolbar.append(stillButton,listButton); container.append(toolbar);
    const pixelWidth = container.clientWidth, zoom = pixelWidth >= 900 ? pixelWidth / 1600 * 1.2 : 1;
    const width = pixelWidth / zoom, height = 460;
    const map = layoutMap({ files:fixture.repo.files, rest:fixture.repo.rest, width, height:zoneOf(height) });
    const crew = fixture.crew.map(c => ({ ...c, status:safe() ? c.status : 'idle' }));
    const frame = buildFrame({ map, crew, touches:safe() ? fixture.touches : [], now:fixture.now, width, height, picked, repoName:fixture.repo.name,
      nameOf:key => crew.find(c => c.key === key)?.name ?? key, ownerOf:key => crew.find(c => c.key === key)?.owner ?? null });
    lastFrame = frame;
    const viewport = element('div','territory-viewport'); viewport.style.height = (folded ? HEADER_H : height) * zoom+'px';
    const root = element('div','territory'); root.style.width = width+'px'; root.style.height = height+'px'; root.style.transform = `scale(${zoom})`;
    viewport.append(root); container.append(viewport);
    const header = element('header','map-header');
    header.append(element('strong','repo-title',fixture.repo.name));
    if (width >= 640) header.append(element('span','facts',`${thousands(map.count)} ${w.files}${map.grouped ? ` · ${map.grouped}` : ''}`));
    if (frame.line) {
      let line = frame.line;
      if (zh) line = line.replaceAll('writing',w.writing).replaceAll('reading',w.reading).replaceAll(' and ','、').replace(/\(\+(\d+) more\)/,'（另 $1 項）');
      const clash = element('button','clash-line',line); clash.onclick=()=>{picked=frame.firstClash;render();}; header.append(clash);
    } else header.append(element('span','spacer'));
    const fold = element('button','fold',folded ? w.map : w.fold); fold.setAttribute('aria-expanded',String(!folded)); fold.onclick=()=>{folded=!folded;render();}; header.append(fold); root.append(header);
    const zone = element('div','map-zone'); zone.style.height = height-HEADER_H+'px'; root.append(zone);
    if (!map.tiles.length) zone.append(element('p','empty-note',w.empty));
    const colorOf = key => t.colors[Math.max(0,fixture.crew.findIndex(c=>c.key===key)) % t.colors.length];
    for (const f of map.folders) {
      const node=box(element('div','folder'),f); if(f.w>=44) node.append(element('span','folder-label',f.label)); zone.append(node);
    }
    for (const tile of map.tiles) {
      const button=box(element('button','tile'),tile); button.dataset.path=tile.path; button.dataset.key=tile.key;
      button.setAttribute('aria-label',tile.path || tile.label); button.setAttribute('aria-pressed',String(picked===tile.key));
      const heat=frame.heat.get(tile.key), visitor=frame.seen.get(tile.key), clash=frame.clash.has(tile.key);
      button.dataset.heat=String(Boolean(heat));
      if(heat||visitor) {const wash=element('span','wash'); wash.style.background=colorOf(heat?.agent??visitor); wash.style.opacity=heat ? 0.14+0.76*heat.level : 0.14; button.append(wash);}
      if(clash)button.append(element('span','collision-wash'));
      if(clash||picked===tile.key){const border=element('span','tile-border');border.style.borderColor=clash?t.blocked:t.foreground;button.append(border);}
      if(tile.w>=44&&tile.h>=18) {const label = tile.kind==='group' && zh ? tile.label.replace(/(\d+) small files?/,'$1 個小檔案').replace(/(\d+) files/,'$1 個檔案') : tile.label;button.append(element('span','tile-label',label));}
      button.onclick=()=>{picked=picked===tile.key?null:tile.key;selected=null;render();};zone.append(button);
    }
    const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');svg.classList.add('trails');svg.setAttribute('width',width);svg.setAttribute('height',frame.zone);svg.setAttribute('aria-hidden','true');
    for(const line of frame.trails){const path=document.createElementNS(svg.namespaceURI,'polyline');path.setAttribute('points',line.points.map(p=>`${p.x},${p.y}`).join(' '));path.setAttribute('stroke',colorOf(line.agent)+'80');svg.append(path);}zone.append(svg);
    const dock=element('div','dock',!safe() ? context.freshness==='unknown'?w.unknown:w.stale : w.dock);dock.style.top=frame.zone+'px';zone.append(dock);
    const size=Math.max(11,Math.min(17,Math.round(width/85)));
    for(const dot of frame.dots){const button=box(element('button','agent-dot'),{x:dot.x-HIT/2,y:dot.y-HIT/2,w:HIT,h:HIT});button.dataset.agent=dot.key;button.dataset.status=dot.status;button.setAttribute('aria-label',dot.name);
      const color=colorOf(dot.key), docked=dot.where==='dock', working=dot.status==='working', blocked=dot.status==='blocked';
      if((working||blocked)&&!docked){for(const [cls,scale] of [['halo',2.8],['ring',1.7]]){const n=element('span',cls);Object.assign(n.style,{width:size*scale+'px',height:size*scale+'px',background:blocked?t.blocked:color});button.append(n);}}
      const core=element('span','core'),diameter=docked?size*.7:dot.sub?size*.75:size;Object.assign(core.style,{width:diameter+'px',height:diameter+'px',background:dot.readable?color:t.dim,borderColor:blocked?t.blocked:dot.status==='done'?t.done:t.foreground,opacity:!working&&!blocked?'.65':'1'});button.append(core);
      const label=element('span','agent-label');label.append(element('span',docked?'dock-name':'agent-name',dot.name));Object.assign(label.style,{left:dot.labelLeft+'px',top:dot.labelTop+'px',width:dot.labelWidth+'px',textAlign:docked?'left':'center'});
      if(docked)label.append(element('span','dock-word',' '+(!dot.readable?w.noTranscript:dot.word==='outside the repo'?w.outside:w.noFile)));button.append(label);
      button.onclick=()=>{selected=dot.key;render();};zone.append(button);
    }
    if(frame.card){const c=frame.card,card=box(element('aside','file-card'),{x:c.x,y:c.y,w:CARD_W,h:c.height});card.setAttribute('aria-label',c.title);const head=element('div','card-head');head.append(element('strong','card-title',c.title),element('span','card-note',zh?c.note.replace(/(\d+) files?/,'$1 個檔案'):c.note));card.append(head);
      if(!c.rows.length)card.append(element('div','card-row',w.visitor));
      for(const row of c.rows){const r=element('button','card-row'),dot=element('span','visitor-dot');dot.style.background=colorOf(row.agent);const age=Math.floor((beat(fixture.now)-row.at)/60000);r.append(dot,element('strong','visitor-name',row.name),element('span','visitor-time',zh?`${age<1?w.now:age+' '+w.ago} ${row.kind==='write'?w.writes:w.reads}`:row.when));r.onclick=()=>{selected=row.agent;render();};card.append(r);}zone.append(card);}
    const terminal=element('div','terminal-unavailable',w.terminal);container.append(terminal);
    const states=element('section','state-vocabulary');states.append(element('p','',w.states));
    for(const [state,label] of Object.entries(stateWords[lang])){const row=element('div','state-row',label);row.dataset.state=state;row.dataset.visual=visualState(state);states.append(row);}states.append(element('p','',w.done));container.append(states);
    const detail=element('section','selection');detail.append(element('strong','',selected?crew.find(c=>c.key===selected)?.name:w.select));const open=element('button','',w.open);open.disabled=true;detail.append(open);container.append(detail);
    const list=element('section','file-list');list.hidden=!listMode;for(const [path,bytes] of fixture.repo.files){const row=element('button','file-row',`${path} · ${sizeText(bytes)}`);row.onclick=()=>{picked=map.locate(path)?.key??null;render();};list.append(row);}container.append(list);
    container.append(element('footer','study-footer',w.license));
  }
  const observer=new ResizeObserver(render);observer.observe(container);
  const keydown=e=>{if(e.key==='Escape'){picked=null;selected=null;render();}};document.addEventListener('keydown',keydown);
  render();
  return {
    setContext(next){if(next.connection==='disconnected')awaiting=true;context={...context,...next};render();},
    setFixture(next){if(next?.synthetic!==true)throw new Error('synthetic_fixture_required');fixture=next;awaiting=context.connection!=='connected';picked=null;selected=null;render();},
    frame(){return lastFrame;},
    dispose(){disposed=true;observer.disconnect();document.removeEventListener('keydown',keydown);container.replaceChildren();},
  };
}

