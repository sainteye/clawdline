// La place (bottom strip): Valbrume seen from above, isometric pixel art (direction A, v3).
// Left, the RTS-style command panel: minimap (click to move the camera) and the selected unit.
// Right, the village: the static world (ground, buildings, props, shadows, night lights) is baked
// by tools/bake_world.py into a few big images, day and night, exactly as the approved mocks;
// only the units, their markers, front cut-outs (depth), glows and smoke are live.
//
// Every agent is a hero of its class: working = `work` loop at its class building; idle = `rest`
// at the campfire; blocked = `idle` + flashing `!` + red ring + speech bubble; done = `idle` + gold
// `?` + halo. When the spot changes the hero walks there along the road (walk_dl / walk_ur and their
// mirrors), at its own speed. Frame-by-frame animation uses one shared data timer (~10 fps), only
// while a visible unit needs it; movement itself is a native `left`/`top` transition. Heroes whose
// frames aren't synced yet (tools/sync_heroes.py) stand still and breathe natively.

import { div, image } from "gpui-kit";
import { h_flex, v_flex } from "gpui-base";
import { agents, spaces, run, setAgentOrder, props, focused, layout, shared, setShared } from "herdr";
import { UI, C, hash, clip, heroOf, heroTitle, guards, QUEST, sky, stop } from "./kit.js";
import { WORLD, TILES, FRONTS } from "./monde-data.js";
import { HEROES } from "./heros-data.js";
import { SPOTS, FIRE, REST, ROAD, BUILDINGS, market, SMOKE, GLOWS, LISIERE } from "./lieux.js";
import { t as tr, detectLang } from "./i18n.js";
import { Activites } from "./activite.js";
import { Ambiance, behind } from "./ambiance.js";
import { DEMO_LOG } from "./demo.js";
import { commandPanel, MINI_INSET } from "./commande.js";
import { EXT } from "./terre-data.js";

const ART = "mods/world-of-codecraft/art";
const MONDE = `${ART}/monde`;
const PANEL = 262; // command panel width (lines up with the quest board)
const RANK = { blocked: 0, done: 1, working: 2, idle: 3, garde: 4 };
const LOG_KEY = "wocc.chronicle";
const TICK = 100; // ms: the shared frame timer (10 fps)
const ONLY_SELECTED = false; // if CPU is too high: frame-animate only the selected unit

// Where each class works.
const WORK = { mage: "tour", artificier: "atelier", guerrier: "caserne", pretre: "temple", rodeur: "grange", voleur: "etal1" };
const RECRUIT = {
  tour: { argv: ["claude"], get what() { return tr("recruit").tour; } },
  atelier: { argv: ["codex"], get what() { return tr("recruit").atelier; } },
  caserne: { argv: null, get what() { return tr("recruit").caserne; } },
};
const RACE_EN = { elfe: "elf", orque: "orc", humain: "human", "mort-vivant": "undead", nain: "dwarf" };
const CLS_EN = { mage: "mage", artificier: "artificer", guerrier: "warrior", pretre: "priest", rodeur: "ranger", voleur: "rogue" };
const RACE_FR = Object.fromEntries(Object.entries(RACE_EN).map(([f, e]) => [e, f]));
const CLS_FR = Object.fromEntries(Object.entries(CLS_EN).map(([f, e]) => [e, f]));
const placeName = (cls) => BUILDINGS[WORK[cls]]?.nom ?? market();

// ------------------------------------------------------------------ the road

const dist = (a, b) => Math.hypot(b[0] - a[0], b[1] - a[1]);
const ROAD_S = ROAD.reduce((acc, p, i) => (acc.push(i ? acc[i - 1] + dist(ROAD[i - 1], p) : 0), acc), []);

/** The closest point of the road to p: { p, s (arc length), i (segment) }. */
function onRoad(p) {
  let best = null;
  for (let i = 0; i < ROAD.length - 1; i++) {
    const a = ROAD[i], b = ROAD[i + 1];
    const dx = b[0] - a[0], dy = b[1] - a[1], L2 = dx * dx + dy * dy;
    const t = Math.max(0, Math.min(1, ((p[0] - a[0]) * dx + (p[1] - a[1]) * dy) / L2));
    const q = [a[0] + t * dx, a[1] + t * dy];
    const d = dist(p, q);
    if (!best || d < best.d) best = { d, p: q, s: ROAD_S[i] + t * Math.sqrt(L2), i };
  }
  return best;
}

