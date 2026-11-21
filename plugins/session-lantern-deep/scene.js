/* Lantern Deep: an original hand-drawn underground scene for Clawdline Sessions.
 * Every character, enemy, prop and motion here is authored for this view. */
(() => {
"use strict";
const $ = id => document.getElementById(id);
const STATES = ["ready", "working", "holding", "waiting_you", "waiting_session", "unknown", "milestone_complete", "work_complete"];
const WORDS = {
  en: {
    name: "Lantern Deep", sessions: n => n + (n === 1 ? " Session" : " Sessions"), needs: n => n + " need you", needsNone: "Nobody needs you",
    list: "Readable list", close: "Close", open: "Open Session", next: "Next who needs you", unassigned: "No Project",
    fresh: "Fresh reading", stale: "May be stale", unknownFresh: "Freshness unknown", disconnected: "Disconnected. Readings may be stale.",
    empty: "The deep is quiet. No Sessions yet.", error: "The host is unavailable", cannotOpen: "This Session cannot be opened now",
    still: "Motion is paused", hint: "Drag or scroll to wander. Select a wanderer to open its Session.",
    states: { ready: "Ready, on patrol", working: "Working, in a fight", holding: "Holding, drilling the blade", waiting_you: "Needs you, puzzled", waiting_session: "Waiting on another Session, climbing", unknown: "No state reported", milestone_complete: "Milestone complete, resting", work_complete: "Work complete, resting" },
  },
  zh: {
    name: "燈淵", sessions: n => n + " 個 Session", needs: n => n + " 個在等你", needsNone: "沒有人在等你",
    list: "文字清單", close: "關閉", open: "開啟 Session", next: "下一個在等你的", unassigned: "未分類 Project",
    fresh: "讀數新鮮", stale: "可能不是最新", unknownFresh: "無法確認新舊", disconnected: "連線中斷，讀數可能不是最新。",
    empty: "深處一片寂靜，目前沒有 Session。", error: "無法連上宿主", cannotOpen: "這個 Session 現在無法開啟",
    still: "動態已暫停", hint: "拖曳或捲動可四處走走，點選角色即可開啟它的 Session。",
    states: { ready: "待命，巡邏中", working: "工作中，正在戰鬥", holding: "暫停中，練劍", waiting_you: "在等你，一臉疑惑", waiting_session: "等其他 Session，攀牆中", unknown: "沒有回報狀態", milestone_complete: "階段完成，休息中", work_complete: "工作完成，休息中" },
  },
};
const ART = { ruins: "%%ART:bg-ruins%%", grotto: "%%ART:bg-grotto%%", crystal: "%%ART:bg-crystal%%", "platform-ruins": "%%ART:platform-ruins%%", "platform-grotto": "%%ART:platform-grotto%%", "platform-crystal": "%%ART:platform-crystal%%", pillar: "%%ART:pillar%%", dummy: "%%ART:dummy%%", gate: "%%ART:gate%%", shell: "%%ART:shell%%", mushrooms: "%%ART:mushrooms%%" };
const CAST = [], FOES = [];
for (let i = 1; i <= 12; i++) CAST.push("char-" + String(i).padStart(2, "0"));
for (let i = 1; i <= 6; i++) FOES.push("enemy-" + i);
/*CAST_ART*/
const BIOMES = [
  { key: "ruins", sky: ["#0a1422", "#12283a", "#060a12"], fog: [120, 170, 200], far: "#16303f", mid: "#0d1d2b", near: "#070d16", rim: "#7fb4cc", mote: [190, 230, 255], lamp: [200, 235, 255] },
  { key: "grotto", sky: ["#07160f", "#12331f", "#040a07"], fog: [120, 190, 140], far: "#143524", mid: "#0b2116", near: "#050e09", rim: "#8fd09a", mote: [200, 255, 190], lamp: [215, 255, 200] },
  { key: "crystal", sky: ["#120a1e", "#2a1638", "#07040d"], fog: [190, 130, 200], far: "#2b1a3f", mid: "#190f29", near: "#0b0613", rim: "#d79be0", mote: [255, 200, 245], lamp: [255, 205, 240] },
];
const SCARF = [38, 348, 188, 96, 268, 18, 160, 318, 210, 60];
const EYE = { ready: [150, 225, 255], working: [255, 190, 90], holding: [200, 210, 255], waiting_you: [255, 120, 130], waiting_session: [170, 200, 255], unknown: [90, 100, 120], milestone_complete: [160, 255, 190], work_complete: [160, 255, 190] };
const G = 1750, TAU = Math.PI * 2;
const clamp = (v, a, b) => v < a ? a : v > b ? b : v, lerp = (a, b, t) => a + (b - a) * t, sign = v => v < 0 ? -1 : 1;
const easeOut = t => 1 - (1 - t) * (1 - t) * (1 - t);
function rng(seed) { let a = seed >>> 0; return () => { a |= 0; a = a + 0x6D2B79F5 | 0; let t = Math.imul(a ^ a >>> 15, 1 | a); t = t + Math.imul(t ^ t >>> 7, 61 | t) ^ t; return ((t ^ t >>> 14) >>> 0) / 4294967296; }; }
function hash(text) { let h = 2166136261; for (let i = 0; i < text.length; i++) { h ^= text.charCodeAt(i); h = Math.imul(h, 16777619); } return h >>> 0; }
const rgba = (c, a) => "rgba(" + c[0] + "," + c[1] + "," + c[2] + "," + a + ")";

const canvas = $("stage"), ctx = canvas.getContext("2d");
let view = null, rows = [], stream = null, revision = -1, awaiting = false, selected = null, listOpen = false, busy = false, openError = "";
let context = { language: "en", visible: true, reducedMotion: false, connection: "disconnected", presentation: "fullscreen" };
let W = 0, H = 0, DPR = 1, scale = 1, groundY = 0, cam = 0, camTarget = 0, camMin = 0, camMax = 0, now = 0, raf = 0, lastFrame = 0, userPanAt = -1e9;
let wild = new Map(), wildList = [];
let chambers = [], stations = new Map(), chars = new Map(), enemies = [], fx = [], motes = [], props = new Map(), lastFocus = null, titleChamber = null, titleAt = 0, placed = false;
const images = {};
const w = () => WORDS[context.language && context.language.startsWith("zh") ? "zh" : "en"];
const status = r => STATES.includes(r.workState) ? r.workState : "unknown";
const freshness = r => context.connection !== "connected" || awaiting ? "stale" : r.freshness;
const live = r => freshness(r) === "fresh";
const motion = () => context.visible && !context.reducedMotion;
const projectName = r => r.project && r.project.label || w().unassigned;

for (const key of Object.keys(ART)) if (ART[key].startsWith("data:")) { const img = new Image(); img.onload = () => { images[key] = img; if (!raf) draw(); }; img.src = ART[key]; }

const greyCache = new Map();
function grey(key) {
  let g = greyCache.get(key); const img = images[key];
  if (!g && img) { g = document.createElement("canvas"); g.width = img.width; g.height = img.height; const c = g.getContext("2d"); c.drawImage(img, 0, 0); const d = c.getImageData(0, 0, g.width, g.height), a = d.data; for (let i = 0; i < a.length; i += 4) { const v = (a[i] * .3 + a[i + 1] * .5 + a[i + 2] * .2) * .62 + 14; a[i] = v * .9; a[i + 1] = v * .96; a[i + 2] = v * 1.06; } c.putImageData(d, 0, 0); greyCache.set(key, g); }
  return g;
}
/* ---------- glow sprites ---------- */
const glowCache = new Map();
function glow(color) {
  const key = color.join(",");
  let g = glowCache.get(key);
  if (!g) {
    g = document.createElement("canvas"); g.width = g.height = 64;
    const c = g.getContext("2d"), grad = c.createRadialGradient(32, 32, 0, 32, 32, 32);
    grad.addColorStop(0, rgba(color, 1)); grad.addColorStop(.25, rgba(color, .45)); grad.addColorStop(.6, rgba(color, .1)); grad.addColorStop(1, rgba(color, 0));
    c.fillStyle = grad; c.fillRect(0, 0, 64, 64); glowCache.set(key, g);
  }
  return g;
}
function drawGlow(x, y, r, color, alpha) { ctx.globalAlpha = alpha; ctx.drawImage(glow(color), x - r, y - r, r * 2, r * 2); ctx.globalAlpha = 1; }

/* ---------- layout: Projects become chambers, states become stations ---------- */
const TIER = [0, -128, -250, -372];
function layout() {
  const map = new Map();
  for (const r of rows) { const key = r.project ? r.project.ref : "￿"; if (!map.has(key)) map.set(key, { key, label: projectName(r), rows: [] }); map.get(key).rows.push(r); }
  const groups = [...map.values()].sort((a, b) => a.label.localeCompare(b.label) || a.key.localeCompare(b.key));
  chambers = []; stations = new Map(); for (const e of wild.values()) e.seen = false;
  let x = 0;
  groups.forEach((g, index) => {
    const by = {}; for (const s of STATES) by[s] = [];
    for (const r of g.rows) by[live(r) ? status(r) : "unknown"].push(r);
    const done = by.milestone_complete.concat(by.work_complete), rnd = rng(hash(g.key) + 17);
    const ch = { key: g.key, label: g.label, index, biome: BIOMES[index % BIOMES.length], hue: SCARF[index % SCARF.length], x0: x, platforms: [], dummies: [], shafts: [], lamps: [], camps: [], pieces: [], beacon: null, count: g.rows.length };
    let at = x + 150; ch.sign = x + 70;
    // Puzzled wanderers gather on the floor beneath a tall beacon near the entrance.
    if (by.waiting_you.length) { ch.beacon = at + 10; by.waiting_you.forEach((r, i) => stations.set(r.ref, { mode: "ponder", x: at + 52 + i * 66, y: 0, ch })); at += 90 + by.waiting_you.length * 66; }
    // Everything else is a unit that can live on the floor or on a floating slab.
    const kinds = { fight: by.working.map(r => [r]), drill: by.holding.map(r => [r]), camp: [], roam: [], statue: [] };
    for (let i = 0; i < done.length; i += 3) kinds.camp.push(done.slice(i, i + 3));
    for (let i = 0; i < by.ready.length; i += 2) kinds.roam.push(by.ready.slice(i, i + 2));
    for (let i = 0; i < by.unknown.length; i += 3) kinds.statue.push(by.unknown.slice(i, i + 3));
    const units = [];
    for (let k = 0, more = true; more; k++) { more = false; for (const kind of ["fight", "roam", "drill", "camp", "statue"]) if (kinds[kind][k]) { units.push({ kind, rows: kinds[kind][k] }); more = true; } }
    const high = Math.round(units.length * .62), slots = [];
    for (let c = 0; slots.length < high; c++) for (const tier of c % 2 ? [2] : [1, 3]) slots.push({ c, tier });
    slots.length = high; const cols = high ? slots[high - 1].c + 1 : 0, low = units.length - high;
    const span = Math.max(340, cols * 236, low * 196), a0 = at, a1 = at + span;
    // Slabs first take every other unit so each kind appears on several levels.
    let hi = 0, lo = 0;
    units.forEach((u, i) => {
      const up = hi < high && (lo >= low || i % 5 !== 0 && (i * 7 + index) % 8 < 6);
      if (up) { const slot = slots[hi++], cx = lerp(a0, a1, (slot.c + .5) / cols) + (rnd() - .5) * 26, wide = u.kind === "drill" || u.kind === "statue" ? 156 : 184, y = TIER[slot.tier] + Math.round((rnd() - .5) * 26); u.plat = { x0: cx - wide / 2, x1: cx + wide / 2, y, tier: slot.tier, biome: ch.biome.key }; ch.platforms.push(u.plat); u.cx = cx; u.y = y; u.hw = wide / 2 - 14; }
      else { u.cx = lerp(a0, a1, (lo + .5) / low); u.y = 0; u.hw = Math.min(82, span / low / 2 - 12); lo++; }
    });
    for (const u of units) {
      const base = { y: u.y, ch, plat: u.plat || null };
      if (u.kind === "fight") stations.set(u.rows[0].ref, { ...base, mode: "fight", x: u.cx, lane: { cx: u.cx, hw: u.hw } });
      else if (u.kind === "drill") { const id = g.key + ":d" + u.rows[0].ref; ch.dummies.push({ id, x: u.cx + 26, y: u.y }); stations.set(u.rows[0].ref, { ...base, mode: "drill", x: u.cx - 18, dummy: id, dummyX: u.cx + 26 }); }
      else if (u.kind === "camp") { const camp = { x: u.cx, y: u.y, beds: [] }; ch.camps.push(camp); u.rows.forEach((r, k) => { const dx = [-46, 46, 82][k]; if (status(r) === "work_complete") camp.beds.push({ x: u.cx + dx, flip: dx > 0 }); stations.set(r.ref, { ...base, mode: "rest", x: u.cx + dx, face: dx < 0 ? 1 : -1 }); }); }
      else if (u.kind === "roam") u.rows.forEach((r, k) => stations.set(r.ref, { ...base, mode: "roam", x: u.cx + (k ? 30 : -30), range: [u.cx - u.hw, u.cx + u.hw] }));
      else u.rows.forEach((r, k) => stations.set(r.ref, { ...base, mode: "statue", x: u.cx + (k - (u.rows.length - 1) / 2) * 44 }));
    }
    ch.lamps.push(a0 + span * .3); if (span > 700) ch.lamps.push(a0 + span * .78);
    // Ground set pieces stand behind everything else.
    const pieceKinds = { ruins: ["gate", "shell"], grotto: ["mushrooms", "shell"], crystal: ["gate", "mushrooms"] }[ch.biome.key];
    for (let k = 0, n = Math.max(1, Math.round(span / 520)); k < n; k++) ch.pieces.push({ kind: pieceKinds[k % 2], x: lerp(a0, a1, (k + .5) / n) + (rnd() - .5) * 120, flip: rnd() < .5, size: .85 + rnd() * .3 });
    at = a1 + 30;
    const shaftCount = Math.ceil(by.waiting_session.length / 3);
    for (let s = 0; s < shaftCount; s++) ch.shafts.push({ left: at + 40 + s * 210, right: at + 40 + s * 210 + 96, top: -292 });
    by.waiting_session.forEach((r, i) => stations.set(r.ref, { mode: "climb", x: ch.shafts[Math.floor(i / 3)].left + 30, y: 0, shaft: ch.shafts[Math.floor(i / 3)], slot: i % 3, ch, plat: null }));
    at += shaftCount * 210 + (shaftCount ? 30 : 0);
    at += 100; ch.x1 = at; chambers.push(ch); x = at + 90;
    // Wild creatures fill the air, the ceiling and the quiet stretches of floor.
    const room = ch.x1 - ch.x0, plan = [];
    for (let k = 0, n = 2 + Math.round(room / 380); k < n; k++) plan.push({ skin: 1, a: ch.x0 + 60, b: ch.x1 - 60, y: -170 - rnd() * 250, speed: 16 + rnd() * 16 });
    for (let k = 0, n = 1 + Math.round(room / 460); k < n; k++) { const sx = lerp(ch.x0 + 90, ch.x1 - 90, (k + rnd()) / n); plan.push({ skin: 3, a: sx, b: sx, y: -330 - rnd() * 90, speed: 0 }); }
    for (let k = 0, n = 1 + Math.round(room / 420); k < n; k++) plan.push({ skin: [0, 2, 4, 0][Math.floor(rnd() * 4)], a: ch.x0 + 50, b: ch.x1 - 50, y: 0, speed: 10 + rnd() * 12 });
    plan.forEach((w, k) => { const id = g.key + ":w" + k; let e = wild.get(id); if (!e) { e = { x: lerp(w.a, w.b, rnd()), face: rnd() < .5 ? 1 : -1, phase: rnd() * TAU, state: "roam", t: 9, hurt: 0, flash: 0, cool: 0, fade: 1, flee: 0 }; wild.set(id, e); } Object.assign(e, { skin: w.skin, a: w.a, b: w.b, home: w.y, y: e.y === undefined ? w.y : e.y, base: w.y, speed: w.speed, seen: true }); });
  });
  for (const [id, e] of wild) if (!e.seen) wild.delete(id); wildList = [...wild.values()];
  const total = chambers.length ? chambers[chambers.length - 1].x1 : 600;
  const half = W / scale / 2;
  if (total <= half * 2) camMin = camMax = total / 2; else { camMin = half - 40; camMax = total - half + 40; }
  camTarget = clamp(camTarget, camMin, camMax);
  for (const d of chambers.flatMap(c => c.dummies)) if (!props.has(d.id)) props.set(d.id, { a: 0, va: 0 });
}

/* ---------- characters ---------- */
function makeChar(r, st) {
  const c = { ref: r.ref, row: r, x: st.x, y: st.y, vx: 0, vy: 0, face: 1, ground: true, st: null, mode: "", sub: "", t: 0, cd: 0, step: 0, sq: 0, sqv: 0, hem: 0, hemv: 0, tilt: 0, tiltTo: 0, run: 0, sword: 1, swordTo: 1, slash: 0, slashKind: "", sit: 0, arm: 0, hit: 0, dash: 0, ghosts: [], scarf: [], alpha: 0, seed: rng(hash(r.ref)), enemy: null, spare: null, plat: null, q: 0, blink: 2, look: 0, sx: 0, sy: 0, cast: 0, land: 0 };
  for (let i = 0; i < 6; i++) c.scarf.push({ x: st.x - i * 4, y: st.y - 31, px: st.x - i * 4, py: st.y - 31 });
  return c;
}
function assign(c, st, snap) {
  const prev = c.st; c.st = st;
  if (prev && prev.mode === st.mode && Math.abs(prev.x - st.x) < 1 && prev.y === st.y) { if (c.plat && c.plat === prev.plat) c.plat = st.plat || null; return; }
  dropEnemies(c);
  if (snap || !motion() || Math.abs(st.x - c.x) > 1500 || !prev || (st.plat && st.plat.y < -150)) {
    if (prev && motion()) { burst(c.x, c.y - 26, c.st.ch.biome.mote, 12); burst(st.x, st.y - 26, c.st.ch.biome.mote, 8); }
    c.x = st.x; c.y = st.y; c.vx = c.vy = 0; c.plat = st.plat || null; c.ground = true; c.alpha = prev && motion() ? 0 : 1;
    for (const p of c.scarf) { p.x = p.px = c.x; p.y = p.py = c.y - 31; }
    begin(c);
  } else { c.mode = "travel"; c.sub = ""; c.sit = 0; c.q = 0; }
}
function dropEnemies(c) { if (c.enemy) c.enemy.dead = true; if (c.spare) c.spare.dead = true; c.enemy = c.spare = null; }
// A wanderer that ends up below its own slab goes back instead of acting out its state from the floor.
function relocate(c) { const st = c.st; dropEnemies(c); if (st.plat.y < -150 || !motion()) { burst(c.x, c.y - 26, st.ch.biome.mote, 10); c.x = st.x; c.y = st.y; c.vx = c.vy = 0; c.plat = st.plat; c.ground = true; c.alpha = 0; for (const q of c.scarf) { q.x = q.px = c.x; q.y = q.py = c.y - 31; } begin(c); } else { c.mode = "travel"; c.sub = ""; c.sit = 0; } }
function begin(c) {
  const st = c.st; c.mode = st.mode; c.sub = "start"; if (st.mode !== "climb") c.plat = st.plat || null; c.t = c.seed() * 1.2; c.cd = 0; c.step = 0; c.tiltTo = 0; c.arm = 0; c.q = 0;
  c.swordTo = st.mode === "rest" || st.mode === "statue" ? 3 : st.mode === "roam" || st.mode === "ponder" || st.mode === "climb" ? 0 : 1;
  if (st.face) c.face = st.face;
  if (st.mode === "rest" || st.mode === "statue" || st.mode === "ponder") { c.x = c.x; }
  if (st.mode === "climb") c.t = st.slot * 1.1 + c.seed();
}
function squash(c, v) { c.sqv += v; }
function dust(x, y, n, color) { for (let i = 0; i < n; i++) fx.push({ kind: "dust", x: x + (Math.random() - .5) * 14, y: y - 2, vx: (Math.random() - .5) * 70, vy: -Math.random() * 40 - 8, age: 0, life: .45 + Math.random() * .3, r: 3 + Math.random() * 4, color: color || [150, 170, 190] }); }
function burst(x, y, color, n) { for (let i = 0; i < n; i++) { const a = Math.random() * TAU, s = 60 + Math.random() * 170; fx.push({ kind: "spark", x, y, vx: Math.cos(a) * s, vy: Math.sin(a) * s - 40, age: 0, life: .35 + Math.random() * .4, r: 5 + Math.random() * 6, color }); } }
function slash(c, kind) {
  c.slash = .0001; c.slashKind = kind; squash(c, kind === "down" ? 2.4 : -1.6);
  if (archer(c)) return;
  const off = kind === "up" ? [0, -62, -Math.PI / 2] : kind === "down" ? [0, 4, Math.PI / 2] : [28 * c.face, -27, 0];
  fx.push({ kind: "arc", c, ox: off[0], oy: off[1], rot: off[2], flip: c.face, age: 0, life: .17 });
}
function attack(c, e, kind, kx, ky) {
  if (!archer(c)) { slash(c, kind); strike(c, e, kx, ky); return; }
  c.slash = .0001; c.slashKind = kind; squash(c, -1.2);
  fx.push({ kind: "arrow", x: c.x + c.face * 14, y: c.y - 30, e, c, kx, ky, age: 0, life: 1.2 });
}
function jump(c, vy) { c.vy = vy; c.ground = false; squash(c, 3.2); dust(c.x, c.y, 3); }
function runTo(c, x, speed, dt) {
  const dx = x - c.x;
  if (Math.abs(dx) < 3) { c.vx = lerp(c.vx, 0, Math.min(1, dt * 16)); return true; }
  const dir = sign(dx); if (c.face !== dir && c.ground && Math.abs(c.vx) > 90) dust(c.x, c.y, 2);
  c.face = dir; c.vx = lerp(c.vx, dir * speed, Math.min(1, dt * 11)); return false;
}
function recruit(c, side) {
  const lane = c.st.lane, base = c.st.y, high = !!c.st.plat; let best = null, bestD = 560;
  for (const w of wildList) { if (w.cool > 0 || w.fade < 1) continue; const fly = w.skin === 1 || w.skin === 3; if (high && !fly) continue; const d = Math.hypot(w.x - lane.cx, w.y - base); if (d < bestD) { bestD = d; best = w; } }
  if (!best) return null;
  const fly = best.skin === 1 || best.skin === 3, e = { base, skin: best.skin, type: fly ? "flyer" : "crawler", x: best.x, y: best.y, vx: 0, vy: 0, hp: best.skin === 5 ? 6 : fly ? 3 : 4, flash: 0, state: "approach", t: 0, lane, face: sign(lane.cx - best.x), dead: false, phase: best.phase, next: 1.5 + c.seed() * 2, tx: lane.cx + side * lane.hw * .8, ty: base + (fly ? -78 : 0) };
  best.cool = 5 + Math.random() * 5; best.fade = 0; best.x = lerp(best.a, best.b, Math.random()); enemies.push(e); return e;
}
function spawnEnemy(c, forced) {
  const called = recruit(c, forced || (c.seed() < .5 ? -1 : 1)); if (called) return called;
  const lane = c.st.lane, side = forced || (c.seed() < .5 ? -1 : 1), skin = [0, 1, 2, 0, 4, 5, 3, 1, 2, 4][Math.floor(c.seed() * 10)], flyer = skin === 1 || skin === 3;
  const base = c.st.y, e = { base, skin, type: flyer ? "flyer" : "crawler", x: lane.cx + side * lane.hw, y: base + (flyer ? -78 : 0), vx: 0, vy: 0, hp: skin === 5 ? 6 : flyer ? 3 : 4, flash: 0, state: "emerge", t: 0, lane, face: -side, dead: false, phase: c.seed() * TAU, next: 1.5 + c.seed() * 2 };
  enemies.push(e); dust(e.x, base, flyer ? 0 : 6, [90, 70, 60]); return e;
}
function strike(c, e, kx, ky) {
  e.hp--; e.flash = .12; e.vx = kx; if (e.type === "flyer") e.vy = ky; e.state = "roam"; e.next = 1.4 + Math.random() * 2;
  c.hit = .055; burst(e.x, e.y - 10, [255, 220, 170], 6);
  fx.push({ kind: "flash", x: e.x, y: e.y - 14, age: 0, life: .12, r: 38 }); fx.push({ kind: "ring", x: e.x, y: e.y - 14, age: 0, life: .22, a: Math.random() * TAU });
  e.hurt = .24;
  if (e.hp <= 0) { e.state = "dying"; e.t = 0; e.vx *= .5; burst(e.x, e.y - 10, [255, 150, 70], 16); dust(e.x, e.y, 6, [60, 40, 50]); for (let i = 0; i < 3; i++) fx.push({ kind: "wisp", x: e.x, y: e.y - 12, c, age: -i * .08, life: .7, vx: (Math.random() - .5) * 200, vy: -160 - Math.random() * 80 }); if (c.spare && !c.spare.dead && c.spare.state !== "dying") { c.enemy = c.spare; c.spare = null; c.cd = .3; } else { c.enemy = null; c.sub = "flourish"; c.t = .8 + c.seed() * .7; c.q = 1; } }
}
function behave(c, dt) {
  const st = c.st; c.t -= dt; c.cd -= dt;
  if (st.plat && c.ground && c.y > st.y + 6 && c.mode !== "travel" && c.mode !== "hop" && c.mode !== "climb") { relocate(c); return; }
  switch (c.mode) {
    case "travel": {
      if (c.plat && c.plat !== st.plat) c.plat = null; c.swordTo = 0;
      if (runTo(c, st.x, 300, dt) && c.ground) {
        if (st.plat && c.y > st.plat.y + 2) { c.plat = st.plat; jump(c, -Math.sqrt(2 * G * (-st.plat.y + 22))); c.mode = "hop"; }
        else begin(c);
      }
      break;
    }
    case "hop": if (c.ground) begin(c); break;
    case "roam": {
      if (c.ground && c.cd <= 0 && c.slash <= 0) for (const w of wildList) { if (w.cool > 0 || w.fade < 1 || w.skin === 1 || w.skin === 3 || Math.abs(w.y - c.y) > 8 || Math.abs(w.x - c.x) > 46) continue; c.face = sign(w.x - c.x); c.swordTo = 1; slash(c, "fwd"); c.cd = 2.6; c.sub = "flick"; c.t = .7; w.hurt = .28; w.flash = .12; w.flee = 1.3; w.face = c.face; c.hit = .05; burst(w.x, w.y - 12, [255, 220, 170], 6); fx.push({ kind: "ring", x: w.x, y: w.y - 12, age: 0, life: .22, a: Math.random() * TAU }); break; }
      if (c.sub === "start" || (c.sub === "pause" && c.t <= 0)) {
        const pick = c.seed();
        if (pick < .16 && c.ground) { c.sub = "flick"; c.t = .7; c.swordTo = 1; slash(c, pick < .08 ? "up" : "fwd"); }
        else { c.sub = "walk"; c.goal = lerp(st.range[0], st.range[1], c.seed()); }
      } else if (c.sub === "walk") { if (runTo(c, c.goal, 58, dt)) { c.sub = "pause"; c.t = 1 + c.seed() * 2.6; c.look = c.seed() < .5 ? 1 : 0; const r = c.seed(); c.pose = r < .22 ? "look" : r < .32 ? "yawn" : r < .4 ? "wave" : ""; c.poseT = c.pose ? Math.max(1.4, c.t - .3) : 0; if (c.pose) { c.t = Math.max(c.t, 1.8); c.look = 0; } } }
      else if (c.sub === "flick") { c.vx = lerp(c.vx, 0, dt * 12); if (c.t <= 0) { c.sub = "pause"; c.t = .6; c.swordTo = 0; } }
      else { c.vx = lerp(c.vx, 0, Math.min(1, dt * 12)); if (c.look && c.t < .9 && c.t + dt >= .9) c.face = -c.face; }
      break;
    }
    case "fight": {
      const lane = st.lane, e = c.enemy; c.swordTo = 1;
      if (c.sub === "start") { if (!runTo(c, lane.cx, 150, dt)) break; c.sub = "idle"; c.t = .2 + c.seed() * .6; }
      if (c.sub === "flourish") { c.vx = lerp(c.vx, 0, dt * 10); if (c.t <= 0) { c.sub = "idle"; c.t = .2 + c.seed() * .7; c.q = 0; } break; }
      if (c.sub === "idle") { runTo(c, lane.cx, 90, dt); if (c.t <= 0) { c.enemy = spawnEnemy(c); if (!c.spare || c.spare.dead) c.spare = spawnEnemy(c, c.enemy.x < lane.cx ? 1 : -1); c.sub = "engage"; } break; }
      if (!e || e.dead || e.state === "dying") { c.enemy = null; c.sub = "idle"; c.t = .6; break; }
      if (c.dash > 0) break;
      const dx = e.x - c.x, ad = Math.abs(dx);
      if (c.ground) c.face = sign(dx);
      if (e.type === "crawler" && e.state === "approach" && ad > 90) { runTo(c, lane.cx, 110, dt); break; }
      if (e.type === "crawler") {
        if (!c.ground) {
          if (ad < 20 && c.vy > 40 && c.y > st.y - 64 && c.cd <= 0) { c.vy = -470; c.cd = .35; attack(c, e, "down", 0, 0); }
          else c.vx = lerp(c.vx, clamp(dx * 4, -210, 210), dt * 6);
        } else if (e.state === "lunge" && ad < 96 && archer(c)) { if (c.dash <= 0 && c.cd < .3) { c.dash = .15; c.vx = -sign(dx) * 430; dust(c.x, c.y, 3); c.cd = .35; } }
        else if (e.state === "lunge" && ad < 96) { jump(c, -560); c.vx = sign(dx) * 150; }
        else if (ad > reach(c) + 8 || (archer(c) && ad < 60 && Math.abs(c.x - lane.cx) < lane.hw - 12)) runTo(c, clamp(e.x - sign(dx) * reach(c), lane.cx - lane.hw - 6, lane.cx + lane.hw + 6), 160, dt);
        else {
          c.vx = lerp(c.vx, 0, dt * 14);
          if (c.cd <= 0 && e.state !== "emerge" && e.state !== "approach") {
            c.cd = (archer(c) ? .7 : .4) + c.seed() * .22; c.step++; attack(c, e, "fwd", c.face * 150, 0);
            if (c.enemy && c.step % 3 === 2 && !archer(c)) { c.dash = .15; c.vx = -c.face * 430; c.cd = .5; dust(c.x, c.y, 3); }
          }
        }
      } else {
        if (e.state === "approach") { runTo(c, lane.cx, 110, dt); break; }
        if (archer(c)) { if (ad > 150) runTo(c, clamp(e.x - sign(dx) * 110, lane.cx - lane.hw, lane.cx + lane.hw), 160, dt); else { c.vx = lerp(c.vx, 0, dt * 12); if (c.cd <= 0 && c.ground) { c.cd = .8 + c.seed() * .3; attack(c, e, ad < 30 ? "up" : "fwd", sign(dx || c.face) * 120, -90); } } break; }
        if (!c.ground) { if (c.cd <= 0 && Math.hypot(dx, e.y - (c.y - 30)) < 48 + reach(c) - 34) { const up = Math.abs(dx) < 16; c.cd = .5; attack(c, e, up ? "up" : "fwd", sign(dx || c.face) * 150, -120); c.vy = Math.min(c.vy, -120); } }
        else if (ad > 26) runTo(c, clamp(e.x - sign(dx) * 16, lane.cx - lane.hw, lane.cx + lane.hw), 160, dt);
        else { c.vx = lerp(c.vx, 0, dt * 12); if (c.cd <= 0) { jump(c, -Math.sqrt(2 * G * clamp(st.y - e.y - 26, 30, 110))); c.vx = sign(dx) * 40; c.cd = .12; } }
      }
      break;
    }
    case "drill": {
      const bow = archer(c), home = bow ? st.dummyX - 96 : st.x, d = props.get(st.dummy), SEQ = bow ? ["shoot", "shoot", "shoot", "rest"] : ["slash", "slash", "up", "back", "in", "slash", "slash", "rest"]; c.swordTo = 1;
      if (c.sub === "start") { if (runTo(c, home, 140, dt)) { c.sub = "seq"; c.step = 0; c.t = .5; c.face = 1; } break; }
      if (c.sub === "leap") {
        if (!c.ground && c.vy > 0 && c.y > st.y - 66 && Math.abs(c.x - st.dummyX) < 22 && c.cd <= 0) { slash(c, "down"); c.vy = -340; c.vx = -120; c.cd = 1; if (d) d.va -= 5; burst(st.dummyX, st.y - 40, [240, 220, 170], 5); c.hit = .05; }
        if (c.ground && c.cd > 0) { c.sub = "seq"; c.t = .5; }
        break;
      }
      if (c.dash > 0) break;
      if (c.t > 0) { if (c.step % SEQ.length === 0 || SEQ[(c.step - 1) % SEQ.length] === "rest") runTo(c, home, 90, dt), c.face = Math.abs(c.x - home) < 4 ? 1 : c.face; else c.vx = lerp(c.vx, 0, dt * 14); break; }
      const act = SEQ[c.step++ % SEQ.length]; c.face = 1;
      if (act === "shoot") { c.slash = .0001; c.slashKind = "fwd"; fx.push({ kind: "arrow", x: c.x + 14, y: c.y - 30, tx: st.dummyX - 4, ty: st.y - 36, dummy: d, age: 0, life: 1 }); c.t = .75; }
      else if (act === "slash") { if (Math.abs(c.x - home) < 22) { slash(c, "fwd"); if (d) d.va += 7 + c.seed() * 3; burst(st.dummyX - 6, st.y - 36, [240, 220, 170], 4); c.hit = .04; } c.t = .36; }
      else if (act === "up") { slash(c, "up"); c.t = .5; }
      else if (act === "back") { c.dash = .15; c.vx = -430; c.t = .5; dust(c.x, c.y, 3); }
      else if (act === "in") { c.dash = .15; c.vx = 430; c.t = .2; dust(c.x, c.y, 3); }
      else if (act === "leap") { jump(c, -575); c.vx = (st.dummyX - c.x) / .33; c.sub = "leap"; c.cd = 0; }
      else { c.swordTo = 2; c.t = 1.3 + c.seed() * 1.4; }
      break;
    }
    case "climb": {
      const s = st.shaft, L = s.left + 9, R = s.right - 9; c.plat = { x0: s.right, x1: s.right + 46, y: s.top }; c.swordTo = 0;
      if (c.sub === "start" || c.sub === "base") { if (runTo(c, s.left + 34, 120, dt) && c.ground && c.t <= 0) { jump(c, -590); c.vx = -230; c.face = -1; c.sub = "air"; } }
      else if (c.sub === "air") {
        if (c.x <= L && c.x > L - 14 && c.vx < 0) { c.x = L; c.sub = "cling"; c.face = 1; c.t = .2 + c.seed() * .2; c.vx = 0; c.vy *= .25; squash(c, -2); dust(L - 6, c.y - 20, 2); }
        else if (c.x >= R && c.x < R + 14 && c.vx > 0 && c.y > s.top + 4) { c.x = R; c.sub = "cling"; c.face = -1; c.t = .2 + c.seed() * .2; c.vx = 0; c.vy *= .25; squash(c, -2); dust(R + 6, c.y - 20, 2); }
        else if (c.ground && c.y <= s.top + 1) { c.sub = "top"; c.t = 1.6 + c.seed() * 2; c.vx = 0; }
        else if (c.ground) { c.sub = "base"; c.t = .5; }
      } else if (c.sub === "vault") { if (c.ground) { if (c.y <= s.top + 1) { c.sub = "top"; c.t = 1.6 + c.seed() * 2; c.vx = 0; } else { c.sub = "base"; c.t = .5; } }
      } else if (c.sub === "cling") {
        c.vy = lerp(c.vy, 46, dt * 8); c.vx = 0; c.ground = false;
        if (Math.random() < dt * 9) dust(c.x - c.face * 8, c.y - 6, 1);
        if (c.y >= -2) { c.sub = "base"; c.t = .4; }
        else if (c.t <= 0) { const top = c.face === 1 && c.y < s.top + 86; if (top) { const rise = c.y - s.top + 34, vy = Math.sqrt(2 * G * rise), time = vy / G + Math.sqrt(2 * 34 / G); c.vy = -vy; c.vx = (s.right + 22 - c.x) / time; c.sub = "vault"; } else { c.vx = c.face * 262; c.vy = -520; c.sub = "air"; } c.kick = .16; squash(c, 3); }
      } else if (c.sub === "top") {
        if (c.t > .5) { c.vx = lerp(c.vx, 0, dt * 10); if (c.t < 1.2 && c.t + dt >= 1.2) c.face = -c.face; }
        else { c.face = -1; c.vx = -80; if (!c.ground) { c.sub = "fall"; c.vx = -30; } }
      } else if (c.sub === "fall") { if (c.ground) { c.sub = "base"; c.t = .7 + c.seed() * 1.5; c.vx = 0; } }
      break;
    }
    case "ponder": {
      c.vx = lerp(c.vx, 0, Math.min(1, dt * 12));
      if (c.sub === "start") { if (!runTo(c, st.x, 140, dt)) break; c.sub = "think"; c.t = 0; c.q = 0; }
      c.q = Math.min(1, c.q + dt * 4);
      if (c.sub === "walk") { if (runTo(c, c.goal, 46, dt)) { c.sub = "think"; c.t = .5; } break; }
      if (c.t <= 0) {
        const k = c.step++ % 6; c.arm = 0; c.tiltTo = 0;
        if (k === 0) { c.face = -c.face; c.t = .7; }
        else if (k === 1) { c.tiltTo = .34 * c.face; c.t = 1.5; }
        else if (k === 2) { c.arm = 1; c.tiltTo = -.18 * c.face; c.t = 1.4; }
        else if (k === 3) { c.face = -c.face; c.t = .6; }
        else if (k === 4) { squash(c, 3); c.q = .3; c.t = 1.1; c.arm = 2; }
        else { c.sub = "walk"; c.goal = st.x + (c.seed() - .5) * 22; }
      }
      break;
    }
    case "rest": if (c.sub === "start") { if (runTo(c, st.x, 110, dt)) { c.sub = "sit"; c.face = st.face || 1; } } else { c.vx = 0; c.sit = Math.min(1, c.sit + dt * 3); } break;
    default: c.vx = 0;
  }
  if (c.mode !== "rest") c.sit = Math.max(0, c.sit - dt * 5);
}
function physics(c, dt) {
  if (c.hit > 0) { c.hit -= dt; return; }
  if (c.dash > 0) { c.dash -= dt; c.vy = 0; if (c.ghosts.length < 6) c.ghosts.push({ x: c.x, y: c.y, face: c.face, a: .5 }); if (c.dash <= 0) c.vx *= .25; }
  else if (!(c.mode === "climb" && c.sub === "cling")) c.vy += G * dt;
  const prevY = c.y; c.x += c.vx * dt; c.y += c.vy * dt;
  if (c.plat && c.st.plat === c.plat && c.mode !== "travel" && c.mode !== "hop" && c.mode !== "climb") c.x = clamp(c.x, c.plat.x0 + 8, c.plat.x1 - 8);
  let floor = 0; const p = c.plat;
  if (p && c.x >= p.x0 && c.x <= p.x1 && prevY <= p.y + .5) floor = p.y;
  if (c.vy >= 0 && c.y >= floor) { if (!c.ground) { squash(c, -Math.min(5, c.vy / 150)); if (c.vy > 240) { dust(c.x, floor, 4); c.land = .13; } } c.y = floor; c.vy = 0; c.ground = true; }
  else if (c.y < floor - .5) c.ground = false;
  // springs
  c.sqv += (-170 * c.sq - 15 * c.sqv) * dt; c.sq += c.sqv * dt; c.sq = clamp(c.sq, -.32, .32);
  const hemTo = clamp(-c.vx * .04, -11, 11); c.hemv += ((hemTo - c.hem) * 120 - c.hemv * 12) * dt; c.hem += c.hemv * dt;
  c.tilt = lerp(c.tilt, c.tiltTo, Math.min(1, dt * 7));
  c.sword = lerp(c.sword, c.swordTo, Math.min(1, dt * 10));
  if (c.slash > 0) { c.slash += dt / .27; if (c.slash >= 1) c.slash = 0; }
  if (c.ground && Math.abs(c.vx) > 12) c.run += Math.abs(c.vx) * dt * .085; else c.run = lerp(c.run, Math.round(c.run / Math.PI) * Math.PI, Math.min(1, dt * 10));
  c.land -= dt; c.kick = (c.kick || 0) - dt; c.poseT = (c.poseT || 0) - dt; c.blink -= dt; if (c.blink < -.12) c.blink = 1.5 + Math.random() * 4;
  c.alpha = Math.min(1, c.alpha + dt * 4);
  for (const g of c.ghosts) g.a -= dt * 2.6; c.ghosts = c.ghosts.filter(g => g.a > 0);
  // scarf: a short verlet ribbon pinned at the neck
  const s = c.scarf, nx = c.x - c.face * 3, ny = c.y - 31 + c.sit * 9 + c.sq * 20;
  s[0].x = nx; s[0].y = ny;
  for (let i = 1; i < s.length; i++) { const q = s[i], vx = (q.x - q.px) * .9, vy = (q.y - q.py) * .9; q.px = q.x; q.py = q.y; q.x += vx + Math.sin(now * 2.3 + i + c.x * .01) * 5 * dt - c.face * 14 * dt; q.y += vy + 150 * dt * dt * 6; }
  for (let k = 0; k < 2; k++) for (let i = 1; i < s.length; i++) { const a = s[i - 1], b = s[i], dx = b.x - a.x, dy = b.y - a.y, d = Math.hypot(dx, dy) || 1, f = (d - 5.2) / d; b.x -= dx * f; b.y -= dy * f; if (b.y > c.y - 1 && c.ground) b.y = c.y - 1; }
}
function updateEnemy(e, dt) {
  e.t += dt; e.flash -= dt; e.next -= dt; e.hurt = (e.hurt || 0) - dt;
  const lane = e.lane;
  if (e.state === "dying") { e.vx *= 1 - dt * 5; e.x += e.vx * dt; if (e.t > .5) e.dead = true; return; }
  if (e.state === "emerge") { if (e.t > .55) { e.state = "roam"; e.t = 0; } return; }
  if (e.state === "approach") { const dx = e.tx - e.x, dy = e.ty - e.y, d = Math.hypot(dx, dy); if (d < 6 || e.t > 6) { e.state = "roam"; e.t = 0; e.y = e.ty; } else { const v = Math.min(d, 170 * dt); e.x += dx / d * v; e.y += dy / d * v; e.face = sign(dx || e.face); } return; }
  if (e.type === "crawler") {
    if (e.state === "roam") { e.vx = lerp(e.vx, e.face * 26, dt * 3); if (e.next <= 0) { e.state = "tele"; e.t = 0; } }
    else if (e.state === "tele") { e.vx = lerp(e.vx, 0, dt * 10); if (e.t > .5) { e.state = "lunge"; e.t = 0; e.vx = e.face * 300; dust(e.x, e.base, 4, [90, 70, 60]); } }
    else if (e.state === "lunge") { if (e.t > .42) { e.state = "roam"; e.next = 2 + Math.random() * 2.5; } }
    e.x += e.vx * dt;
  } else {
    e.vx = lerp(e.vx, e.face * 30, dt * 2); e.vy = lerp(e.vy, 0, dt * 3); e.x += e.vx * dt; e.y += e.vy * dt;
    e.y = lerp(e.y, e.base - 80 + Math.sin(now * 2.2 + e.phase) * 12, dt * 2.2);
  }
  if (e.x < lane.cx - lane.hw) { e.x = lane.cx - lane.hw; e.face = 1; if (e.state === "lunge") e.state = "roam"; }
  if (e.x > lane.cx + lane.hw) { e.x = lane.cx + lane.hw; e.face = -1; if (e.state === "lunge") e.state = "roam"; }
}
function faceEnemy(c) { const e = c.enemy; if (e && !e.dead && e.state !== "dying" && e.state !== "lunge" && e.state !== "tele") e.face = sign(c.x - e.x); }

function update(dt) {
  for (const c of chars.values()) {
    if (!live(c.row)) continue;
    behave(c, dt); physics(c, dt); faceEnemy(c);
  }
  for (const e of enemies) updateEnemy(e, dt);
  for (const e of wildList) { e.cool -= dt; e.hurt -= dt; e.flash -= dt; e.flee -= dt; if (e.cool > 0) continue; e.fade = Math.min(1, e.fade + dt * 1.2); e.x += e.face * e.speed * (e.flee > 0 ? 6 : 1) * dt; if (e.x < e.a) { e.x = e.a; e.face = 1; } else if (e.x > e.b) { e.x = e.b; e.face = -1; } e.y = e.skin === 1 ? e.home + Math.sin(now * 1.3 + e.phase) * 22 : e.skin === 3 ? e.home + Math.sin(now * .7 + e.phase) * 34 : e.home; }
  enemies = enemies.filter(e => !e.dead);
  for (const d of props.values()) { d.va += (-90 * d.a - 5 * d.va) * dt; d.a += d.va * dt; }
  for (const f of fx) {
    f.age += dt;
    if (f.kind === "dust") { f.x += f.vx * dt; f.y += f.vy * dt; f.vx *= 1 - dt * 3; }
    else if (f.kind === "spark") { f.x += f.vx * dt; f.y += f.vy * dt; f.vy += 420 * dt; f.vx *= 1 - dt * 2; }
    else if (f.kind === "arrow") { const tx = f.e ? f.e.x : f.tx, ty = f.e ? f.e.y - 14 : f.ty, dx = tx - f.x, dy = ty - f.y, d = Math.hypot(dx, dy) || 1, v = 560 * dt; f.dx = dx / d; f.dy = dy / d; if (f.e && (f.e.dead || f.e.state === "dying")) f.age = f.life; else if (d <= v + 8) { f.age = f.life; if (f.e) strike(f.c, f.e, f.kx, f.ky); else { if (f.dummy) f.dummy.va += 6; burst(tx, ty, [240, 220, 170], 4); } } else { f.x += f.dx * v; f.y += f.dy * v; } }
    else if (f.kind === "wisp" && f.age > 0) { const k = clamp(f.age / f.life, 0, 1), tx = f.c.x, ty = f.c.y - 30; f.vx = lerp(f.vx, (tx - f.x) * 9, k * k); f.vy = lerp(f.vy, (ty - f.y) * 9, k * k); f.x += f.vx * dt; f.y += f.vy * dt; }
  }
  fx = fx.filter(f => f.age < f.life);
  cam += (camTarget - cam) * Math.min(1, dt * 6);
}

/* ---------- drawing ---------- */
const X = wx => (wx - cam) * scale + W / 2, Y = wy => groundY + wy * scale;
function biomeAt(wx) { for (const c of chambers) if (wx < c.x1 + 45) return c.biome; return chambers.length ? chambers[chambers.length - 1].biome : BIOMES[0]; }
function chamberAt(wx) { for (const c of chambers) if (wx < c.x1 + 45) return c; return chambers[chambers.length - 1] || null; }
let mix = {};
function drawBackdrop() {
  const b = biomeAt(cam), grad = ctx.createLinearGradient(0, 0, 0, H);
  grad.addColorStop(0, b.sky[0]); grad.addColorStop(.55, b.sky[1]); grad.addColorStop(1, b.sky[2]);
  ctx.fillStyle = grad; ctx.fillRect(0, 0, W, H);
  // Painted backdrop: mirrored tiling, slow parallax, cross-faded between neighbouring chambers.
  const here = chamberAt(cam); if (!here) return;
  const layers = [[here.biome.key, 1]], edge = 260, next = chambers[here.index + 1], prev = chambers[here.index - 1];
  if (next && cam > here.x1 + 45 - edge) layers.push([next.biome.key, clamp((cam - (here.x1 + 45 - edge)) / (edge * 2), 0, .5)]);
  if (prev && cam < here.x0 - 45 + edge) layers.push([prev.biome.key, clamp(((here.x0 - 45 + edge) - cam) / (edge * 2), 0, .5)]);
  mix = {}; mix[here.biome.key] = 1 - (layers[1] ? layers[1][1] : 0); if (layers[1]) mix[layers[1][0]] = (mix[layers[1][0]] || 0) + layers[1][1];
  for (const [key, alpha] of layers) {
    const img = images[key]; if (!img) continue;
    const h = H * 1.04, wd = img.width * h / img.height, off = cam * scale * .16, first = Math.floor(off / wd);
    ctx.globalAlpha = alpha;
    for (let i = first; i * wd - off < W; i++) {
      const dx = i * wd - off;
      ctx.drawImage(img, dx, -H * .02, wd + 1, h);
    }
    ctx.globalAlpha = 1;
  }
}
function drawSilhouettes(par, layerName, alpha, heightK, seedBase) {
  const color = biomeAt(cam)[layerName];
  const cell = 240 * scale, off = cam * scale * par, first = Math.floor((off - W / 2) / cell) - 1, last = Math.floor((off + W / 2) / cell) + 1;
  ctx.fillStyle = color; ctx.globalAlpha = alpha;
  for (let k = first; k <= last; k++) {
    const r0 = rng(seedBase + k * 7919), x = k * cell - off + W / 2 + r0() * cell * .5, kind = r0();
    let r = r0;
    // hanging rock
    const sw = (26 + r() * 60) * scale, sh = (60 + r() * 190) * heightK * scale;
    ctx.beginPath(); ctx.moveTo(x - sw, 0); ctx.quadraticCurveTo(x - sw * .2, sh * .35, x + (r() - .5) * sw * .4, sh); ctx.quadraticCurveTo(x + sw * .3, sh * .3, x + sw, 0); ctx.fill();
    const bx = x + cell * (.3 + r() * .4), base = groundY + 30 * scale, seedCell = seedBase * 31 + k * 104729;
    for (const biome of Object.keys(mix)) { if (mix[biome] <= .01) continue; r = rng(seedCell); ctx.globalAlpha = alpha * mix[biome]; ctx.fillStyle = BIOMES.find(b => b.key === biome)[layerName];
    if (biome === "ruins") {
      if (kind < .6) { const pw = (16 + r() * 16) * scale, ph = (150 + r() * 240) * heightK * scale, broken = r() < .5; ctx.beginPath(); ctx.moveTo(bx - pw, base); ctx.lineTo(bx - pw * .8, base - ph); if (broken) { ctx.lineTo(bx - pw * .2, base - ph - 14 * scale); ctx.lineTo(bx + pw * .3, base - ph + 8 * scale); } else { ctx.lineTo(bx - pw * 1.5, base - ph - 8 * scale); ctx.lineTo(bx - pw * 1.5, base - ph - 20 * scale); ctx.lineTo(bx + pw * 1.5, base - ph - 20 * scale); ctx.lineTo(bx + pw * 1.5, base - ph - 8 * scale); } ctx.lineTo(bx + pw * .8, base - ph); ctx.lineTo(bx + pw, base); ctx.fill(); }
      // chain
      const cx = x + sw * 1.6, cl = (90 + r() * 160) * heightK * scale; ctx.fillRect(cx, 0, 1.6 * scale, cl); ctx.beginPath(); ctx.arc(cx + .8 * scale, cl, 4 * scale, 0, TAU); ctx.fill();
    } else if (biome === "grotto") {
      for (let v = 0; v < 3; v++) { const vx = x + (v - 1) * sw * (1 + r()), vl = (110 + r() * 220) * heightK * scale, sway = Math.sin(now * .5 + k + v) * 6 * scale * par; ctx.beginPath(); ctx.moveTo(vx - 2 * scale, 0); ctx.quadraticCurveTo(vx + sway, vl * .6, vx + sway * 1.6, vl); ctx.quadraticCurveTo(vx + sway + 3 * scale, vl * .6, vx + 2 * scale, 0); ctx.fill(); for (let l = 1; l < 5; l++) { const ly = vl * l / 5, lx = vx + sway * (l / 5) * 1.3; ctx.beginPath(); ctx.ellipse(lx + (l % 2 ? 6 : -6) * scale, ly, 7 * scale, 3 * scale, l % 2 ? .5 : -.5, 0, TAU); ctx.fill(); } }
      const fh = (60 + r() * 120) * heightK * scale; for (let f = -3; f <= 3; f++) { ctx.beginPath(); ctx.moveTo(bx - 3 * scale, base); ctx.quadraticCurveTo(bx + f * 12 * scale, base - fh * .8, bx + f * 26 * scale, base - fh * (1 - Math.abs(f) * .16)); ctx.quadraticCurveTo(bx + f * 9 * scale, base - fh * .5, bx + 3 * scale, base); ctx.fill(); }
    } else {
      for (let s = 0; s < 4; s++) { const cx = bx + (s - 1.5) * 20 * scale, chh = (50 + r() * 150) * heightK * scale, cw = (8 + r() * 9) * scale, tiltX = (s - 1.5) * 12 * scale; ctx.beginPath(); ctx.moveTo(cx - cw, base); ctx.lineTo(cx - cw * .7 + tiltX, base - chh * .82); ctx.lineTo(cx + tiltX, base - chh); ctx.lineTo(cx + cw * .8 + tiltX, base - chh * .8); ctx.lineTo(cx + cw, base); ctx.fill(); }
    }
    }
    ctx.globalAlpha = alpha; ctx.fillStyle = color;
  }
  ctx.globalAlpha = 1;
}
function drawCrystalGlow(par) {
  const cell = 240 * scale, off = cam * scale * par, first = Math.floor((off - W / 2) / cell) - 1, last = Math.floor((off + W / 2) / cell) + 1;
  ctx.globalCompositeOperation = "lighter";
  for (let k = first; k <= last; k++) { if (!mix.crystal) continue; const r = rng(911 + k * 7919), x = k * cell - off + W / 2 + r() * cell * .5 + cell * .45; drawGlow(x, groundY - 40 * scale, 120 * scale, [230, 120, 220], (.16 + Math.sin(now * .8 + k) * .04) * mix.crystal); }
  ctx.globalCompositeOperation = "source-over";
}
function drawShafts() {
  ctx.globalCompositeOperation = "lighter";
  const cell = 520 * scale, par = .55, off = cam * scale * par, first = Math.floor((off - W / 2) / cell) - 1, last = Math.floor((off + W / 2) / cell) + 1;
  for (let k = first; k <= last; k++) {
    const r = rng(4242 + k * 131), x = k * cell - off + W / 2 + r() * cell * .6, wd = (50 + r() * 90) * scale, lean = (r() - .3) * 240 * scale, b = biomeAt(k * 520 / par), a = .05 + .035 * Math.sin(now * .35 + k * 1.7);
    const g = ctx.createLinearGradient(x, 0, x + lean, groundY); g.addColorStop(0, rgba(b.fog, a * 1.8)); g.addColorStop(1, rgba(b.fog, 0));
    ctx.fillStyle = g; ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x + wd, 0); ctx.lineTo(x + wd * 2.2 + lean, groundY + 20); ctx.lineTo(x + lean - wd * .6, groundY + 20); ctx.fill();
  }
  ctx.globalCompositeOperation = "source-over";
}
function drawFog(par, yK, alpha) {
  const b = biomeAt(cam), cell = 300 * scale, off = cam * scale * par + now * 9 * scale * par, first = Math.floor((off - W / 2) / cell) - 1, last = Math.floor((off + W / 2) / cell) + 1, g = glow(b.fog);
  ctx.globalCompositeOperation = "lighter";
  for (let k = first; k <= last; k++) { const r = rng(77 + k * 31 + Math.round(par * 100)), x = k * cell - off + W / 2, rw = (240 + r() * 200) * scale, rh = (60 + r() * 60) * scale, y = groundY + (yK + (r() - .5) * 50) * scale; ctx.globalAlpha = alpha * (.6 + r() * .4); ctx.drawImage(g, x - rw, y - rh, rw * 2, rh * 2); }
  ctx.globalAlpha = 1; ctx.globalCompositeOperation = "source-over";
}
function drawLamp(wx, tall, color, pulse) {
  const x = X(wx), base = groundY, h = tall * scale, flick = .82 + .1 * Math.sin(now * 5.1 + wx) + .08 * Math.sin(now * 12.7 + wx * .3);
  ctx.strokeStyle = "#05080d"; ctx.lineWidth = 3.5 * scale; ctx.lineCap = "round";
  ctx.beginPath(); ctx.moveTo(x, base + 4); ctx.lineTo(x, base - h + 16 * scale); ctx.quadraticCurveTo(x, base - h, x + 14 * scale, base - h); ctx.stroke();
  const lx = x + 14 * scale, ly = base - h + 13 * scale;
  ctx.lineWidth = 1.4 * scale; ctx.beginPath(); ctx.moveTo(lx, base - h); ctx.lineTo(lx, ly - 8 * scale); ctx.stroke();
  ctx.globalCompositeOperation = "lighter"; drawGlow(lx, ly, (70 + pulse * 26) * scale * flick, color, .5 + pulse * .25); drawGlow(lx, ly, 20 * scale, [255, 255, 255], .8); ctx.globalCompositeOperation = "source-over";
  ctx.fillStyle = "#05080d"; ctx.beginPath(); ctx.moveTo(lx - 6 * scale, ly - 8 * scale); ctx.lineTo(lx + 6 * scale, ly - 8 * scale); ctx.lineTo(lx + 4 * scale, ly - 5 * scale); ctx.lineTo(lx - 4 * scale, ly - 5 * scale); ctx.fill();
  ctx.fillStyle = rgba(color, .95); ctx.beginPath(); ctx.ellipse(lx, ly, 4.6 * scale, 6.5 * scale, 0, 0, TAU); ctx.fill();
  ctx.fillStyle = "#05080d"; ctx.fillRect(lx - 4 * scale, ly + 6 * scale, 8 * scale, 2.5 * scale);
}
function drawGround() {
  const left = cam - W / scale / 2 - 40, right = cam + W / scale / 2 + 40;
  for (const ch of chambers) {
    if (ch.x1 < left || ch.x0 > right) continue;
    const b = ch.biome;
    // dividing rock between chambers
    if (ch.index > 0) { const x = X(ch.x0 - 45), r = rng(ch.index * 97), wT = (70 + r() * 30) * scale, wB = (58 + r() * 24) * scale, wM = (20 + r() * 10) * scale, my = groundY * (.42 + r() * .16), lean = (r() - .5) * 30 * scale; ctx.fillStyle = "#04060a"; ctx.beginPath(); ctx.moveTo(x - wT, -2); ctx.bezierCurveTo(x - wT * .5, my * .5, x - wM + lean, my * .7, x - wM + lean, my); ctx.bezierCurveTo(x - wM + lean, my + (groundY - my) * .5, x - wB * .7, groundY - 30 * scale, x - wB, groundY + 6); ctx.lineTo(x + wB, groundY + 6); ctx.bezierCurveTo(x + wB * .6, groundY - 40 * scale, x + wM + lean, my + (groundY - my) * .4, x + wM + lean, my); ctx.bezierCurveTo(x + wM + lean, my * .6, x + wT * .6, my * .4, x + wT, -2); ctx.fill(); ctx.strokeStyle = rgba(b.fog, .22); ctx.lineWidth = 1.4 * scale; ctx.beginPath(); ctx.moveTo(x + wT, -2); ctx.bezierCurveTo(x + wT * .6, my * .4, x + wM + lean, my * .6, x + wM + lean, my); ctx.bezierCurveTo(x + wM + lean, my + (groundY - my) * .4, x + wB * .6, groundY - 40 * scale, x + wB, groundY + 2); ctx.stroke(); }
    // sign post with the Project name
    const sx = X(ch.sign); ctx.strokeStyle = "#04070b"; ctx.lineWidth = 4 * scale; ctx.lineCap = "round"; ctx.beginPath(); ctx.moveTo(sx, groundY + 3); ctx.lineTo(sx + 2 * scale, groundY - 66 * scale); ctx.stroke();
    ctx.font = "600 " + Math.max(10, 11 * scale) + "px Georgia,'Songti TC','Noto Serif TC',serif"; const label = clip(ch.label, 16), tw = ctx.measureText(label).width + 16 * scale;
    ctx.save(); ctx.translate(sx + 2 * scale, groundY - 62 * scale); ctx.rotate(-.04); ctx.fillStyle = "#070b12"; ctx.strokeStyle = rgba(b.fog, .5); ctx.lineWidth = 1; ctx.beginPath(); ctx.moveTo(-tw / 2, -11 * scale); ctx.lineTo(tw / 2 + 8 * scale, -11 * scale); ctx.lineTo(tw / 2 + 15 * scale, 0); ctx.lineTo(tw / 2 + 8 * scale, 11 * scale); ctx.lineTo(-tw / 2, 11 * scale); ctx.closePath(); ctx.fill(); ctx.stroke(); ctx.fillStyle = rgba(b.lamp, .92); ctx.textAlign = "center"; ctx.textBaseline = "middle"; ctx.fillText(label, 4 * scale, 1); ctx.restore();
    for (const pc of ch.pieces) { const img = images[pc.kind]; if (!img) continue; const ht = ({ gate: 250, shell: 130, mushrooms: 120 })[pc.kind] * pc.size * scale, wd = ht * img.width / img.height, px = X(pc.x); ctx.save(); ctx.translate(px, groundY + ht * .04); if (pc.flip) ctx.scale(-1, 1); ctx.globalAlpha = .92; ctx.drawImage(img, -wd / 2, -ht, wd, ht); ctx.restore(); if (pc.kind === "mushrooms") { ctx.globalCompositeOperation = "lighter"; drawGlow(px, groundY - ht * .7, ht * .9, [150, 255, 160], .22 + .05 * Math.sin(now * 1.3 + pc.x)); ctx.globalCompositeOperation = "source-over"; } }
    for (const l of ch.lamps) drawLamp(l, 104, b.lamp, 0);
    if (ch.beacon !== null) drawLamp(ch.beacon, 176, [255, 170, 120], .5 + .5 * Math.sin(now * 2.4));
    for (const cp of ch.camps) { if (!cp.y) { drawCampProp("tent", cp.x - 96, 0, 92, false); drawCampProp("logpile", cp.x + 92, 0, 22, false); drawCampProp("pot", cp.x + 62, 0, 40, true); } for (const bed of cp.beds) drawCampProp("bedroll", bed.x, cp.y, 13, bed.flip); drawCamp(cp.x, cp.y, b); }
    for (const s of ch.shafts) drawShaft(s, b);
    for (const d of ch.dummies) drawDummy(d, b);
    for (const p of ch.platforms) drawSlab(p, b);
  }
  // the floor itself
  const g = ctx.createLinearGradient(0, groundY, 0, H); g.addColorStop(0, "#05080d"); g.addColorStop(1, "#010203"); ctx.fillStyle = g;
  ctx.beginPath(); ctx.moveTo(0, H); ctx.lineTo(0, groundY);
  const stepW = 22, first = Math.floor(left / stepW); const pts = [];
  for (let i = first; i * stepW < right + stepW; i++) { const r = rng(i * 53 + 5), wx = i * stepW, y = groundY + (r() * 3 - 1.2) * scale; pts.push([X(wx), y, wx, r()]); ctx.lineTo(X(wx), y); }
  ctx.lineTo(W, H); ctx.fill();
  ctx.lineWidth = 1.6 * scale; ctx.lineJoin = "round";
  for (let i = 1; i < pts.length; i++) { const b = biomeAt(pts[i][2]); ctx.strokeStyle = rgba(b.fog, .34); ctx.beginPath(); ctx.moveTo(pts[i - 1][0], pts[i - 1][1]); ctx.lineTo(pts[i][0], pts[i][1]); ctx.stroke(); }
  // tufts, pebbles and small glowing buds along the rim
  for (const p of pts) {
    const b = biomeAt(p[2]);
    if (p[3] < .42) { ctx.strokeStyle = "#05080d"; ctx.lineWidth = 1.6 * scale; const n = 2 + Math.floor(p[3] * 9); for (let k = 0; k < n; k++) { const lean = (k - n / 2) * 2.6 + Math.sin(now * 1.3 + p[2] * .05 + k) * 1.6; ctx.beginPath(); ctx.moveTo(p[0] + k * 2 * scale, p[1] + 1); ctx.quadraticCurveTo(p[0] + (k * 2 + lean * .5) * scale, p[1] - 7 * scale, p[0] + (k * 2 + lean) * scale, p[1] - (9 + p[3] * 14) * scale); ctx.stroke(); } }
    else if (p[3] > .93) { const bob = Math.sin(now * 1.1 + p[2]) * 1.5 * scale; ctx.strokeStyle = "#05080d"; ctx.lineWidth = 1.4 * scale; ctx.beginPath(); ctx.moveTo(p[0], p[1]); ctx.quadraticCurveTo(p[0] + 5 * scale, p[1] - 12 * scale, p[0] + 2 * scale, p[1] - 20 * scale + bob); ctx.stroke(); ctx.globalCompositeOperation = "lighter"; drawGlow(p[0] + 2 * scale, p[1] - 21 * scale + bob, 16 * scale, b.mote, .5 + .2 * Math.sin(now * 2 + p[2])); ctx.globalCompositeOperation = "source-over"; ctx.fillStyle = rgba(b.mote, .95); ctx.beginPath(); ctx.arc(p[0] + 2 * scale, p[1] - 21 * scale + bob, 2.4 * scale, 0, TAU); ctx.fill(); }
  }
}
function drawSlab(p, b) {
  const x0 = X(p.x0), x1 = X(p.x1), y = Y(p.y), t = 13 * scale, r = rng(Math.round(p.x0 * 3));
  const art = images["platform-" + p.biome];
  if (p.tier >= 2) { ctx.strokeStyle = "rgba(4,7,11,.85)"; ctx.lineWidth = 1.6 * scale; ctx.setLineDash([5 * scale, 3 * scale]); ctx.beginPath(); ctx.moveTo(x0 + 16 * scale, y); ctx.lineTo(x0 + 26 * scale, -4); ctx.moveTo(x1 - 16 * scale, y); ctx.lineTo(x1 - 26 * scale, -4); ctx.stroke(); ctx.setLineDash([]); }
  if (art) { const wd = x1 - x0 + 16 * scale, ht = wd * art.height / art.width; ctx.drawImage(art, x0 - 8 * scale, y - ht * .035, wd, ht); return; }
  ctx.fillStyle = "#05080d"; ctx.beginPath(); ctx.moveTo(x0, y); ctx.lineTo(x1, y); ctx.quadraticCurveTo(x1 + 5 * scale, y + t * .6, x1 - 14 * scale, y + t); ctx.lineTo((x0 + x1) / 2 + 6 * scale, y + t * 2.1); ctx.lineTo(x0 + 16 * scale, y + t * 1.1); ctx.quadraticCurveTo(x0 - 5 * scale, y + t * .6, x0, y); ctx.fill();
  ctx.strokeStyle = rgba(b.fog, .42); ctx.lineWidth = 1.5 * scale; ctx.beginPath(); ctx.moveTo(x0 + 1, y); ctx.lineTo(x1 - 1, y); ctx.stroke();
  ctx.strokeStyle = "#05080d"; ctx.lineWidth = 1.2 * scale;
  for (let i = 0; i < 4; i++) { const rx = lerp(x0 + 14 * scale, x1 - 14 * scale, r()), len = (14 + r() * 30) * scale, sw = Math.sin(now * .9 + i + p.x0) * 3 * scale; ctx.beginPath(); ctx.moveTo(rx, y + t); ctx.quadraticCurveTo(rx + sw * .4, y + t + len * .6, rx + sw, y + t + len); ctx.stroke(); }
}
function drawShaft(s, b) {
  const base = groundY + 4, top = Y(s.top), pil = images.pillar;
  if (pil) { for (const [inner, y, side] of [[s.left, s.top - 120, -1], [s.right, s.top, 1]]) { const ht = base - Y(y) + 6 * scale, wd = Math.min(ht * pil.width / pil.height, 58 * scale), x = side < 0 ? X(inner) - wd + 5 * scale : X(inner) - 5 * scale; ctx.drawImage(pil, x, Y(y) - 4 * scale, wd, ht); } }
  else   for (const [x0, x1, y] of [[s.left - 34, s.left, s.top - 120], [s.right, s.right + 46, s.top]]) {
    const a = X(x0), z = X(x1), yy = Y(y), r = rng(Math.round(x0 * 7));
    ctx.fillStyle = "#05080d"; ctx.beginPath(); ctx.moveTo(a - 5 * scale, base); ctx.lineTo(a, yy + 6 * scale); ctx.quadraticCurveTo(a, yy, a + 6 * scale, yy); ctx.lineTo(z - 3 * scale, yy); ctx.quadraticCurveTo(z, yy, z, yy + 6 * scale); ctx.lineTo(z + 5 * scale, base); ctx.fill();
    ctx.fillRect(a - 5 * scale, yy - 7 * scale, z - a + 10 * scale, 9 * scale);
    ctx.strokeStyle = rgba(b.fog, .36); ctx.lineWidth = 1.4 * scale; ctx.beginPath(); const inner = x1 === s.left ? z : a; ctx.moveTo(inner, base); ctx.lineTo(inner, yy + 3 * scale); ctx.stroke(); ctx.beginPath(); ctx.moveTo(a - 4 * scale, yy - 7 * scale); ctx.lineTo(z + 4 * scale, yy - 7 * scale); ctx.stroke();
    ctx.strokeStyle = rgba(b.fog, .13); ctx.lineWidth = 1; for (let by = yy + 46 * scale; by < base - 20; by += 58 * scale) { ctx.beginPath(); ctx.moveTo(a + 1, by); ctx.lineTo(z - 1, by + (r() - .5) * 3); ctx.stroke(); }
    ctx.strokeStyle = "#05080d"; ctx.lineWidth = 1.3 * scale; for (let i = 0; i < 3; i++) { const vx = lerp(a, z, r()), len = (18 + r() * 40) * scale, sw = Math.sin(now * .8 + i + x0) * 2.5 * scale; ctx.beginPath(); ctx.moveTo(vx, yy); ctx.quadraticCurveTo(vx + sw * .4, yy + len * .6, vx + sw, yy + len); ctx.stroke(); }
    ctx.strokeStyle = rgba(b.fog, .12); ctx.lineWidth = 1; for (let i = 0; i < 5; i++) { const cy = lerp(yy + 20 * scale, base - 10, r()), cx = lerp(a + 6 * scale, z - 6 * scale, r()); ctx.beginPath(); ctx.moveTo(cx, cy); ctx.lineTo(cx + (r() - .5) * 16 * scale, cy + 12 * scale); ctx.lineTo(cx + (r() - .5) * 20 * scale, cy + 22 * scale); ctx.stroke(); }
  }
  ctx.globalCompositeOperation = "lighter"; drawGlow(X(s.right + 34), top - 16 * scale, 34 * scale, b.lamp, .5); ctx.globalCompositeOperation = "source-over";
  ctx.fillStyle = rgba(b.lamp, .95); ctx.beginPath(); ctx.arc(X(s.right + 34), top - 16 * scale, 3 * scale, 0, TAU); ctx.fill(); ctx.strokeStyle = "#05080d"; ctx.lineWidth = 2 * scale; ctx.beginPath(); ctx.moveTo(X(s.right + 34), top); ctx.lineTo(X(s.right + 34), top - 12 * scale); ctx.stroke();
}
function drawDummy(d, b) {
  const p = props.get(d.id) || { a: 0 }, x = X(d.x), y = Y(d.y);
  ctx.save(); ctx.translate(x, y); ctx.scale(scale, scale); ctx.rotate(clamp(p.a, -1, 1) * .12);
  if (images.dummy) { const ht = 66, wd = ht * images.dummy.width / images.dummy.height; ctx.drawImage(images.dummy, -wd / 2, -ht + 3, wd, ht); ctx.restore(); return; }
  ctx.strokeStyle = "#05080d"; ctx.lineWidth = 4; ctx.lineCap = "round"; ctx.beginPath(); ctx.moveTo(0, 3); ctx.lineTo(0, -52); ctx.moveTo(-15, -36); ctx.lineTo(15, -36); ctx.stroke();
  ctx.fillStyle = "#0a0f17"; ctx.strokeStyle = rgba(b.fog, .4); ctx.lineWidth = 1.2; ctx.beginPath(); ctx.ellipse(0, -30, 10, 15, 0, 0, TAU); ctx.fill(); ctx.stroke(); ctx.beginPath(); ctx.arc(0, -55, 8, 0, TAU); ctx.fill(); ctx.stroke();
  ctx.strokeStyle = rgba(b.fog, .3); ctx.beginPath(); ctx.moveTo(-8, -34); ctx.lineTo(8, -26); ctx.moveTo(-7, -24); ctx.lineTo(7, -20); ctx.moveTo(-4, -57); ctx.lineTo(4, -53); ctx.stroke();
  ctx.restore();
}
function drawCamp(wx, wy, b) {
  if (images["camp-fire-1"]) return drawCampfire(wx, wy);
  const x = X(wx), y = Y(wy), flick = .85 + .15 * Math.sin(now * 6 + wx);
  ctx.globalCompositeOperation = "lighter"; drawGlow(x, y - 16 * scale, 96 * scale * flick, [255, 190, 120], .42); drawGlow(x, y - 14 * scale, 22 * scale, [255, 240, 210], .9); ctx.globalCompositeOperation = "source-over";
  ctx.fillStyle = "#05080d"; ctx.beginPath(); ctx.moveTo(x - 9 * scale, y + 2); ctx.lineTo(x - 6 * scale, y - 6 * scale); ctx.lineTo(x + 6 * scale, y - 6 * scale); ctx.lineTo(x + 9 * scale, y + 2); ctx.fill();
  ctx.fillStyle = "rgba(255,226,170,.95)"; ctx.beginPath(); ctx.moveTo(x - 4 * scale, y - 7 * scale); ctx.quadraticCurveTo(x - 5 * scale, y - 16 * scale, x + Math.sin(now * 7 + wx) * 1.5 * scale, y - (23 + flick * 4) * scale); ctx.quadraticCurveTo(x + 5 * scale, y - 16 * scale, x + 4 * scale, y - 7 * scale); ctx.fill();
}
let fireBox = null;
function drawCampfire(wx, wy) {
  if (!fireBox) { const m = measure(images["camp-fire-1"]); fireBox = m; }
  const x = X(wx), y = Y(wy), flick = .86 + .1 * Math.sin(now * 9 + wx) + .05 * Math.sin(now * 23 + wx), frame = images["camp-fire-" + [1, 2, 1, 3, 4, 3][Math.floor(now * 8 + wx) % 6]], ht = 60 * scale, k = ht / (fireBox.bottom - fireBox.top);
  ctx.globalCompositeOperation = "lighter"; drawGlow(x, y - 18 * scale, 150 * scale * flick, [255, 170, 90], .5); drawGlow(x, y - 16 * scale, 60 * scale * flick, [255, 220, 160], .55); ctx.globalCompositeOperation = "source-over";
  ctx.drawImage(frame, x - fireBox.cx * k, y - fireBox.bottom * k + 3 * scale, fireBox.size * k, fireBox.size * k);
  ctx.globalCompositeOperation = "lighter";
  for (let i = 0; i < 7; i++) { const p = (now * (.32 + i * .037) + i * .31 + wx * .01) % 1, sx = x + (Math.sin(p * 7 + i * 2.1) * 9 + (i - 3) * 2.2) * scale, sy = y - (24 + p * 96) * scale; drawGlow(sx, sy, (4.5 - p * 3) * scale, [255, 190, 100], (1 - p) * .95); }
  ctx.globalCompositeOperation = "source-over";
}
function drawCampProp(kind, wx, wy, ht, flip) {
  const img = images["camp-" + kind]; if (!img) return;
  if (!drawCampProp.box) drawCampProp.box = {}; const box = drawCampProp.box[kind] || (drawCampProp.box[kind] = measure(img)), k = ht * scale / (box.bottom - box.top);
  ctx.save(); ctx.translate(X(wx), Y(wy)); if (flip) ctx.scale(-1, 1); ctx.drawImage(img, -box.cx * k, -box.bottom * k + 2 * scale, box.size * k, box.size * k); ctx.restore();
}
function clip(text, n) { const a = Array.from(text); return a.length > n ? a.slice(0, n - 1).join("") + "…" : text; }

function drawBlade(len) { ctx.strokeStyle = "#dfe9f2"; ctx.lineWidth = 2.6; ctx.lineCap = "round"; ctx.beginPath(); ctx.moveTo(4, 0); ctx.lineTo(len, 0); ctx.stroke(); ctx.strokeStyle = "#0a0e18"; ctx.lineWidth = 3.4; ctx.beginPath(); ctx.moveTo(-3, 0); ctx.lineTo(3, 0); ctx.moveTo(3.5, -3.5); ctx.lineTo(3.5, 3.5); ctx.stroke(); }
const WEAPON = ["bow", "dagger", "longsword", "dagger", "spear", "hammer", "daggers", "axe", "dagger", "greatsword", "scythe", "dagger"];
const REACH = { bow: 130, spear: 52, greatsword: 50, scythe: 50, longsword: 46, hammer: 40, axe: 38 };
const reach = c => REACH[WEAPON[c.cast]] || 34, archer = c => WEAPON[c.cast] === "bow";
const CAST_SIZE = [60, 66, 58, 60, 68, 58, 60, 62, 54, 66, 54, 58], CAST_HAND = [[9, -20], [9, -20], [10, -20], [9, -18], [9, -20], [10, -20], [9, -20], [9, -20], [8, -18], [6, -24], [10, -24], [10, -22]];
const FRAME_NAMES = ["run-1", "run-2", "run-3", "run-4", "slash-1", "slash-2", "slash-3", "slash-4", "jump", "fall", "cling", "land", "idle-1", "idle-2", "puzzled", "sit"], frameBox = new Map();
function frameKey(cast, name) { return "f" + String(cast + 1).padStart(2, "0") + "-" + name; }
// Every frame of one character shares a canvas; the standing frame gives its ground line and size.
const GROUP_REF = { m: "idle-1", a: "look", b: "think" }, GROUP_OF = {};
for (const n of FRAME_NAMES) GROUP_OF[n] = "m";
for (const n of ["runb-1", "runb-2", "runb-3", "runb-4", "runb-5", "runb-6", "runb-7", "runb-8", "dash", "slash-up", "slash-down", "victory", "sleep-1", "sleep-2", "yawn", "look"]) GROUP_OF[n] = "a";
for (const n of ["cut-1", "cut-2", "cut-3", "cut-4", "cut-5", "cut-6", "guard", "wave", "wall-slide", "wall-kick", "think", "shrug", "warm", "drink", "sit-2", "nod"]) GROUP_OF[n] = "b";
function measure(img) {
  const g = document.createElement("canvas"); g.width = img.width; g.height = img.height;
  const k = g.getContext("2d"); k.drawImage(img, 0, 0); const a = k.getImageData(0, 0, g.width, g.height).data; let top = g.height, bottom = 0, sum = 0, n = 0;
  for (let y = 0; y < g.height; y++) for (let x = 0; x < g.width; x++) if (a[(y * g.width + x) * 4 + 3] > 40) { if (y < top) top = y; if (y > bottom) bottom = y; if (y > g.height * .55) { sum += x; n++; } }
  return { top, bottom, cx: n ? sum / n : g.width / 2, size: g.width };
}
// The standing frame of each sheet gives that sheet's ground line, centre and scale.
function castBox(cast, group) {
  const id = cast + (group || "m"); if (frameBox.has(id)) return frameBox.get(id);
  if (!group || group === "m") for (const name of FRAME_NAMES) if (!images[frameKey(cast, name)]) return null;
  const img = images[frameKey(cast, GROUP_REF[group || "m"])]; if (!img) return null;
  const box = measure(img); frameBox.set(id, box); return box;
}
const has = (c, name) => !!images[frameKey(c.cast, name)] && !!castBox(c.cast, GROUP_OF[name]);
const REST_POSES = ["sit", "warm", "sit-2", "drink", "warm", "nod"];
function pickFrame(c) {
  const cling = c.mode === "climb" && c.sub === "cling", air = !c.ground && !cling, moving = c.ground && Math.abs(c.vx) > 12, any = (...names) => names.find(n => has(c, n));
  if (c.slash > 0) {
    const p = c.slash;
    if (c.slashKind === "down" && has(c, "slash-down")) return ["slash-down", 0];
    if (c.slashKind === "up" && has(c, "slash-up")) return ["slash-up", 0];
    const rot = c.slashKind === "down" ? .75 : c.slashKind === "up" ? -.7 : 0;
    if (has(c, "cut-6")) return [p < .1 ? "cut-2" : p < .24 ? "cut-3" : p < .46 ? "cut-4" : p < .76 ? "cut-5" : "cut-6", rot];
    return [p < .14 ? "slash-1" : p < .38 ? "slash-2" : p < .78 ? "slash-3" : "slash-4", rot];
  }
  if (c.dash > 0) return [any("dash", "run-1"), 0];
  if (cling) return [c.kick > 0 ? any("wall-kick", "cling") : c.vy > 26 ? any("wall-slide", "cling") : "cling", 0];
  if (air) return [c.kick > 0 && has(c, "wall-kick") ? "wall-kick" : c.vy < 60 ? "jump" : "fall", 0];
  if (c.sit > .5) {
    if (status(c.row) === "work_complete") return [any(Math.floor(now * .7 + c.x * .01) % 2 ? "sleep-2" : "sleep-1", "sit"), 0];
    return [any(REST_POSES[Math.floor(now / 4.5 + (hash(c.ref) % 97)) % REST_POSES.length], "sit"), 0];
  }
  if (c.land > 0) return ["land", 0];
  if (moving) return has(c, "runb-8") ? ["runb-" + (1 + (Math.floor(c.run / (Math.PI / 4)) % 8 + 8) % 8), 0] : ["run-" + (1 + (Math.floor(c.run / (Math.PI / 2)) % 4 + 4) % 4), 0];
  if (c.mode === "ponder") return [c.arm === 2 ? any("shrug", "puzzled") : c.arm === 1 ? "puzzled" : Math.abs(c.tiltTo) > .1 ? any("think", "puzzled") : Math.floor(now * 1.6 + c.x * .013) % 2 ? "idle-2" : "idle-1", 0];
  if (c.mode === "fight" && c.sub === "flourish" && c.t > .25) return [any("victory", "idle-1"), 0];
  if (c.mode === "fight" && c.enemy && c.enemy.state === "tele") return [any("guard", "idle-1"), 0];
  if (c.pose && c.poseT > 0 && has(c, c.pose)) return [c.pose, 0];
  return [Math.floor(now * 1.6 + c.x * .013) % 2 ? "idle-2" : "idle-1", 0];
}
function drawFrames(c, ghost) {
  if (!castBox(c.cast)) return false;
  const ok = live(c.row), state = ok ? status(c.row) : "unknown", x = ghost ? ghost.x : c.x, y = ghost ? ghost.y : c.y, face = ghost ? ghost.face : c.face;
  const picked = pickFrame(c), name = picked[0], rot = picked[1], box = castBox(c.cast, GROUP_OF[name]);
  const key = frameKey(c.cast, name), img = ok ? images[key] : grey(key) || images[key], sq = c.sq * .45;
  // Scale by the sheet's own standing height so every sheet draws the character at one size.
  const k = CAST_SIZE[c.cast] * 1.3 / (box.bottom - box.top);
  ctx.save(); ctx.translate(X(x), Y(y)); ctx.scale(scale, scale);
  ctx.globalAlpha = ghost ? ghost.a * .45 : c.alpha * (ok ? 1 : .75);
  if (!ghost && state !== "unknown") { const col = EYE[state], s = c.scarf, tail = s[s.length - 1], wx = tail.x - x, wy = tail.y - y - 16 + Math.sin(now * 3 + c.x * .1) * 2.5; ctx.globalCompositeOperation = "lighter"; for (let i = 2; i < s.length; i++) { ctx.globalAlpha = c.alpha * .1 * i; ctx.drawImage(glow(col), s[i].x - x - 5, s[i].y - y - 21, 10, 10); } ctx.globalAlpha = c.alpha; ctx.drawImage(glow(col), wx - 13, wy - 13, 26, 26); ctx.drawImage(glow([255, 255, 255]), wx - 4, wy - 4, 8, 8); ctx.globalCompositeOperation = "source-over"; }
  ctx.scale(face * (1 - sq * .7), 1 + sq);
  if (rot) { ctx.translate(0, -30); ctx.rotate(rot); ctx.translate(0, 30); }
  ctx.drawImage(img, -box.cx * k, -box.bottom * k + 1.5, box.size * k, box.size * k);
  ctx.restore(); ctx.globalAlpha = 1;
  if (!ghost && ok && name.startsWith("sleep")) for (let i = 0; i < 3; i++) { const p = (now * .35 + i / 3 + c.x * .01) % 1, zx = X(x + face * 6 + p * 14 + Math.sin(p * 6 + i) * 3), zy = Y(y - 22 - p * 34); ctx.globalAlpha = Math.sin(p * Math.PI) * .85; ctx.font = "700 " + (7 + p * 7) * scale + "px Georgia,serif"; ctx.textAlign = "center"; ctx.fillStyle = "#dfeaf4"; ctx.fillText("z", zx, zy); }
  ctx.globalAlpha = 1; return true;
}
function drawPuppet(c, ghost) {
  if (drawFrames(c, ghost)) return true;
  const key = CAST[c.cast], img = images[key]; if (!img) return false;
  const ok = live(c.row), state = ok ? status(c.row) : "unknown", x = ghost ? ghost.x : c.x, y = ghost ? ghost.y : c.y, face = ghost ? ghost.face : c.face;
  const cling = c.mode === "climb" && c.sub === "cling", air = !c.ground && !cling, moving = c.ground && Math.abs(c.vx) > 12;
  const breathe = Math.sin(now * 2.1 + c.x * .02) * .014, sy = (1 + c.sq + breathe + (air ? clamp(Math.abs(c.vy) / 5200, 0, .1) : 0)) * (1 - c.sit * .16), sx = 1 - c.sq * .7 - breathe + c.sit * .06;
  const bob = moving ? Math.abs(Math.sin(c.run)) * -3 : 0, waddle = moving ? Math.sin(c.run) * .09 : 0, kS = c.slash > 0 ? 1 - easeOut(clamp(c.slash * 1.7, 0, 1)) : 0;
  const lean = clamp(c.vx * .0011, -.24, .24) * face + (cling ? -.2 : 0) + (air ? clamp(c.vy * .0002, -.12, .12) : 0) + kS * (c.slashKind === "down" ? .5 : c.slashKind === "up" ? -.3 : .28) + c.tilt * .6 + waddle;
  const ht = CAST_SIZE[c.cast] * 1.22, wd = ht * img.width / img.height, hand = [CAST_HAND[c.cast][0] * 1.22, CAST_HAND[c.cast][1] * 1.22];
  ctx.save(); ctx.translate(X(x), Y(y)); ctx.scale(scale, scale);
  ctx.globalAlpha = ghost ? ghost.a * .45 : c.alpha * (ok ? 1 : .75);
  if (!ghost && state !== "unknown") { // a small state-coloured wisp trails each wanderer
    const col = EYE[state], s = c.scarf, tail = s[s.length - 1], wx = tail.x - x, wy = tail.y - y - 16 + Math.sin(now * 3 + c.x * .1) * 2.5;
    ctx.globalCompositeOperation = "lighter"; for (let i = 2; i < s.length; i++) { ctx.globalAlpha = c.alpha * .1 * i; ctx.drawImage(glow(col), s[i].x - x - 5, s[i].y - y - 21, 10, 10); } ctx.globalAlpha = c.alpha; ctx.drawImage(glow(col), wx - 13, wy - 13, 26, 26); ctx.drawImage(glow([255, 255, 255]), wx - 4, wy - 4, 8, 8); ctx.globalCompositeOperation = "source-over";
  }
  ctx.scale(face * sx, sy); ctx.translate(kS * 5, bob); ctx.rotate(lean);
  const slashing = c.slash > 0 && !ghost, carry = c.sword;
  if (!slashing && carry < .5 && !ghost) { ctx.save(); ctx.translate(-wd * .12, -ht * .5); ctx.rotate(-2.15); drawBlade(27); ctx.restore(); }
  ctx.drawImage(ok ? img : grey(key) || img, -wd / 2, -ht + 2, wd, ht);
  if (slashing) { const k = easeOut(clamp(c.slash * 1.7, 0, 1)), kind = c.slashKind, a = kind === "up" ? lerp(.5, -2.7, k) : kind === "down" ? lerp(-1.2, 1.9, k) : lerp(-2.4, .85, k); ctx.save(); ctx.translate(hand[0], hand[1]); ctx.rotate(a); drawBlade(30); ctx.restore(); }
  else if (carry >= .5 && !ghost) { const planted = clamp(carry - 2, 0, 1), rest = clamp(carry - 1, 0, 1) - planted, spin = c.mode === "fight" && c.sub === "flourish" && c.t > .5 ? (c.t - .5) * 14 : 0; ctx.save(); if (planted > .5) { ctx.translate(wd * .5 + 4, -30); ctx.rotate(Math.PI / 2); } else { ctx.translate(hand[0], hand[1]); ctx.rotate(lerp(.45, 1.2, rest) + Math.sin(now * 2 + c.x) * .04 + spin); } drawBlade(planted > .5 ? 30 : 28); ctx.restore(); }
  ctx.restore(); ctx.globalAlpha = 1; return true;
}
function drawChar(c, ghost) {
  if (drawPuppet(c, ghost)) return;
  const r = c.row, ok = live(r), state = ok ? status(r) : "unknown", hue = c.st.ch.hue, x = ghost ? ghost.x : c.x, y = ghost ? ghost.y : c.y, face = ghost ? ghost.face : c.face;
  const sit = c.sit, cling = c.mode === "climb" && c.sub === "cling", air = !c.ground && !cling, moving = c.ground && Math.abs(c.vx) > 12;
  const breathe = Math.sin(now * 2.1 + c.x * .02) * .012, sy = 1 + c.sq + breathe + (air ? clamp(Math.abs(c.vy) / 5200, 0, .1) : 0), sx = 1 - c.sq * .7 - breathe;
  const bob = moving ? Math.abs(Math.sin(c.run)) * -2.2 : 0, lean = clamp(c.vx * .0011, -.22, .22) + (cling ? -.16 * face : 0);
  ctx.save(); ctx.translate(X(x), Y(y)); ctx.scale(scale, scale);
  if (ghost) ctx.globalAlpha = ghost.a * .5; else ctx.globalAlpha = c.alpha * (ok ? 1 : .6);
  // scarf ribbon, drawn in world space behind the cloak
  if (!ghost) { ctx.strokeStyle = ok ? "hsl(" + hue + ",72%,62%)" : "#56617a"; ctx.lineCap = "round"; ctx.lineJoin = "round"; for (let i = 1; i < c.scarf.length; i++) { ctx.lineWidth = 4.4 - i * .6; ctx.beginPath(); ctx.moveTo(c.scarf[i - 1].x - x, c.scarf[i - 1].y - y); ctx.lineTo(c.scarf[i].x - x, c.scarf[i].y - y); ctx.stroke(); } }
  ctx.scale(face * sx, sy); ctx.rotate(lean * face * face);
  const down = sit * 10, ink = ok ? "#0b0f1c" : "#1b2130", rim = ok ? "#3a466b" : "#3a4254";
  // legs
  if (sit < .5) { ctx.strokeStyle = ink; ctx.lineWidth = 3.6; ctx.lineCap = "round"; const a = Math.sin(c.run) * 7, lift = Math.max(0, Math.cos(c.run)) * 4, lift2 = Math.max(0, -Math.cos(c.run)) * 4, tuck = air ? 4 : 0; ctx.beginPath(); ctx.moveTo(-3, -12); ctx.lineTo(-3 + a - tuck, -lift - tuck); ctx.moveTo(3, -12); ctx.lineTo(3 - a + tuck * .5, -lift2 - tuck * 1.6); ctx.stroke(); }
  // sword carried on the back sits behind the cloak
  const slashing = c.slash > 0, carry = c.sword;
  if (!slashing && carry < .5 && !ghost) { ctx.save(); ctx.translate(-5, -27 + bob + down); ctx.rotate(-2.2); drawBlade(27); ctx.restore(); }
  // cloak
  const hem = c.hem, flare = air ? clamp(c.vy * .012, -3, 7) : 0, wave = Math.sin(now * 3 + c.x * .05) * 1.2, hy = -7 + down * .2 - flare;
  ctx.fillStyle = ink; ctx.strokeStyle = rim; ctx.lineWidth = 1.3; ctx.lineJoin = "round";
  ctx.beginPath(); ctx.moveTo(-6, -33 + bob + down); ctx.quadraticCurveTo(-12, -22 + down, -14 + hem + wave - sit * 5, hy); ctx.quadraticCurveTo(-6 + hem * .7, hy + 4.5, hem * .6, hy + 1.5 + wave * .5); ctx.quadraticCurveTo(7 + hem * .7, hy + 5, 13 + hem * .5 - wave + sit * 5, hy); ctx.quadraticCurveTo(11, -22 + down, 6, -33 + bob + down); ctx.closePath(); ctx.fill(); ctx.stroke();
  // collar in the Project colour
  ctx.strokeStyle = ok ? "hsl(" + hue + ",70%,58%)" : "#56617a"; ctx.lineWidth = 3.4; ctx.lineCap = "round"; ctx.beginPath(); ctx.moveTo(-6, -32 + bob + down); ctx.quadraticCurveTo(0, -28.5 + bob + down, 6, -32 + bob + down); ctx.stroke();
  // hood: a pale leaf-shaped cowl with one back-swept tip
  ctx.save(); ctx.translate(0, -40 + bob + down); ctx.rotate(c.tilt * face + (moving ? .06 : 0));
  const tip = -hem * .5 - (air ? c.vy * .004 : 0);
  ctx.fillStyle = ok ? "#e6edf2" : "#8792a3"; ctx.strokeStyle = ok ? "#9fb2c2" : "#5d6777"; ctx.lineWidth = 1;
  ctx.beginPath(); ctx.moveTo(10.5, 1); ctx.bezierCurveTo(11, -8, 3, -12.5, -3, -10.5); ctx.bezierCurveTo(-8, -14, -13 + tip * .6, -18, -18 + tip, -17 - Math.abs(tip) * .2); ctx.bezierCurveTo(-13 + tip * .5, -11, -11.5, -5, -10.5, 1); ctx.bezierCurveTo(-10, 9, 10, 9, 10.5, 1); ctx.closePath(); ctx.fill(); ctx.stroke();
  // shadowed face and two ember eyes that carry the state colour
  ctx.fillStyle = "#0a0d18"; ctx.beginPath(); ctx.ellipse(2.6, 1.2, 6.6, 5.6, 0, 0, TAU); ctx.fill();
  if (state !== "unknown" && !ghost) { const eye = EYE[state], blink = c.blink < 0 || sit > .8 ? .15 : 1; ctx.globalCompositeOperation = "lighter"; ctx.globalAlpha *= .7; ctx.drawImage(glow(eye), -4, -6, 15, 14); ctx.globalAlpha /= .7; ctx.globalCompositeOperation = "source-over"; ctx.fillStyle = rgba(eye, 1); ctx.beginPath(); ctx.ellipse(1.2, 1, 1.5, 2.5 * blink, 0, 0, TAU); ctx.ellipse(5.8, 1, 1.5, 2.5 * blink, 0, 0, TAU); ctx.fill(); }
  ctx.restore();
  // arm and blade
  if (!ghost) {
    if (slashing) { const k = easeOut(clamp(c.slash * 1.7, 0, 1)), kind = c.slashKind, a = kind === "up" ? lerp(.5, -2.7, k) : kind === "down" ? lerp(-1.2, 1.9, k) : lerp(-2.4, .85, k); ctx.save(); ctx.translate(4, -27 + bob); ctx.rotate(a); ctx.strokeStyle = ink; ctx.lineWidth = 3; ctx.beginPath(); ctx.moveTo(-3, 0); ctx.lineTo(2, 0); ctx.stroke(); drawBlade(30); ctx.restore(); }
    else if (carry >= .5) { const planted = clamp(carry - 2, 0, 1), rest = clamp(carry - 1, 0, 1) - planted, spin = c.mode === "fight" && c.sub === "flourish" && c.t > .5 ? (c.t - .5) * 14 : 0; ctx.save(); if (planted > .5) { ctx.translate(15, -30 + down * .4); ctx.rotate(Math.PI / 2); } else { ctx.translate(6, -24 + bob + down * .7); ctx.rotate(lerp(.5, 1.25, rest) + Math.sin(now * 2 + c.x) * .04 + spin); } drawBlade(planted > .5 ? 30 : 28); ctx.restore(); }
    if (c.arm === 1) { ctx.strokeStyle = ink; ctx.lineWidth = 3; ctx.lineCap = "round"; const wig = Math.sin(now * 16) * 1.6; ctx.beginPath(); ctx.moveTo(5, -30); ctx.quadraticCurveTo(13, -36, 8 + wig, -47); ctx.stroke(); }
    else if (c.arm === 2) { ctx.strokeStyle = ink; ctx.lineWidth = 3; ctx.lineCap = "round"; ctx.beginPath(); ctx.moveTo(6, -30); ctx.lineTo(15, -33); ctx.moveTo(-6, -30); ctx.lineTo(-15, -33); ctx.stroke(); }
  }
  ctx.restore(); ctx.globalAlpha = 1;
}
function drawQuestion(c) {
  const x = X(c.x), y = Y(c.y - CAST_SIZE[c.cast] * 1.3 - 16) + Math.sin(now * 2.6 + c.x) * 3 * scale, k = easeOut(clamp(c.q, 0, 1)), s = scale * (.5 + .5 * k);
  ctx.globalCompositeOperation = "lighter"; drawGlow(x, y, 26 * s, [255, 150, 120], .55); ctx.globalCompositeOperation = "source-over";
  ctx.save(); ctx.translate(x, y); ctx.rotate(Math.sin(now * 1.7 + c.x) * .12); ctx.font = "700 " + 22 * s + "px Georgia,serif"; ctx.textAlign = "center"; ctx.textBaseline = "middle"; ctx.lineWidth = 3 * s; ctx.strokeStyle = "#1a0b10"; ctx.strokeText("?", 0, 0); ctx.fillStyle = "#ffd9c4"; ctx.fillText("?", 0, 0); ctx.restore();
  // an attention ring on the ground
  const p = (now * .7 + c.x * .01) % 1; ctx.strokeStyle = rgba([255, 160, 130], (1 - p) * .5); ctx.lineWidth = 1.5 * scale; ctx.beginPath(); ctx.ellipse(X(c.x), Y(c.st.y) + 1, (10 + p * 22) * scale, (2 + p * 4) * scale, 0, 0, TAU); ctx.stroke();
}
const FOE_SIZE = [40, 46, 44, 48, 40, 70], FOE_FLIP = [1, 1, 1, 1, 1, 1];
const FOE_FRAMES = ["move-1", "move-2", "windup", "attack", "hurt", "death-1", "death-2", "death-3"], foeBox = new Map();
function foeKey(skin, name) { return "g" + (skin + 1) + "-" + name; }
function foeFrameBox(skin) {
  if (foeBox.has(skin)) return foeBox.get(skin);
  for (const name of FOE_FRAMES) if (!images[foeKey(skin, name)]) return null;
  const img = images[foeKey(skin, "move-1")], g = document.createElement("canvas"); g.width = img.width; g.height = img.height;
  const k = g.getContext("2d"); k.drawImage(img, 0, 0); const a = k.getImageData(0, 0, g.width, g.height).data; let top = g.height, bottom = 0, left = g.width, right = 0;
  for (let y = 0; y < g.height; y++) for (let x = 0; x < g.width; x++) if (a[(y * g.width + x) * 4 + 3] > 40) { if (y < top) top = y; if (y > bottom) bottom = y; if (x < left) left = x; if (x > right) right = x; }
  const box = { top, bottom, cx: (left + right) / 2, size: g.width }; foeBox.set(skin, box); return box;
}
function drawFoeFrames(e) {
  const box = foeFrameBox(e.skin); if (!box) return false;
  const grow = e.state === "emerge" ? easeOut(clamp(e.t / .55, 0, 1)) : 1, shake = e.state === "tele" ? Math.sin(now * 70) * 1.5 : e.hurt > .12 ? Math.sin(now * 90) * 2 : 0;
  let name, hop = 0;
  if (e.state === "dying") name = e.t < .13 ? "death-1" : e.t < .3 ? "death-2" : "death-3";
  else if (e.hurt > 0) name = "hurt";
  else if (e.state === "tele") name = "windup";
  else if (e.state === "lunge") name = "attack";
  else name = Math.floor(now * (e.skin === 1 ? 11 : e.skin === 3 ? 2.5 : 5) + e.phase) % 2 ? "move-2" : "move-1";
  if (e.skin === 2 && e.state === "roam" && e.hurt <= 0) hop = -Math.abs(Math.sin(now * 5 + e.phase)) * 10; else if (e.skin === 2 && e.state === "lunge") hop = -Math.sin(clamp(e.t / .42, 0, 1) * Math.PI) * 30;
  const img = images[foeKey(e.skin, name)], k = FOE_SIZE[e.skin] / (box.bottom - box.top), x = X(e.x) + shake * scale, y = Y(e.y + hop), fade = e.state === "dying" ? clamp(1.6 - e.t * 3, 0, 1) : 1;
  if (e.skin === 3 && e.state !== "dying") { ctx.strokeStyle = "rgba(200,210,220,.4)"; ctx.lineWidth = scale; ctx.beginPath(); ctx.moveTo(x, y - FOE_SIZE[3] * scale * .9); ctx.lineTo(x + Math.sin(now * 1.8 + e.phase) * 6 * scale, y - 260 * scale); ctx.stroke(); }
  ctx.globalCompositeOperation = "lighter"; drawGlow(x, y - FOE_SIZE[e.skin] * .5 * scale, FOE_SIZE[e.skin] * (e.state === "dying" ? 1.5 + e.t * 2 : 1.1) * scale * grow, [255, 110, 40], (.26 + .08 * Math.sin(now * 5 + e.phase) + (e.state === "tele" ? .2 : 0)) * fade); ctx.globalCompositeOperation = "source-over";
  ctx.save(); ctx.translate(x, y); ctx.scale(scale * e.face * grow, scale * grow); if (e.hurt > 0 && e.state !== "dying") ctx.rotate(-.18 * clamp(e.hurt / .24, 0, 1)); ctx.globalAlpha = fade;
  ctx.drawImage(img, -box.cx * k, -box.bottom * k + 1, box.size * k, box.size * k);
  if (e.flash > 0) { ctx.globalCompositeOperation = "lighter"; ctx.globalAlpha = .85; ctx.drawImage(img, -box.cx * k, -box.bottom * k + 1, box.size * k, box.size * k); ctx.globalCompositeOperation = "source-over"; }
  ctx.restore(); ctx.globalAlpha = 1; return true;
}
function drawWild(e) {
  const box = foeFrameBox(e.skin); if (!box || e.cool > 0) return;
  const hop = e.skin === 2 ? -Math.abs(Math.sin(now * 3 + e.phase)) * 9 : 0, img = images[foeKey(e.skin, e.hurt > 0 ? "hurt" : Math.floor(now * (e.skin === 1 ? 11 : e.skin === 3 ? 2.5 : 5) + e.phase) % 2 ? "move-2" : "move-1")], k = FOE_SIZE[e.skin] * .82 / (box.bottom - box.top), x = X(e.x), y = Y(e.y + hop);
  if (e.skin === 3) { ctx.strokeStyle = "rgba(200,210,220,.32)"; ctx.lineWidth = scale; ctx.beginPath(); ctx.moveTo(x, y - FOE_SIZE[3] * scale * .7); ctx.lineTo(x, -4); ctx.stroke(); }
  ctx.globalCompositeOperation = "lighter"; drawGlow(x, y - FOE_SIZE[e.skin] * .4 * scale, FOE_SIZE[e.skin] * .8 * scale, [255, 110, 40], .16); ctx.globalCompositeOperation = "source-over";
  ctx.save(); ctx.translate(x, y); ctx.scale(scale * (e.face || 1), scale); ctx.globalAlpha = .88 * e.fade; ctx.drawImage(img, -box.cx * k, -box.bottom * k + 1, box.size * k, box.size * k); if (e.flash > 0) { ctx.globalCompositeOperation = "lighter"; ctx.drawImage(img, -box.cx * k, -box.bottom * k + 1, box.size * k, box.size * k); ctx.globalCompositeOperation = "source-over"; } ctx.restore(); ctx.globalAlpha = 1;
}
function drawFoe(e) {
  if (drawFoeFrames(e)) return true;
  if (e.state === "dying") return true;
  const img = images[FOES[e.skin]]; if (!img) return false;
  const grow = e.state === "emerge" ? easeOut(clamp(e.t / .55, 0, 1)) : 1, shake = e.state === "tele" ? Math.sin(now * 70) * 1.5 : 0, ht = FOE_SIZE[e.skin], wd = ht * img.width / img.height;
  let hop = 0, rot = 0, sqx = 1, sqy = 1;
  if (e.skin === 2) { const ph = (now * 1.6 + e.phase) % 1; hop = e.state === "roam" ? -Math.sin(ph * Math.PI) * 14 : e.state === "lunge" ? -Math.sin(clamp(e.t / .42, 0, 1) * Math.PI) * 30 : 0; sqy = 1 + (hop < -2 ? .1 : -.06); sqx = 2 - sqy; }
  else if (e.skin === 4) rot = e.x * .09;
  else if (e.skin === 1) { sqx = 1 + Math.sin(now * 22 + e.phase) * .07; sqy = 1 - Math.sin(now * 22 + e.phase) * .05; }
  else if (e.skin === 3) rot = Math.sin(now * 1.8 + e.phase) * .12;
  else { sqy = 1 + Math.sin(now * 7 + e.phase) * .035; sqx = 2 - sqy; rot = e.skin === 5 ? Math.sin(now * 3 + e.phase) * .05 : 0; }
  if (e.state === "tele") { sqy *= .84; sqx *= 1.1; } else if (e.state === "lunge" && e.skin !== 2) { sqx *= 1.14; sqy *= .92; }
  const x = X(e.x) + shake * scale, y = Y(e.y + hop);
  if (e.skin === 3) { ctx.strokeStyle = "rgba(200,210,220,.4)"; ctx.lineWidth = 1 * scale; ctx.beginPath(); ctx.moveTo(x, y - ht * scale * .9); ctx.lineTo(x + Math.sin(now * 1.8 + e.phase) * 6 * scale, y - 260 * scale); ctx.stroke(); }
  ctx.globalCompositeOperation = "lighter"; drawGlow(x, y - ht * .5 * scale, ht * 1.1 * scale * grow, [255, 110, 40], .26 + .08 * Math.sin(now * 5 + e.phase)); ctx.globalCompositeOperation = "source-over";
  ctx.save(); ctx.translate(x, y); ctx.scale(scale * e.face * FOE_FLIP[e.skin] * grow * sqx, scale * grow * sqy);
  if (e.skin === 4) { ctx.translate(0, -ht / 2); ctx.rotate(rot); ctx.translate(0, ht / 2); } else ctx.rotate(rot);
  ctx.drawImage(img, -wd / 2, -ht + 1, wd, ht);
  if (e.flash > 0) { ctx.globalCompositeOperation = "lighter"; ctx.globalAlpha = .9; ctx.drawImage(img, -wd / 2, -ht + 1, wd, ht); ctx.drawImage(img, -wd / 2, -ht + 1, wd, ht); ctx.globalAlpha = 1; ctx.globalCompositeOperation = "source-over"; }
  ctx.restore(); return true;
}
function drawEnemy(e) {
  if (drawFoe(e)) return;
  const x = X(e.x), y = Y(e.y), grow = e.state === "emerge" ? easeOut(clamp(e.t / .55, 0, 1)) : 1, shake = e.state === "tele" ? Math.sin(now * 70) * 1.5 : 0;
  ctx.globalCompositeOperation = "lighter"; drawGlow(x, y - 14 * scale, 40 * scale * grow, [255, 110, 40], .3 + .08 * Math.sin(now * 5 + e.phase)); ctx.globalCompositeOperation = "source-over";
  ctx.save(); ctx.translate(x + shake * scale, y); ctx.scale(scale * e.face * grow * 1.4, scale * grow * 1.4);
  const body = e.flash > 0 ? "#fff3e0" : "#1c1018", rim = e.flash > 0 ? "#ffffff" : "#d2643a";
  if (e.type === "crawler") {
    const sq = e.state === "tele" ? .82 : e.state === "lunge" ? 1.12 : 1 + Math.sin(now * 6 + e.phase) * .03; ctx.scale(2 - sq, sq);
    ctx.strokeStyle = "#17101c"; ctx.lineWidth = 2.4; ctx.lineCap = "round"; for (let i = -1; i <= 1; i++) { const sw = Math.sin(now * 13 + i * 2 + e.phase) * 2.5; ctx.beginPath(); ctx.moveTo(i * 7, -4); ctx.lineTo(i * 8 + sw, 0); ctx.stroke(); }
    ctx.fillStyle = body; ctx.strokeStyle = rim; ctx.lineWidth = 1.2; ctx.beginPath(); ctx.moveTo(-15, -3); ctx.quadraticCurveTo(-15, -17, -7, -18); ctx.lineTo(-6, -25); ctx.lineTo(-2, -19); ctx.lineTo(1, -27); ctx.lineTo(4, -19); ctx.lineTo(8, -23); ctx.lineTo(9, -16); ctx.quadraticCurveTo(16, -13, 15, -3); ctx.closePath(); ctx.fill(); ctx.stroke();
    ctx.globalCompositeOperation = "lighter"; ctx.drawImage(glow([255, 140, 60]), 2, -17, 16, 16); ctx.globalCompositeOperation = "source-over"; ctx.fillStyle = "#ffb060"; ctx.beginPath(); ctx.ellipse(10, -9, 2.2, e.state === "tele" ? 3.2 : 2.2, 0, 0, TAU); ctx.fill();
  } else {
    const flap = Math.sin(now * 26 + e.phase); ctx.fillStyle = e.flash > 0 ? "#fff3e0" : "rgba(60,42,70,.85)"; ctx.strokeStyle = rim; ctx.lineWidth = 1;
    for (const s of [-1, 1]) { ctx.save(); ctx.translate(-2, -8); ctx.rotate(s * (.5 + flap * .55)); ctx.beginPath(); ctx.ellipse(-9, 0, 12, 4.5, 0, 0, TAU); ctx.fill(); ctx.stroke(); ctx.restore(); }
    ctx.fillStyle = body; ctx.beginPath(); ctx.ellipse(0, -6, 9, 7.5, 0, 0, TAU); ctx.fill(); ctx.stroke(); ctx.beginPath(); ctx.moveTo(-8, -4); ctx.lineTo(-16, -1); ctx.lineTo(-7, -8); ctx.fill();
    ctx.globalCompositeOperation = "lighter"; ctx.drawImage(glow([255, 140, 60]), -3, -14, 16, 16); ctx.globalCompositeOperation = "source-over"; ctx.fillStyle = "#ffb060"; ctx.beginPath(); ctx.arc(5, -6, 2.2, 0, TAU); ctx.fill();
  }
  ctx.restore();
}
function drawFx() {
  for (const f of fx) {
    const k = clamp(f.age / f.life, 0, 1);
    if (f.kind === "dust") { ctx.fillStyle = rgba(f.color, (1 - k) * .28); ctx.beginPath(); ctx.arc(X(f.x), Y(f.y), f.r * (1 + k * 1.6) * scale, 0, TAU); ctx.fill(); }
    else if (f.kind === "spark") { ctx.globalCompositeOperation = "lighter"; drawGlow(X(f.x), Y(f.y), f.r * (1 - k * .6) * scale, f.color, 1 - k); ctx.globalCompositeOperation = "source-over"; }
    else if (f.kind === "arrow") { const px = X(f.x), py = Y(f.y), ax = (f.dx || 1) * 15 * scale, ay = (f.dy || 0) * 15 * scale; ctx.globalCompositeOperation = "lighter"; drawGlow(px, py, 9 * scale, [255, 240, 200], .5); ctx.globalCompositeOperation = "source-over"; ctx.strokeStyle = "#f2ead8"; ctx.lineWidth = 2 * scale; ctx.lineCap = "round"; ctx.beginPath(); ctx.moveTo(px - ax, py - ay); ctx.lineTo(px, py); ctx.stroke(); ctx.strokeStyle = "rgba(242,234,216,.3)"; ctx.beginPath(); ctx.moveTo(px - ax * 3, py - ay * 3); ctx.lineTo(px - ax, py - ay); ctx.stroke(); }
    else if (f.kind === "ring") { const r = (8 + easeOut(k) * 30) * scale, px = X(f.x), py = Y(f.y); ctx.strokeStyle = "rgba(255,240,215," + (1 - k) + ")"; ctx.lineWidth = (3.2 * (1 - k) + .4) * scale; ctx.lineCap = "round"; ctx.beginPath(); for (let i = 0; i < 6; i++) { const a = f.a + i * TAU / 6; ctx.moveTo(px + Math.cos(a) * r * .55, py + Math.sin(a) * r * .55); ctx.lineTo(px + Math.cos(a) * r, py + Math.sin(a) * r); } ctx.stroke(); }
    else if (f.kind === "flash") { ctx.globalCompositeOperation = "lighter"; drawGlow(X(f.x), Y(f.y), f.r * (1 + k) * scale, [255, 245, 225], (1 - k) * .9); ctx.globalCompositeOperation = "source-over"; }
    else if (f.kind === "wisp" && f.age > 0) { ctx.globalCompositeOperation = "lighter"; drawGlow(X(f.x), Y(f.y), 12 * scale, [255, 200, 140], 1 - k * .6); drawGlow(X(f.x), Y(f.y), 4 * scale, [255, 255, 255], 1); ctx.globalCompositeOperation = "source-over"; }
    else if (f.kind === "arc") {
      const c = f.c, e = easeOut(k); ctx.save(); ctx.translate(X(c.x + f.ox), Y(c.y + f.oy)); ctx.scale(scale * f.flip, scale); ctx.rotate(f.rot * f.flip);
      const R = 30 * (.72 + e * .45), a = 1.25; ctx.globalCompositeOperation = "lighter"; ctx.globalAlpha = (1 - k) * .35; ctx.drawImage(glow([180, 215, 255]), -R * .6, -R * 1.5, R * 2.4, R * 3); ctx.globalAlpha = 1 - k * k; ctx.globalCompositeOperation = "source-over";
      ctx.fillStyle = "#f4fbff"; ctx.beginPath(); ctx.moveTo(Math.cos(-a) * R, Math.sin(-a) * R); ctx.arc(0, 0, R, -a, a); ctx.quadraticCurveTo(R * lerp(.62, 1.02, e), 0, Math.cos(-a) * R, Math.sin(-a) * R); ctx.fill(); ctx.restore(); ctx.globalAlpha = 1;
    }
  }
}
function drawFireflies() {
  const cell = 150, left = cam - W / scale / 2 - 60, right = cam + W / scale / 2 + 60, first = Math.floor(left / cell), last = Math.floor(right / cell);
  ctx.globalCompositeOperation = "lighter";
  for (let k = first; k <= last; k++) for (let j = 0; j < 2; j++) {
    const r = rng(k * 9176 + j * 131 + 7), bx = k * cell + r() * cell, by = -30 - r() * 380, sp = .25 + r() * .35, ph = r() * TAU, rad = 26 + r() * 38, blink = Math.max(0, Math.sin(now * (1.1 + r() * 1.4) + ph));
    const t = now * sp + ph, wx = bx + Math.sin(t) * rad + Math.sin(t * 2.3 + 1) * rad * .4, wy = by + Math.cos(t * 1.3) * rad * .6 + Math.sin(t * .7) * 12, a = .25 + blink * .75, x = X(wx), y = Y(wy);
    for (let q = 1; q <= 3; q++) { const tt = t - q * .05, tx = bx + Math.sin(tt) * rad + Math.sin(tt * 2.3 + 1) * rad * .4, ty = by + Math.cos(tt * 1.3) * rad * .6 + Math.sin(tt * .7) * 12; drawGlow(X(tx), Y(ty), 4 * scale, [210, 255, 130], a * .22 / q); }
    drawGlow(x, y, (9 + blink * 6) * scale, [200, 255, 120], a * .8); drawGlow(x, y, 2.6 * scale, [255, 255, 220], a);
  }
  ctx.globalCompositeOperation = "source-over";
}
function drawMotes() {
  ctx.globalCompositeOperation = "lighter";
  for (const m of motes) {
    const px = ((m.x * W - cam * scale * m.z + now * m.vx * scale) % (W + 60) + W + 60) % (W + 60) - 30, py = ((m.y * H - now * m.vy * scale) % (H + 40) + H + 40) % (H + 40) - 20 + Math.sin(now * m.w + m.p) * 10 * scale;
    const b = biomeAt(cam), a = (.3 + .7 * Math.abs(Math.sin(now * m.w * .7 + m.p))) * m.a;
    drawGlow(px, py, m.r * scale * m.z * 2.2, b.mote, a);
  }
  ctx.globalCompositeOperation = "source-over";
}
function drawForeground() {
  const par = 1.45, cell = 560 * scale, off = cam * scale * par, first = Math.floor((off - W / 2) / cell) - 1, last = Math.floor((off + W / 2) / cell) + 1;
  ctx.fillStyle = "#010204";
  for (let k = first; k <= last; k++) {
    const r = rng(3131 + k * 17), x = k * cell - off + W / 2 + r() * cell * .5, sw = (40 + r() * 60) * scale, sh = (40 + r() * 80) * scale;
    ctx.beginPath(); ctx.moveTo(x - sw, -2); ctx.quadraticCurveTo(x - sw * .2, sh * .4, x + (r() - .5) * sw * .3, sh); ctx.quadraticCurveTo(x + sw * .3, sh * .3, x + sw, -2); ctx.fill();
    if (r() < .7) { const bx = x + cell * .5, n = 5 + Math.floor(r() * 4), fh = (50 + r() * 70) * scale; for (let f = 0; f < n; f++) { const t = f / (n - 1) - .5, sway = Math.sin(now * .8 + k + f) * 4 * scale; ctx.beginPath(); ctx.moveTo(bx - 4 * scale, H + 4); ctx.quadraticCurveTo(bx + t * 60 * scale, H - fh * .8, bx + t * 150 * scale + sway, H - fh * (1 - Math.abs(t) * .7)); ctx.quadraticCurveTo(bx + t * 40 * scale, H - fh * .4, bx + 4 * scale, H + 4); ctx.fill(); } }
  }
  const v = ctx.createRadialGradient(W / 2, H * .52, Math.min(W, H) * .32, W / 2, H * .52, Math.max(W, H) * .78); v.addColorStop(0, "rgba(0,0,0,0)"); v.addColorStop(1, "rgba(0,0,0,.72)"); ctx.fillStyle = v; ctx.fillRect(0, 0, W, H);
  const t = ctx.createLinearGradient(0, 0, 0, H * .2); t.addColorStop(0, "rgba(0,0,0,.6)"); t.addColorStop(1, "rgba(0,0,0,0)"); ctx.fillStyle = t; ctx.fillRect(0, 0, W, H * .2);
}
function drawTags(list) {
  const crowded = list.length > 16;
  ctx.textAlign = "center"; ctx.textBaseline = "middle";
  for (const c of list) {
    const state = live(c.row) ? status(c.row) : "unknown", need = state === "waiting_you", sel = selected === c.ref;
    if (crowded && !need && !sel) continue;
    const text = clip(c.row.title, sel ? 30 : 16), size = sel ? 12.5 : 11; ctx.font = (sel || need ? "600 " : "500 ") + size + "px ui-sans-serif,system-ui,-apple-system,'PingFang TC',sans-serif";
    const tw = ctx.measureText(text).width + 14, x = clamp(c.sx, tw / 2 + 4, W - tw / 2 - 4), y = c.sy - (CAST_SIZE[c.cast] * 1.3 + (need ? 36 : 12)) * scale - (sel ? 4 : 0), h = size + 9;
    ctx.fillStyle = sel ? "rgba(236,244,250,.94)" : "rgba(5,8,14,.62)"; ctx.strokeStyle = need ? "rgba(255,160,130,.8)" : sel ? "rgba(255,255,255,.9)" : "rgba(170,200,220,.22)"; ctx.lineWidth = 1;
    ctx.beginPath(); ctx.roundRect(x - tw / 2, y - h / 2, tw, h, h / 2); ctx.fill(); ctx.stroke();
    ctx.fillStyle = sel ? "#0a0f18" : need ? "#ffe2d6" : "rgba(226,236,244,.9)"; ctx.fillText(text, x, y + .5);
  }
}
function draw() {
  if (!W || !H) return;
  ctx.setTransform(DPR, 0, 0, DPR, 0, 0);
  drawBackdrop();
  const b = biomeAt(cam);
  drawSilhouettes(.34, "far", .3, .7, 101); drawFog(.4, -60, .2); drawShafts(); drawCrystalGlow(.6);
  drawSilhouettes(.6, "mid", .8, .85, 202); drawFog(.7, -24, .2);
  drawGround();
  const left = cam - W / scale / 2 - 60, right = cam + W / scale / 2 + 60, seen = [];
  for (const c of chars.values()) { c.sx = X(c.x); c.sy = Y(c.y); if (c.x > left && c.x < right) seen.push(c); }
  seen.sort((a, b) => (a.ref === selected) - (b.ref === selected));
  ctx.globalAlpha = 1; for (const e of wildList) if (e.x > left && e.x < right) drawWild(e);
  for (const c of seen) for (const g of c.ghosts) drawChar(c, g);
  for (const e of enemies) if (e.x > left && e.x < right) drawEnemy(e);
  for (const c of seen) { drawChar(c, null); if (c.mode === "ponder" && live(c.row)) drawQuestion(c); }
  drawFx(); drawFog(1.1, 6, .16); drawFireflies(); drawMotes(); drawForeground(); drawTags(seen);
  if (selected) { const c = chars.get(selected); if (c && seen.includes(c)) { const p = .5 + .5 * Math.sin(now * 4); ctx.strokeStyle = "rgba(240,248,255," + (.5 + p * .4) + ")"; ctx.lineWidth = 1.5; ctx.beginPath(); ctx.ellipse(c.sx, c.sy + 2, (17 + p * 2) * scale, 4.5 * scale, 0, 0, TAU); ctx.stroke(); } }
}

/* ---------- loop, sizing, camera ---------- */
function frame(time) {
  raf = 0; if (!motion()) { draw(); return; }
  const dt = Math.min(.05, lastFrame ? (time - lastFrame) / 1000 : .016); lastFrame = time;
  for (let left = dt; left > 1e-5; left -= 1 / 100) { const step = Math.min(left, 1 / 100); now += step; update(step); }
  if (now - userPanAt > 1 && selected && chars.has(selected) && followSel) { camTarget = clamp(chars.get(selected).x, camMin, camMax); }
  draw(); announce(); raf = requestAnimationFrame(frame);
}
let followSel = false;
function kick() { if (motion()) { if (!raf) { lastFrame = 0; raf = requestAnimationFrame(frame); } } else { if (raf) { cancelAnimationFrame(raf); raf = 0; } draw(); } }
function resize() {
  const box = canvas.getBoundingClientRect(); W = Math.max(1, box.width); H = Math.max(1, box.height); DPR = Math.min(2, window.devicePixelRatio || 1);
  canvas.width = Math.round(W * DPR); canvas.height = Math.round(H * DPR);
  scale = clamp(Math.min(H / 740, W / 430), .62, 1.7); groundY = Math.round(H * .8);
  layout(); for (const c of chars.values()) { const st = stations.get(c.ref); if (st) assign(c, st, false); }
  kick();
}
function announce() {
  const ch = chamberAt(cam); if (!ch || chambers.length < 2) { $("area").classList.remove("on"); return; }
  if (titleChamber !== ch.key) { titleChamber = ch.key; titleAt = now; $("area-name").textContent = ch.label; $("area-count").textContent = w().sessions(ch.count); $("area").classList.add("on"); }
  else if (now - titleAt > 3.2) $("area").classList.remove("on");
}
function reconcile() {
  layout();
  const seenRefs = new Set();
  let order = 0;
  for (const r of rows) { seenRefs.add(r.ref); const st = stations.get(r.ref); let c = chars.get(r.ref); const castIndex = (order++ * 5 + 1) % 12; if (!c) { c = makeChar(r, st); c.cast = castIndex; chars.set(r.ref, c); c.row = r; assign(c, st, true); c.alpha = placed && motion() ? 0 : 1; } else { c.row = r; assign(c, st, false); } }
  for (const [ref, c] of chars) if (!seenRefs.has(ref)) { dropEnemies(c); chars.delete(ref); }
  if (!placed && rows.length) { placed = true; const first = rows.find(r => r.focused) || rows.find(r => live(r) && status(r) === "waiting_you") || null; cam = camTarget = clamp(first ? stations.get(first.ref).x : (chambers[0].x0 + Math.min(chambers[0].x1, chambers[0].x0 + W / scale)) / 2, camMin, camMax); }
  const focus = rows.find(r => r.focused); if (focus && focus.ref !== lastFocus) { selected = focus.ref; followSel = true; camTarget = clamp(chars.get(focus.ref).x, camMin, camMax); } lastFocus = focus ? focus.ref : null;
  if (!motion()) { now += .001; for (const c of chars.values()) { c.sit = c.mode === "rest" ? 1 : 0; c.sword = c.swordTo; c.q = 1; c.alpha = 1; if (c.mode === "travel") { c.x = c.st.x; c.y = c.st.y; begin(c); } if (c.mode === "fight" && !c.enemy) { c.x = c.st.x - 14; c.y = c.st.y; c.face = 1; c.enemy = spawnEnemy(c); c.enemy.state = "roam"; c.enemy.x = c.st.x + 30; c.enemy.face = -1; c.sub = "engage"; } if (c.mode === "climb") { const sh = c.st.shaft, k = c.st.slot; c.vx = c.vy = 0; if (k === 1) { c.x = sh.left + 9; c.y = sh.top * .5; c.face = 1; c.sub = "cling"; c.ground = false; } else if (k === 2) { c.x = sh.right + 22; c.y = sh.top; c.face = -1; c.sub = "top"; c.ground = true; } else { c.x = c.st.x; c.y = 0; c.sub = "base"; c.ground = true; } } for (const q of c.scarf) { q.x = q.px = c.x - c.face * 3; q.y = q.py = c.y - 31; } } cam = camTarget; }
}
function panTo(ref) { const c = chars.get(ref); if (!c) return; followSel = true; userPanAt = -1e9; camTarget = clamp(c.x, camMin, camMax); if (!motion()) { cam = camTarget; draw(); } }

/* ---------- DOM: HUD, selection card, readable list ---------- */
const el = (tag, text, cls) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; if (cls) n.className = cls; return n; };
const freshLabel = r => ({ fresh: w().fresh, stale: w().stale, unknown: w().unknownFresh })[freshness(r)] || w().unknownFresh;
function render() {
  const t = w(), zh = context.language && context.language.startsWith("zh"), need = rows.filter(r => live(r) && status(r) === "waiting_you");
  document.documentElement.lang = zh ? "zh-Hant-TW" : "en"; document.title = t.name;
  document.body.classList.toggle("embedded", context.presentation === "embedded");
  $("title").textContent = zh ? "燈 淵" : "LANTERN DEEP"; $("count").textContent = t.sessions(rows.length);
  $("needs").textContent = need.length ? t.needs(need.length) : t.needsNone; $("needs").disabled = !need.length; $("needs").classList.toggle("hot", need.length > 0); $("needs").title = t.next;
  $("list").textContent = t.list; $("list").setAttribute("aria-pressed", String(listOpen)); $("close").setAttribute("aria-label", t.close); $("close").title = t.close;
  $("note").textContent = context.connection !== "connected" ? t.disconnected : context.reducedMotion ? t.still : ""; $("note").hidden = !$("note").textContent;
  $("empty").textContent = t.empty; $("empty").hidden = rows.length > 0; $("hint").textContent = t.hint;
  const card = $("card"), r = rows.find(x => x.ref === selected);
  card.hidden = !r;
  if (r) { $("card-title").textContent = r.title; $("card-meta").textContent = projectName(r) + " · " + t.states[status(r)] + " · " + freshLabel(r); const b = $("card-open"); b.textContent = t.open; b.disabled = busy || !live(r); $("card-error").textContent = openError; }
  const panel = $("roster"); panel.hidden = !listOpen;
  if (listOpen) {
    const keep = document.activeElement && document.activeElement.dataset ? document.activeElement.dataset.ref : null; panel.replaceChildren(el("h2", t.list));
    for (const ch of chambers) { panel.append(el("h3", ch.label)); for (const row of rows.filter(x => (x.project ? x.project.ref : "￿") === ch.key)) { const b = el("button", undefined, "row"); b.type = "button"; b.dataset.ref = row.ref; b.setAttribute("aria-pressed", String(selected === row.ref)); const dot = el("span", undefined, "dot"); dot.style.background = rgba(EYE[live(row) ? status(row) : "unknown"], 1); const text = el("span", undefined, "row-text"); text.append(el("span", row.title, "row-title"), el("span", t.states[status(row)] + " · " + freshLabel(row), "row-meta")); b.append(dot, text); b.onclick = () => { select(row.ref); panTo(row.ref); }; panel.append(b); } }
    if (keep) { const again = [...panel.querySelectorAll("button")].find(b => b.dataset.ref === keep); if (again) again.focus({ preventScroll: true }); }
  }
}
function select(ref) { selected = ref; openError = ""; followSel = !!ref; render(); if (!raf) draw(); }
function snapshot(data) {
  if (data.streamId === stream && data.revision <= revision) return;
  stream = data.streamId; revision = data.revision; rows = [...data.sessions].sort((a, b) => a.ref.localeCompare(b.ref, "en")); awaiting = context.connection !== "connected";
  if (selected && !rows.some(r => r.ref === selected)) selected = null;
  reconcile(); render(); kick();
}
async function openSelected() {
  const r = rows.find(x => x.ref === selected); if (!r || !view || busy || !live(r)) return;
  busy = true; openError = ""; render();
  try { await view.sessions.open(r.ref); } catch (e) { openError = w().cannotOpen + " · " + (e.code || "host_unavailable"); }
  busy = false; render();
}
let drag = null;
canvas.addEventListener("pointerdown", e => { drag = { x: e.clientX, cam: camTarget, moved: false, id: e.pointerId }; canvas.setPointerCapture(e.pointerId); });
canvas.addEventListener("pointermove", e => { if (!drag || drag.id !== e.pointerId) return; const dx = e.clientX - drag.x; if (Math.abs(dx) > 6) drag.moved = true; if (drag.moved) { followSel = false; userPanAt = now; camTarget = clamp(drag.cam - dx / scale, camMin, camMax); cam = lerp(cam, camTarget, .6); if (!raf) { cam = camTarget; draw(); } } });
canvas.addEventListener("pointerup", e => {
  if (!drag || drag.id !== e.pointerId) return; const moved = drag.moved; drag = null; if (moved) return;
  const box = canvas.getBoundingClientRect(), px = e.clientX - box.left, py = e.clientY - box.top; let best = null, bestD = 44 * Math.max(1, scale);
  for (const c of chars.values()) { const d = Math.hypot(c.sx - px, c.sy - 28 * scale - py); if (d < bestD) { bestD = d; best = c; } }
  select(best ? best.ref : null);
});
canvas.addEventListener("pointercancel", () => { drag = null; });
canvas.addEventListener("wheel", e => { e.preventDefault(); followSel = false; userPanAt = now; camTarget = clamp(camTarget + (Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY) / scale, camMin, camMax); if (!raf) { cam = camTarget; draw(); } }, { passive: false });
document.addEventListener("keydown", e => {
  if (e.key === "Escape") { e.preventDefault(); if (selected) select(null); else if (listOpen) { listOpen = false; render(); } else if (view) view.close().catch(() => {}); }
  else if (e.key === "ArrowLeft" || e.key === "ArrowRight") { if (document.activeElement && document.activeElement.closest("#roster")) return; followSel = false; userPanAt = now; camTarget = clamp(camTarget + (e.key === "ArrowLeft" ? -160 : 160), camMin, camMax); if (!raf) { cam = camTarget; draw(); } }
  else if (e.key === "Enter" && selected && document.activeElement === document.body) openSelected();
});
$("list").onclick = () => { listOpen = !listOpen; render(); };
$("close").onclick = () => { if (view) view.close().catch(() => {}); };
$("card-open").onclick = openSelected;
$("card-dismiss").onclick = () => select(null);
let needAt = -1;
$("needs").onclick = () => { const need = rows.filter(r => live(r) && status(r) === "waiting_you"); if (!need.length) return; needAt = (needAt + 1) % need.length; select(need[needAt].ref); panTo(need[needAt].ref); };
new ResizeObserver(resize).observe(canvas);
const moteRandom = rng(9);
for (let i = 0; i < 70; i++) motes.push({ x: moteRandom(), y: moteRandom(), z: .4 + moteRandom() * 1.1, r: 1.6 + moteRandom() * 3, a: .3 + moteRandom() * .5, vx: 4 + moteRandom() * 10, vy: 2 + moteRandom() * 8, w: .4 + moteRandom() * 1.2, p: moteRandom() * TAU });
render(); resize();
connectViewPlugin().then(async api => {
  view = api;
  view.onContext(next => { const was = context; context = next; if (next.connection !== "connected") awaiting = true; if (was.language !== next.language || was.connection !== next.connection || was.visible !== next.visible || was.reducedMotion !== next.reducedMotion) { titleChamber = null; reconcile(); } render(); kick(); });
  snapshot(await view.sessions.subscribe(snapshot));
}).catch(e => { $("note").hidden = false; $("note").textContent = w().error + " · " + (e.code || "host_unavailable"); });
})();