/** Points from a to b: straight when close, else onto the road, along it, and off it. */
function route(a, b) {
  if (dist(a, b) < 170) return [a, b];
  const ra = onRoad(a), rb = onRoad(b);
  const pts = [a, ra.p];
  if (ra.s < rb.s) for (let i = ra.i + 1; i <= rb.i; i++) pts.push(ROAD[i]);
  else for (let i = ra.i; i > rb.i; i--) pts.push(ROAD[i]);
  pts.push(rb.p, b);
  return pts.filter((p, i) => i === 0 || dist(p, pts[i - 1]) > 2);
}

/** Walk anim for a step: towards the viewer = walk_dl (mirrored: walk_dr), away = walk_ur (walk_ul). */
const walkAnim = (dx, dy) => (dy > 0 || (dy === 0 && dx < 0) ? (dx <= 0 ? "walk_dl" : "walk_dr") : (dx >= 0 ? "walk_ur" : "walk_ul"));

export default class Place {
  init(_p, cx) {
    detectLang(cx);
    this.born = Date.now();
    this.gotoAt = shared("wocc.goto")?.at; // ignore a minimap click left over from an earlier session
    this.w = 1400;
    this.camX = null;
    this.lastFocus = null;
    this.now = new Date();
    this.prev = null;
    this.mob = new Map(); // address -> { x, y, goal, legs: [{ a, b, t0, t1 }] }
    this.timer = null;
    this.sig = "";
    this.act = new Activites(); // what each hero does while working (Claude Code hook)
    this.amb = new Ambiance(); // village life: walkers, animals, treasury, fair (only when animated)
    this.live = true; // the « Animations » toggle (shared wocc.anim): off = still images, no walk, no scenes
    try { this.log = JSON.parse(localStorage.getItem(LOG_KEY) ?? "[]"); } catch { this.log = []; }
    cx.timer.every(60_000, (cx) => { this.now = new Date(); cx.notify(); });
  }

  // ---------------------------------------------------------------- chronicle

  say(text, kind, address) {
    try { this.log = JSON.parse(localStorage.getItem(LOG_KEY) ?? "[]"); } catch {} // the other village may have written
    this.log.unshift({ at: Date.now(), text, kind, address });
    this.log = this.log.slice(0, 40);
    try { localStorage.setItem(LOG_KEY, JSON.stringify(this.log)); } catch {}
  }

  watch(list) {
    const now = new Map(list.map((a) => [a.address, a]));
    if (this.prev) {
      for (const a of list) {
        const was = this.prev.get(a.address);
        const h = heroOf(a.address, a.program);
        const where = a.status === "idle" ? tr("campfire") : placeName(h.cls);
        const quest = a.tab_name && !/^\d+$/.test(a.tab_name) ? tr("quoted", clip(a.tab_name, 34)) : "";
        if (!was) this.say(`[${where}] ${tr("arrives", heroTitle(h), h.prog)}`, a.status, a.address);
        else if (was.status !== a.status) {
          if (a.status === "idle" && was.status === "done") continue;
          const verb = QUEST[a.status].fr + (a.status === "working" || a.status === "done" ? quest : "");
          this.say(`[${where}] ${heroTitle(h)} (${h.prog}) ${verb}`, a.status, a.address);
        }
      }
      for (const [addr, a] of this.prev) if (!now.has(addr)) this.say(`[${tr("gate")}] ${tr("leaves", heroTitle(heroOf(addr, a.program)))}`, "idle", null);
    } else if (!this.log.length) {
      const b = list.filter((a) => a.status === "blocked").length;
      this.say(`[${tr("crier")}] ${tr("hearYe", list.length, b)}`, "idle", null);
    }
    this.prev = new Map(list.map((a) => [a.address, { status: a.status, program: a.program }]));
  }

  // ---------------------------------------------------------------- units and movement

  /** Where a unit is at time t, and the leg it walks (null when standing). */
  pos(m, t) {
    for (const l of m.legs) {
      if (t < l.t0) return { x: l.a[0], y: l.a[1], leg: l };
      if (t < l.t1) {
        const k = (t - l.t0) / (l.t1 - l.t0);
        return { x: l.a[0] + (l.b[0] - l.a[0]) * k, y: l.a[1] + (l.b[1] - l.a[1]) * k, leg: l };
      }
    }
    return { x: m.x, y: m.y, leg: null };
  }

  /** Every unit with its goal spot, current position and leg; starts walks when a goal changes. */
  units(list, t) {
    const taken = new Set();
    const goals = new Map();
    const want = list.map((a) => {
      const h = a.demoHero ? { ...heroOf(a.address, a.program), race: RACE_FR[a.demoHero.split("-")[0]], cls: CLS_FR[a.demoHero.split("-")[1]] } : heroOf(a.address, a.program);
      const scout = a.status === "working" && this.act.scene(a.address) === "eclaire" && LISIERE[WORK[h.cls]];
      const group = a.status === "idle" ? "repos" : scout ? `lisiere-${WORK[h.cls]}` : WORK[h.cls];
      return { a, h, group, spots: group === "repos" ? REST : scout ? LISIERE[WORK[h.cls]] : SPOTS[group] };
    });
    // keep a unit on its current spot when it stays in the same group (no shuffling)
    for (const u of want) {
      const m = this.mob.get(u.a.address);
      if (m && m.group === u.group && !taken.has(m.goal.join())) { taken.add(m.goal.join()); goals.set(u.a.address, m.goal); }
    }
    for (const u of want) {
      if (goals.has(u.a.address)) continue;
      let g = u.spots.find((p) => !taken.has(p.join()));
      if (!g) { const k = taken.size; const base = u.spots[k % u.spots.length]; g = [base[0] + 18 * (1 + Math.floor(k / u.spots.length)), base[1] + 6]; }
      taken.add(g.join());
      goals.set(u.a.address, g);
    }
    const out = [];
    const alive = new Set();
    for (const u of want) {
      const addr = u.a.address;
      alive.add(addr);
      const goal = goals.get(addr);
      let m = this.mob.get(addr);
      if (!m) { m = { x: goal[0], y: goal[1], goal, group: u.group, legs: [] }; this.mob.set(addr, m); }
      else if (m.goal.join() !== goal.join() && !this.live) { m.x = goal[0]; m.y = goal[1]; m.goal = goal; m.group = u.group; m.legs = []; }
      else if (m.goal.join() !== goal.join()) {
        const here = this.pos(m, t);
        const speed = HEROES[`${RACE_EN[u.h.race]}-${CLS_EN[u.h.cls]}`]?.speed ?? 42;
        const pts = route([here.x, here.y], goal);
        m.legs = [];
        let t0 = t;
        for (let i = 1; i < pts.length; i++) {
          const t1 = t0 + (dist(pts[i - 1], pts[i]) / speed) * 1000;
          m.legs.push({ a: pts[i - 1], b: pts[i], t0, t1 });
          t0 = t1;
        }
        m.x = goal[0]; m.y = goal[1]; m.goal = goal; m.group = u.group;
      }
      if (m.legs.length && t >= m.legs[m.legs.length - 1].t1) m.legs = [];
      const p = this.pos(m, t);
      out.push({ a: u.a, h: u.h, x: p.x, y: p.y, leg: p.leg, key: `${RACE_EN[u.h.race]}-${CLS_EN[u.h.cls]}` });
    }
    for (const k of [...this.mob.keys()]) if (!alive.has(k)) this.mob.delete(k);
    return out;
  }

  /** The anim a unit plays now, and its sprite info (falls back to the standing sprite). */
  anim(u) {
    const H = HEROES[u.key];
    let name;
    if (u.leg) name = walkAnim(u.leg.b[0] - u.leg.a[0], u.leg.b[1] - u.leg.a[1]);
    else name = (u.a.status === "working" && this.act.anim(u.a.address)) || (u.a.status === "working" ? "work" : u.a.status === "idle" ? "rest" : "idle");
    const info = H?.anims?.[name];
    if (info) return { name, info, frames: true };
    return { name: "stand", info: H?.stand ?? { w: 50, h: 85, ax: 25, ay: 83 }, frames: false };
  }

  unit(u, selected, night, t, animate) {
    const { a, h } = u;
    const st = a.status;
    const an = this.anim(u);
    const { w, h: hh, ax, ay } = an.info;
    const suffix = night ? "-n" : "";
    // the feet point moves (natively, linear, over the rest of the leg); everything hangs from it
    let el = div().id(`unite-${a.address}`).absolute().left(Math.round(u.leg ? u.leg.b[0] : u.x)).top(Math.round(u.leg ? u.leg.b[1] : u.y)).w(0).h(0);
    // always declared (16 ms when standing): GPUI animates only changes made after the transition exists,
    // so adding it on the render that sets the first leg's target made that leg jump
    {
      const dur = u.leg ? Math.max(16, u.leg.t1 - t) : 16;
      el = el.transition("left", { duration: dur, easing: "linear" }).transition("top", { duration: dur, easing: "linear" });
    }
    // rings under the feet
    if (selected) el = el.child(image(`${MONDE}/ring-selected.png`).absolute().left(-30).top(-17).w(60).h(33));
    if (st === "blocked" && !u.leg) el = el.child(image(`${MONDE}/ring-blocked.png`).absolute().left(-30).top(-17).w(60).h(33).when(this.live, (e) => e.loop("opacity", { from: 0.2, to: 1, period: 460 })));
    if (st === "done" && !u.leg) el = el.child(image(`${MONDE}/ring-done.png`).absolute().left(-30).top(-17).w(60).h(33).when(this.live, (e) => e.loop("opacity", { from: 0.45, to: 1, period: 1100 })));

    // the sprite: every frame of the anim stacked (preloaded), only the current one shown
    let body;
    if (an.frames) {
      const dir = `${ART}/heros/${u.key}/${an.name}`;
      const k = animate ? Math.floor(((t + (hash(a.address) % 997)) / 1000) * an.info.fps) % an.info.n : 0;
      body = div().absolute().left(-ax).top(-ay).w(w).h(hh)
        .children(Array.from({ length: an.info.n }, (_, i) =>
          image(`${dir}/${String(i).padStart(2, "0")}${suffix}.png`).absolute().left(0).top(0).w(w).h(hh).opacity(i === k ? 1 : 0)));
    } else {
      // no frames yet: the standing sprite, breathing (1 px) or bobbing while it walks
      body = div().absolute().left(-ax).top(-ay).w(w).h(hh)
        .child(image(`${ART}/heros/${u.key}/stand${suffix}.png`).absolute().left(0).top(0).w(w).h(hh)
          .when(this.live, (e) => e.loop("top", u.leg ? { from: 0, to: -2, period: 380 } : { from: 0, to: -1, period: 2600 + (hash(a.address) % 900) })));
    }
    el = el.child(body);

    // hit box, tooltip, click
    el = el.child(div().id(`clic-${a.address}`).absolute().left(-ax).top(-ay).w(w).h(hh).cursor_pointer()
      .tooltip(tr("unitTip", heroTitle(h), h.prog, `${a.space_name ?? ""} › ${a.tab_name ?? ""}`, QUEST[st]?.mot ?? tr("onGuardTerm")))
      .on_mouse_down("left", stop)
      .on_click(() => (a.guard ? run("pane.focus", { pane: a.pane }) : run("agent.focus", { terminal: a.address }))));

    const head = -ay;
    if (st === "blocked" && !u.leg) {
      el = el.child(image(`${MONDE}/m-blocked.png`).absolute().left(-12).top(head - 42).w(24).h(38)
        .when(this.live, (e) => e.loop("top", { from: head - 42, to: head - 50, period: 500 })).when(this.live, (e) => e.loop("opacity", { from: 0.35, to: 1, period: 520 })));
      el = el.child(div().absolute().left(18).top(head - 4).h(22).px(6).rounded(3).bg(C.parch).border_2().border_color(C.woodDark)
        .flex().items_center().whitespace_nowrap().font_family(UI).text_size(15).text_color(C.wax)
        .when(this.live, (e) => e.frames(tr("help"), 900)).when(!this.live, (e) => e.child(tr("help")[0])));
    }
    if (st === "done" && !u.leg) {
      el = el.child(image(`${MONDE}/m-done.png`).absolute().left(-14).top(head - 42).w(28).h(38).when(this.live, (e) => e.loop("top", { from: head - 40, to: head - 48, period: 900 })));
      el = el.child(div().absolute().left(-22).top(head + 10).size(3).bg(C.goldLight).when(this.live, (e) => e.loop("top", { from: head + 30, to: head - 6, period: 1000 })).when(this.live, (e) => e.loop("opacity", { from: 1, to: 0, period: 1000 })));
      el = el.child(div().absolute().left(20).top(head + 20).size(3).bg("#fff2c0").when(this.live, (e) => e.loop("top", { from: head + 40, to: head + 2, period: 1300 })).when(this.live, (e) => e.loop("opacity", { from: 1, to: 0, period: 1300 })));
    }
    if (st === "idle" && !u.leg) el = el.child(image(`${ART}/z.svg`).absolute().left(14).top(head).size(8).when(this.live, (e) => e.loop("top", { from: head + 10, to: head - 14, period: 2600 })).when(this.live, (e) => e.loop("opacity", { from: 1, to: 0, period: 2600 })));
    if (!u.leg && !a.guard) el = el.children(this.act.layers(u, head, night, t));
    if (selected) {
      el = el.child(div().absolute().left(-15).top(head - 9).w(30).h(5).bg("#1b1420").border_1().border_color("#000000")
        .child(div().h_full().w(st === "blocked" ? 8 : 28).bg(st === "blocked" ? "#e5685a" : "#7dff6a")));
    }
    return el;
  }

  // ---------------------------------------------------------------- the frame timer

  /** One shared data timer while something needs frames or walks; it re-renders only when a frame changes. */
  tick(cx, need) {
    if (need && !this.timer) {
      this.timer = cx.timer.every(TICK, (cx) => {
        const s = this.frameSig(Date.now());
        if (s !== this.sig) { this.sig = s; cx.notify(); }
      });
    } else if (!need && this.timer) { this.timer.cancel(); this.timer = null; }
  }

  /** What the animated units show at time t (their frames and legs): unchanged = no redraw. */
  frameSig(t) {
    let s = "";
    for (const u of this.anims ?? []) {
      const m = this.mob.get(u.addr);
      if (!m) continue;
      const p = this.pos(m, t);
      const k = u.fps ? Math.floor(((t + u.ph) / 1000) * u.fps) % u.n : 0;
      s += `${u.addr}:${k}:${p.leg ? p.leg.t0 : "-"};`;
    }
    return s;
  }

  hidden() {
    const id = props()?.immersion ? "place-immersion" : "place"; // two villages in the layout, one shown
    const find = (n) => (!n || typeof n !== "object" ? null : n.id === id ? n : (n.children ?? []).map(find).find(Boolean) ?? null);
    try { return Boolean(find(layout())?.hidden); } catch { return false; }
  }

  // ---------------------------------------------------------------- world parts

  smoke() {
    const out = [];
    if (!this.live) return out;
    SMOKE.forEach(([sx, sy], n) => {
      for (let k = 0; k < 3; k++) {
        const x0 = sx - 4, y0 = sy - 8, p = 2800 + k * 520 + n * 130;
        out.push(div().absolute().left(x0).top(y0).size(7 + k * 2).rounded_full().bg(k === 1 ? "#b9b4bfbb" : "#d8d4dcbb")
          .when(this.live, (e) => e.loop("top", { from: y0, to: y0 - 44 - k * 10, period: p, easing: "linear" }))
          .when(this.live, (e) => e.loop("left", { from: x0, to: x0 + 10 + k * 6, period: p, easing: "linear" }))
          .when(this.live, (e) => e.loop("opacity", { from: 0.85, to: 0, period: p, easing: "linear" })));
      }
    });
    return out;
  }

  glows(nightK) {
    if (nightK <= 0) return [];
    return GLOWS.map((g) => {
      const s = g.size;
      return div().absolute().left(g.at[0] - s / 2).top(g.at[1] - s / 2).size(s).rounded_full().bg(`${g.color}22`)
        .opacity(nightK).when(this.live, (e) => e.loop("opacity", { from: 0.35 * nightK, to: nightK, period: g.period }));
    });
  }

  hitboxes() {
    return Object.entries(BUILDINGS).map(([k, b]) => {
      const r = RECRUIT[k];
      let el = div().id(`bat-${k}`).absolute().left(b.box[0]).top(b.box[1]).w(b.box[2]).h(b.box[3])
        .tooltip(r ? tr("recruitTip", b.nom, r.what) : b.nom);
      if (r) el = el.on_click((e) => { if (e.click_count === 2) run("tab.new", r.argv ? { argv: r.argv } : {}); });
      return el;
    });
  }

  /** A front cut-out (the object's own baked pixels), crossfaded like the world. */
  front(f, nightK) {
    let el = div().absolute().left(f.x).top(f.y).w(f.w).h(f.h);
    if (nightK < 1) el = el.child(image(`${MONDE}/avant-${f.key}-jour.png`).absolute().left(0).top(0).w(f.w).h(f.h));
    if (nightK > 0) el = el.child(image(`${MONDE}/avant-${f.key}-nuit.png`).absolute().left(0).top(0).w(f.w).h(f.h).opacity(nightK));
    return el;
  }

  // ---------------------------------------------------------------- the panel

  /** Immersive layout: the command panel is another block (commande.js, role "commande"). Tell it where the
   *  camera and the units are (store key wocc.view, only when it changes, at most every 300 ms) and obey its
   *  minimap clicks (wocc.goto). */
  bridge(units, vw) {
    const g = shared("wocc.goto");
    if (g && g.at !== this.gotoAt) {
      this.gotoAt = g.at;
      this.camX = g.x - vw / 2;
      if (typeof g.y === "number") this.camY = g.y;
      this.manual = true;
    }
    const view = { cam: Math.round(this.cam(vw)), vw: Math.round(vw), dots: units.map((u) => [Math.round(u.x / 8) * 8, Math.round(u.y / 8) * 8, u.a.status]) };
    const key = JSON.stringify(view);
    const now = Date.now();
    if (key !== this.viewKey && now - (this.viewAt ?? 0) > 300) { this.viewKey = key; this.viewAt = now; setShared("wocc.view", view); }
  }

  minimap(units, vw) {
    const mw = PANEL - MINI_INSET, k = mw / WORLD.w, mh = Math.round(WORLD.h * k);
    const col = { blocked: C.quest, done: C.gold, working: "#7dff6a", idle: "#cfc8da", garde: "#9aa4b1" };
    const view = (vw / WORLD.w) * mw;
    const vx = (this.cam(vw) / WORLD.w) * mw;
    return div().id("minicarte").relative().w(mw).h(mh).flex_none().cursor_pointer()
      .tooltip(tr("mapTip"))
      .on_mouse_down("left", (e, cx) => {
        const lx = e.local_position?.x;
        if (typeof lx === "number") { this.camX = (lx / mw) * WORLD.w - vw / 2; this.manual = true; cx.notify(); }
        cx.stop_propagation();
      })
      .child(image(`${MONDE}/minicarte.png`).absolute().left(0).top(0).w(mw).h(mh))
      .children(units.map((u) => {
        let d = div().absolute().left(u.x * k - 2).top(u.y * k - 3).size(4).bg(col[u.a.status] ?? "#ffffff").border_1().border_color("#000000");
        if (u.a.status === "blocked") d = d.when(this.live, (e) => e.loop("opacity", { from: 0.2, to: 1, period: 420 }));
        return d;
      }))
      .child(div().absolute().left(vx).top(0).w(view).h(mh).border_1().border_color("#ffffff"));
  }

  /** Immersive layout: the world y at the top of the view, kept inside the painted bands. */
  camV() {
    const h = this.h ?? WORLD.h, top = -EXT.nord, bottom = WORLD.h + EXT.sud;
    if (h >= bottom - top) return (top + bottom - h) / 2;
    const c = this.camY ?? (WORLD.h / 2 - 40 - h / 2);
    return Math.max(top, Math.min(bottom - h, c));
  }

  cam(vw) {
    const max = Math.max(0, WORLD.w - vw);
    const c = this.camX ?? (WORLD.w / 2 - vw / 2);
    return Math.max(0, Math.min(max, c));
  }

  chronicle() {
    const tone = { blocked: C.quest, done: C.gold, working: "#8fd16a", idle: "#cfc8da" };
    return v_flex().absolute().left(10).top(8).gap(2).children((props()?.demo ? DEMO_LOG : this.log).slice(0, 4).map((e, i) => {
      const tag = e.text.match(/^\[[^\]]+\]/)?.[0] ?? "";
      const d = new Date(e.at);
      return h_flex().id(`ev-${i}-${e.at}`).gap(5).px(6).h(20).items_center().rounded(2).bg("#0b0810bb").whitespace_nowrap().cursor_pointer()
        .opacity(i === 0 ? 1 : 0.9 - i * 0.15)
        .on_mouse_down("left", stop)
        .on_click(() => { if (e.address) run("agent.focus", { terminal: e.address }); })
        .font_family(UI).text_size(16)
        .child(div().text_color("#8a8296").child(`${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`))
        .child(div().text_color(tone[e.kind] ?? C.gold).child(tag))
        .child(div().text_color(C.cream).child(e.text.slice(tag.length).trim()));
    }));
  }

  // ---------------------------------------------------------------- demo (props.demo)

  demo() {
    const sp = spaces()[0];
    // [status, program, hero]: the animated heroes first, then fallbacks
    const D = [["working", "claude", "orc-mage"], ["blocked", "shell", "human-warrior"], ["done", "opencode", "elf-ranger"], ["idle", "codex", "dwarf-artificer"],
      ["working", "gemini", "human-priest"], ["working", "claude", "elf-mage"], ["blocked", "aider", "undead-rogue"], ["working", "codex", "orc-artificer"],
      ["working", "claude", "human-mage"], ["working", "claude", "undead-mage"]];
    // demo activities (props.demo): combat with a blow every 2.5 s and a failed tool every 20 s, a scout with
    // two familiars, a meditation, a thinker; the scene changes immediately (no file, no anti-zapping wait)
    const n = Date.now();
    const ACT = {
      0: { act: "code", tache: 1, coups: Math.floor(n / 2500) % 400, subs: 0, err: 1, errT: n - (n % 20000), t: n },
      5: { act: "eclaire", tache: 1, subs: 2, t: n },
      7: { act: "test", tache: 1, essai: { ok: Math.floor(n / 12000) % 2 === 0, t: n - (n % 12000) }, commitT: n - (n % 15000), pushT: n - (n % 30000), t: n },
      8: { act: "pense", tache: 1, subs: 1, t: n },
      9: { act: "code", tache: 7, coups: Math.floor(n / 3100) % 400, subs: 0, errTache: 3, t: n }, // the troll boss
      4: { act: "medite", tache: 1, subs: 0, t: n },
    };
    const cyc = props()?.demo === "marche" ? Math.floor(Date.now() / 30000) % 2 : 0;
    return D.map(([status, key, hero], i) => ({
      address: `demo-${i}-${key}`, space: sp?.id, space_name: sp?.name, tab_name: tr("demoTabs")[i],
      status: cyc && i < 4 ? (status === "idle" ? "working" : "idle") : status, seq: i, focused: false, demoHero: hero, demoAct: ACT[i],
      program: { key: key === "shell" ? "sh" : key, name: key, shell: false } }));
  }

  render(cx) {
    const t = Date.now();
    const at = new Date(this.now);
    if (typeof props()?.heure === "number") at.setHours(props().heure, 0); // demo: another time of day
    const sk = sky(at);
    const nightK = sk.phase === "nuit" ? 1 : sk.phase === "jour" ? 0 : 0.55; // night layer over the day one
    const nightHeroes = sk.phase === "nuit";

    this.live = shared("wocc.anim") !== false;
    if (!this.live) for (const m of this.mob.values()) m.legs = [];
    let list = agents().map((a) => ({ ...a }));
    if (props()?.demo) list = this.demo(); // screenshots: only the demo heroes, none of the person's agents
    else if (this.hidden()) { // the other village keeps the chronicle; start afresh when shown again
      this.prev = null;
      try { this.log = JSON.parse(localStorage.getItem(LOG_KEY) ?? "[]"); } catch {}
    } else if (shared("wocc.lang") || Date.now() - this.born > 4000) this.watch(list); // chronicle once the language is known
    const order = [...list].sort((a, b) => RANK[a.status] - RANK[b.status] || b.seq - a.seq);
    if (!props()?.demo) setAgentOrder(order.map((a) => a.address));
    const gs = guards().slice(0, 4).map((p) => ({ address: p.terminal, pane: p.pane, guard: true, status: "garde", program: p.program, tab_name: p.title, focused: p.focused }));
    this.act.watch(cx, order, this.hidden(), !this.live);
    const units = this.units([...order, ...gs], t);

    // the selected unit = the focused agent (or the focused shell)
    const f = focused();
    const sel = units.find((u) => u.a.address === (f?.agent ?? f?.terminal)) ?? units.find((u) => u.a.focused) ?? null;
    // immersive layout: the command panel is its own block (role "commande"), the village takes the full width
    const imm = Boolean(props()?.immersion);
    const vw = Math.max(200, this.w - (imm ? 0 : PANEL));
    if (imm) this.bridge(units, vw);
    if (sel && sel.a.address !== this.lastFocus) {
      this.lastFocus = sel.a.address;
      this.manual = false; // a new selection takes the camera back
      this.camX = sel.x - vw / 2; // glide to the selected unit
      if (imm) this.camY = sel.y - 60 - (this.h ?? WORLD.h) / 2; // and bring the village back into view
    } else if (sel?.leg && !this.manual) this.camX = sel.x - vw / 2; // and follow it while it walks
    const camX = this.cam(vw);

    // frame animation: visible units with frames, or any walking unit
    const hidden = this.hidden();
    const visible = (u) => u.x > camX - 80 && u.x < camX + vw + 80;
    this.anims = [];
    for (const u of units) {
      const an = this.anim(u);
      const animate = this.live && !hidden && visible(u) && an.frames && (!ONLY_SELECTED || u === sel);
      u.animate = animate;
      if (animate || u.leg) this.anims.push({ addr: u.a.address, fps: animate ? an.info.fps : 0, n: an.info.n ?? 1, ph: hash(u.a.address) % 997 });
    }
    this.tick(cx, !hidden && this.anims.length > 0);
    this.sig = this.frameSig(t);

    // depth: units by feet y (+3, like the compositor), front cut-outs above the units behind them
    const things = units.map((u) => ({ z: u.y + 3, el: this.unit(u, u === sel, nightHeroes, t, u.animate) }));
    for (const u of units) { const c = this.act.creature(u, nightHeroes); if (c) things.push(c); }
    const working = list.filter((a) => a.status === "working").length;
    this.amb.watch(cx, this.live && !hidden, { phase: sk.phase });
    const life = this.live ? this.amb.things(nightHeroes, sk.phase, working) : [];
    things.push(...life);
    for (const fr of FRONTS) {
      const over = units.some((u) => {
        const an = this.anim(u);
        const x0 = u.x - an.info.ax, y0 = u.y - an.info.ay;
        return u.y + 3 < fr.z && x0 < fr.x + fr.w && x0 + an.info.w > fr.x && y0 < fr.y + fr.h && u.y > fr.y;
      });
      const walkerBehind = life.some((th) => th.box && behind(th.box).includes(fr));
      if (over || walkerBehind) things.push({ z: fr.z, el: this.front(fr, nightK) });
    }
    things.sort((p, q) => p.z - q.z);

    // immersive layout: the lands around the village (tools/bake_lands.py, terre-data.js) and a vertical camera too
    const worldTop = imm ? -this.camV() : Math.round(((this.h ?? WORLD.h) - WORLD.h) / 2);
    const glide = this.drag ? { duration: 16, easing: "linear" } : { duration: 650, easing: "ease-in-out" };
    const band = (name, y) => [
      ...EXT.tiles.map((k) => image(`${MONDE}/${name}-${k}-jour.png`).absolute().left(k * 1000).top(y).w(1000).h(EXT[name])),
      ...(nightK > 0 ? EXT.tiles.map((k) => image(`${MONDE}/${name}-${k}-nuit.png`).absolute().left(k * 1000).top(y).w(1000).h(EXT[name]).opacity(nightK)) : []),
    ];

    // the camera glides (650 ms) except while the view is dragged, where it follows the pointer
    const layer = (id) => div().id(id).absolute().top(worldTop).left(-camX).w(WORLD.w).h(WORLD.h)
      .transition("left", glide).transition("top", glide);
    const world = layer("monde")
      .when(imm, (el) => el.children(band("nord", -EXT.nord)).children(band("sud", WORLD.h)))
      .children(TILES.map((tl) => image(`${MONDE}/jour-${tl.k}.png`).absolute().left(tl.x).top(0).w(tl.w).h(WORLD.h)))
      .when(nightK > 0, (el) => el.children(TILES.map((tl) => image(`${MONDE}/nuit-${tl.k}.png`).absolute().left(tl.x).top(0).w(tl.w).h(WORLD.h).opacity(nightK))))
      .children(this.glows(nightK))
      .children(this.hitboxes())
      .children(things.map((th) => th.el))
      .children(this.smoke());

    const panel = imm ? null : commandPanel(PANEL, this.minimap(units, vw), sel, { on: this.live, toggle: () => setShared("wocc.anim", !this.live) });

    return h_flex().id("place").size_full().bg(C.slate)
      .on_size_change((s, cx) => {
        if (Math.abs(s.width - this.w) > 10 || s.height !== this.h) { this.w = s.width; this.h = s.height; cx.notify(); }
      })
      .when(!imm, (el) => el.child(panel))
      .child(div().id("vue").flex_1().h_full().relative().overflow_hidden().bg("#3f6b2c")
        .on_scroll_wheel((e, cx) => {
          if (imm) { this.camX = this.cam(vw) - e.delta.x; this.camY = this.camV() - e.delta.y; }
          else this.camX = this.cam(vw) - (Math.abs(e.delta.x) > Math.abs(e.delta.y) ? e.delta.x : e.delta.y);
          this.manual = true;
          cx.notify();
        })
        // click and drag the village to pan (units and buttons stop the press, so their clicks still work)
        .when(!this.drag, (e) => e.cursor_grab()).when(Boolean(this.drag), (e) => e.cursor_grabbing())
        .on_mouse_down("left", (e, cx) => { this.drag = { x0: e.position.x, y0: e.position.y, cam0: this.cam(vw), camY0: this.camV() }; cx.notify(); })
        .on_mouse_move((e, cx) => {
          if (!this.drag) return;
          const x = Math.max(0, Math.min(Math.max(0, WORLD.w - vw), this.drag.cam0 - (e.position.x - this.drag.x0)));
          const y = imm ? this.drag.camY0 - (e.position.y - this.drag.y0) : 0;
          if (Math.abs(x - this.cam(vw)) < 1 && (!imm || Math.abs(y - this.camV()) < 1)) return;
          this.camX = x;
          if (imm) this.camY = y;
          this.manual = true;
          cx.notify();
        })
        .on_mouse_up("left", (_e, cx) => { if (this.drag) { this.drag = null; cx.notify(); } })
        .on_hover((over, cx) => { if (!over && this.drag) { this.drag = null; cx.notify(); } })
        .child(world)
        .child(this.chronicle()));
  }
}
