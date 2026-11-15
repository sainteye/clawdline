// The Herdr Orrery: your agents as planets of a brass orrery, and the sun is
// you. Orbit radius is status (blocked innermost, pulled toward you; then
// done, working, and idle outermost). Each space owns an angular sector of
// the dial, so a planet keeps its place and only moves when its status does:
// then it glides to its new orbit (a native transition, nothing spins).
// The layout gives each copy { role }:
//   orrery    the dome: dial, sun, orbits, planets, cartouche, almanac
//   eyepiece  the rail over the live terminals: space / tab, machine, Settings
//   plaque    a pane's header, an engraved brass plaque

import { View, div, image, svg } from "gpui-kit";
import { h_flex, v_flex } from "gpui-base";
import { agents, spaces, panes, focused, props, run, settings, setAgentOrder } from "herdr";

// on_size_change reports on-screen px (already times the app zoom), while sizes
// set on elements get the zoom applied again, so work in design px throughout.
function designSize(s) {
  const z = Number(settings()?.zoom) || 1;
  return { width: s.width / z, height: s.height / z };
}


const ART = "mods/orrery/art";
const SERIF = "IM FELL English";
const CAPS = "IM FELL English SC";

const C = {
  ink: "#0a0f1a",
  walnut: "#1a130c",
  brass: "#d9b56a",
  brassMid: "#a8823f",
  brassDim: "#6b5530",
  plate: "#2a1f12",
  parchment: "#efe3c6",
  muted: "#bfae8a",
  faint: "#8c7d60",
  ember: "#e0563a",
  gold: "#e8c25a",
  blue: "#8fb4d6",
  pewter: "#a3a8ae",
};

// Orbits, as a share of the dial's radius. Two working orbits keep a busy
// sky from piling up; agents alternate between them by their hash.
const ORBIT = { blocked: 0.3, done: 0.45, working: [0.6, 0.71], idle: 0.84, unknown: 0.84 };
const RANK = { blocked: 0, done: 1, working: 2, idle: 3, unknown: 3 };

const WORD = {
  blocked: "awaiting you",
  done: "completed",
  working: "at work",
  idle: "at rest",
  unknown: "at rest",
};
const INK = { blocked: C.ember, done: C.gold, working: C.blue, idle: C.pewter, unknown: C.pewter };

function unit() {
  // Herdr already scales mod px by the app zoom (AGENTS.md), so sizes pass
  // through as designed; dividing by zoom made content outgrow its block.
  return (n) => n;
}

/** A stable 0..1 for a string, so a planet keeps its longitude. */
function hash(s) {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return ((h >>> 0) % 10007) / 10007;
}

function minutesSince(ms) {
  return ms ? Math.max(0, (Date.now() - ms) / 60000) : 0;
}

function since(ms) {
  if (!ms) return "";
  const m = Math.floor(minutesSince(ms));
  if (m < 1) return "this minute";
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  return h < 48 ? `${h} h ${m % 60} min` : `${Math.floor(h / 24)} days`;
}

const MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];
const DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

function ordinal(n) {
  const s = ["th", "st", "nd", "rd"], v = n % 100;
  return n + (s[(v - 20) % 10] || s[v] || s[0]);
}

function tabOf(tabId) {
  for (const s of spaces()) for (const t of s.tabs) if (t.id === tabId) return { space: s, tab: t };
  return null;
}

/** Every agent placed on the dial: orbit radius share, longitude in radians. */
function chart() {
  const all = spaces();
  const n = Math.max(1, all.length);
  const sector = (Math.PI * 2) / n;
  const index = new Map(all.map((s, i) => [s.id, i]));
  return agents().map((a) => {
    const st = RANK[a.status] !== undefined ? a.status : "unknown";
    const h = hash(a.address);
    const i = index.get(a.space) ?? 0;
    const lon = -Math.PI / 2 + i * sector + (0.14 + 0.72 * h) * sector;
    const orbit = st === "working" ? ORBIT.working[h < 0.5 ? 0 : 1] : ORBIT[st];
    return { a, st, lon, orbit, info: tabOf(a.tab) };
  });
}

function settingsButton(u, id) {
  return h_flex()
    .id(id)
    .flex_none()
    .gap(u(6))
    .px(u(10))
    .h(u(24))
    .items_center()
    .rounded(u(12))
    .bg(C.plate)
    .border(u(1))
    .border_color(C.brassMid)
    .text_color(C.brass)
    .font_family(CAPS)
    .text_size(u(13))
    .cursor_pointer()
    .hover((s) => s.bg("#3a2c18").border_color(C.brass))
    .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
    .on_click((_e, cx) => {
      cx.stop_propagation();
      run("app.settings", {});
    })
    .tooltip("Herdr settings")
    .child(svg(`${ART}/gear.svg`).size(u(13)).text_color(C.brass))
    .child(div().child("Settings"));
}

// ---- the dome

class Dome {
  init(_p, cx) {
    this.size = { width: 900, height: 900 };
    // Data, not animation: working planets grow with time at work, and the
    // almanac's date; both change slowly.
    cx.timer.every(60000, (cx) => cx.notify());
  }

  ring(cxp, cyp, r, color, width, dashed) {
    let el = div()
      .absolute()
      .left(cxp - r)
      .top(cyp - r)
      .w(r * 2)
      .h(r * 2)
      .rounded(r)
      .border(width)
      .border_color(color);
    if (dashed) el = el.border_dashed();
    return el;
  }

  planet(u, p, cxp, cyp, R, focusedAddr) {
    const { a, st, lon, orbit } = p;
    const working = st === "working";
    const mins = working ? minutesSince(p.info?.tab.working_since) : 0;
    const d = u(st === "blocked" ? 30 : st === "done" ? 30 : working ? Math.min(34, 20 + mins / 4) : 18);
    const r = orbit * R;
    const x = cxp + r * Math.cos(lon);
    const y = cyp + r * Math.sin(lon);
    const right = Math.cos(lon) >= -0.15;
    const art = st === "unknown" ? "idle" : st;
    const name = (a.tab_name || a.program?.name || "agent").toLowerCase();
    const mine = a.address === focusedAddr;
    const LW = u(170);

    const label = v_flex()
      .absolute()
      .top(d / 2 - u(15))
      .w(LW)
      .when(right, (el) => el.left(d + u(8)))
      .when(!right, (el) => el.left(-LW - u(8)).items_end())
      .child(div().font_family(CAPS).text_size(u(14)).text_color(mine ? C.parchment : C.muted).truncate().child(name))
      .child(
        div()
          .font_family(SERIF)
          .italic()
          .text_size(u(12))
          .text_color(INK[st])
          .child(`${a.space_name} · ${WORD[st]}${working && mins >= 1 ? `, ${since(p.info?.tab.working_since)}` : ""}`),
      );

    return div()
      .id(`orrery-planet-${a.address}`)
      .absolute()
      .left(x - d / 2)
      .top(y - d / 2)
      .w(d)
      .h(d)
      .transition("left", { duration: 900, easing: "ease-out" })
      .transition("top", { duration: 900, easing: "ease-out" })
      .cursor_pointer()
      .on_click((_e, _cx) => run("agent.focus", { terminal: a.address }))
      .tooltip(`${a.program?.name ?? "Agent"} in ${a.space_name} / ${a.tab_name}: ${WORD[st]}`)
      .when(st === "blocked", (el) =>
        el.child(image(`${ART}/halo-blocked.svg`).absolute().left(-d * 0.9).top(-d * 0.9).w(d * 2.8).h(d * 2.8)),
      )
      .when(mine, (el) => el.child(image(`${ART}/halo-focus.svg`).absolute().left(-d * 0.35).top(-d * 0.35).w(d * 1.7).h(d * 1.7)))
      .child(image(`${ART}/planet-${art}.svg`).absolute().left(0).top(0).size_full())
      .child(label);
  }

  sectorLabels(u, cxp, cyp, R) {
    const all = spaces();
    const n = Math.max(1, all.length);
    const sector = (Math.PI * 2) / n;
    return all.map((s, i) => {
      const mid = -Math.PI / 2 + (i + 0.5) * sector;
      const r = R * 0.965;
      const x = cxp + r * Math.cos(mid);
      const y = cyp + r * Math.sin(mid);
      const W = u(150);
      const tick = -Math.PI / 2 + i * sector;
      return [
        // the sector's boundary, a brass pin on the dial
        div()
          .absolute()
          .left(cxp + R * 0.92 * Math.cos(tick) - u(3))
          .top(cyp + R * 0.92 * Math.sin(tick) - u(3))
          .w(u(6))
          .h(u(6))
          .rounded(u(3))
          .bg(C.brassMid),
        v_flex()
          .id(`orrery-sector-${s.id}`)
          .absolute()
          .left(x - W / 2)
          .top(y - u(14))
          .w(W)
          .items_center()
          .cursor_pointer()
          .on_click((_e, _cx) => run("space.select", { id: s.id }))
          .child(
            div()
              .font_family(CAPS)
              .text_size(u(13))
              .text_color(s.active ? C.brass : C.faint)
              .truncate()
              .child(s.name),
          )
          .child(div().font_family(SERIF).italic().text_size(u(11)).text_color(C.faint).child(s.machine ? `on ${s.machine}` : "this machine")),
      ];
    }).flat();
  }

  cartouche(u) {
    const d = new Date();
    return v_flex()
      .absolute()
      .left(0)
      .top(u(18))
      .w_full()
      .items_center()
      .child(
        v_flex()
          .items_center()
          .px(u(28))
          .py(u(6))
          .border_t(u(1))
          .border_b(u(1))
          .border_color(C.brassMid)
          .child(div().font_family(CAPS).text_size(u(24)).text_color(C.brass).child("The Herdr Orrery"))
          .child(
            div()
              .font_family(SERIF)
              .italic()
              .text_size(u(13))
              .text_color(C.muted)
              .child(`agents in their courses, ${DAYS[d.getDay()]} the ${ordinal(d.getDate())} of ${MONTHS[d.getMonth()]}`),
          ),
      );
  }

  almanac(u, list) {
    const count = (s) => list.filter((p) => p.st === s).length;
    const next =
      list.filter((p) => p.st === "blocked").sort((x, y) => y.a.seq - x.a.seq)[0] ??
      list.filter((p) => p.st === "done").sort((x, y) => y.a.seq - x.a.seq)[0];
    const row = (label, n, color) =>
      h_flex()
        .gap(u(10))
        .items_center()
        .child(div().w(u(8)).h(u(8)).rounded(u(4)).bg(color))
        .child(div().flex_1().font_family(SERIF).text_size(u(14)).text_color(C.muted).child(label))
        .child(div().font_family(SERIF).text_size(u(15)).text_color(C.parchment).child(String(n)));
    return v_flex()
      .absolute()
      .left(u(22))
      .bottom(u(22))
      .w(u(250))
      .p(u(14))
      .gap(u(5))
      .bg("#1d150dE6")
      .border(u(1))
      .border_color(C.brassMid)
      .rounded(u(4))
      .child(div().font_family(CAPS).text_size(u(15)).text_color(C.brass).pb(u(4)).child("The Almanac"))
      .child(row("Awaiting you, inner orbit", count("blocked"), C.ember))
      .child(row("Completed", count("done"), C.gold))
      .child(row("At work", count("working"), C.blue))
      .child(row("At rest, outer orbit", count("idle") + count("unknown"), C.pewter))
      .child(div().h(u(1)).my(u(6)).bg(C.brassDim))
      .child(
        next
          ? v_flex()
              .id("orrery-next")
              .gap(u(2))
              .cursor_pointer()
              .hover((s) => s.opacity(0.85))
              .on_click((_e, _cx) => run("agent.focus", { terminal: next.a.address }))
              .child(div().font_family(SERIF).italic().text_size(u(12)).text_color(C.faint).child("Next to attend"))
              .child(
                div()
                  .font_family(CAPS)
                  .text_size(u(15))
                  .text_color(INK[next.st])
                  .truncate()
                  .child(`${next.a.space_name} · ${next.a.tab_name}`),
              )
          : div().font_family(SERIF).italic().text_size(u(13)).text_color(C.faint).child("All quiet in the heavens."),
      );
  }

  legend(u) {
    const line = (k, t) =>
      h_flex()
        .gap(u(8))
        .items_center()
        .child(image(`${ART}/planet-${k}.svg`).w(u(16)).h(u(16)))
        .child(div().font_family(SERIF).italic().text_size(u(12)).text_color(C.muted).child(t));
    return v_flex()
      .absolute()
      .right(u(22))
      .bottom(u(22))
      .gap(u(4))
      .items_end()
      .child(line("blocked", "innermost, pulled toward you: awaiting you"))
      .child(line("done", "second orbit: completed"))
      .child(line("working", "middle orbits, grows with time: at work"))
      .child(line("idle", "outermost: at rest"));
  }

  render(_cx) {
    const u = unit();
    const { width: W, height: H } = this.size;
    const cxp = W / 2;
    const cyp = H / 2 + u(18);
    const R = Math.max(120, Math.min(W / 2 - u(40), H / 2 - u(70)));
    const list = chart();
    setAgentOrder([...list].sort((x, y) => RANK[x.st] - RANK[y.st] || x.lon - y.lon).map((p) => p.a.address));
    const fa = focused()?.terminal;
    const blocked = list.filter((p) => p.st === "blocked").length;
    const sun = u(110);

    const orbitRings = [ORBIT.blocked, ORBIT.done, ...ORBIT.working, ORBIT.idle].map((k, i) =>
      this.ring(cxp, cyp, k * R, i === 0 && blocked ? "#e0563a66" : i % 2 ? "#a8823f55" : "#d9b56a40", 1, i === 2 || i === 3),
    );

    return div()
      .id("orrery-root")
      .relative()
      .size_full()
      .overflow_hidden()
      .on_size_change((size, cx) => {
        size = designSize(size);
        if (Math.abs(size.width - this.size.width) > 1 || Math.abs(size.height - this.size.height) > 1) {
          this.size = size;
          cx.notify();
        }
      })
      // the glass dome's reflection and the dial
      .child(
        div()
          .absolute()
          .left(cxp - R * 1.04)
          .top(cyp - R * 1.04)
          .w(R * 2.08)
          .h(R * 2.08)
          .rounded(R * 1.04)
          .bg("#16213380"),
      )
      .child(image(`${ART}/dial.svg`).absolute().left(cxp - R).top(cyp - R).w(R * 2).h(R * 2))
      .children(orbitRings)
      .children(this.sectorLabels(u, cxp, cyp, R))
      .child(image(`${ART}/sun.svg`).absolute().left(cxp - sun / 2).top(cyp - sun / 2).w(sun).h(sun))
      .child(
        v_flex()
          .absolute()
          .left(cxp - u(80))
          .top(cyp + sun / 2 + u(4))
          .w(u(160))
          .items_center()
          .child(div().font_family(CAPS).text_size(u(14)).text_color(C.brass).child("You"))
          .when(blocked > 0, (el) =>
            el.child(div().font_family(SERIF).italic().text_size(u(12)).text_color(C.ember).child(`${blocked} awaiting you`)),
          ),
      )
      .children(list.map((p) => this.planet(u, p, cxp, cyp, R, fa)))
      .child(this.cartouche(u))
      .child(this.almanac(u, list))
      .child(this.legend(u));
  }
}

// ---- the rail over the eyepiece

class Eyepiece {
  init() {}
  render(_cx) {
    const u = unit();
    const s = spaces().find((x) => x.active);
    const t = s?.tabs.find((x) => x.active);
    const n = panes().filter((p) => p.tab === t?.id).length;
    return h_flex()
      .id("orrery-eyepiece")
      .size_full()
      .px(u(16))
      .gap(u(12))
      .items_center()
      .border_b(u(1))
      .border_color(C.brassDim)
      .child(div().flex_none().font_family(CAPS).text_size(u(16)).text_color(C.brass).child("The Eyepiece"))
      .child(div().flex_none().w(u(1)).h(u(16)).bg(C.brassDim))
      .child(
        div()
          .flex_1()
          .min_w(0)
          .truncate()
          .font_family(SERIF)
          .italic()
          .text_size(u(14))
          .text_color(C.muted)
          .child(s ? `${s.name} / ${t?.name ?? ""}${s.machine ? `, on ${s.machine}` : ""} · ${n} ${n === 1 ? "glass" : "glasses"}` : "No space in view"),
      )
      .child(settingsButton(u, "orrery-settings"));
  }
}

// ---- a pane's engraved plaque

class Plaque {
  init() {}
  render(_cx) {
    const u = unit();
    const id = props()?.pane;
    const p = panes().find((x) => x.pane === id);
    if (!p) return div();
    const st = p.status && WORD[p.status] ? p.status : null;
    const lit = p.focused;
    const name = (p.program?.name || "Shell").toLowerCase();
    const title = p.title && p.title !== p.program?.name ? p.title.replace(/^[^\p{L}\p{N}]+/u, "") : "";
    const act = (key, word, fn) =>
      div()
        .id(`orrery-${key}-${id}`)
        .flex_none()
        .px(u(6))
        .font_family(CAPS)
        .text_size(u(12))
        .text_color(lit ? C.brass : C.faint)
        .cursor_pointer()
        .hover((s) => s.text_color(C.parchment))
        .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
        .on_click((_e, cx) => {
          cx.stop_propagation();
          fn();
        })
        .child(word);
    return h_flex()
      .id(`orrery-plaque-${id}`)
      .size_full()
      .px(u(10))
      .gap(u(8))
      .items_center()
      .bg(lit ? "#33261a" : "#1c150e")
      .border_b(u(1))
      .border_color(lit ? C.brass : C.brassDim)
      .on_click((e, _cx) => run(e.click_count >= 2 ? "pane.zoom" : "pane.focus", { pane: id }))
      .child(div().flex_none().w(u(6)).h(u(6)).rounded(u(3)).bg(st ? INK[st] : C.brassDim))
      .child(div().flex_none().font_family(CAPS).text_size(u(14)).text_color(lit ? C.brass : C.muted).child(name))
      .child(div().flex_1().min_w(0).truncate().font_family(SERIF).italic().text_size(u(13)).text_color(C.faint).child(title))
      .when(Boolean(st), (el) => el.child(div().flex_none().font_family(SERIF).italic().text_size(u(12)).text_color(INK[st]).child(WORD[st])))
      .child(act("split", "split", () => run("pane.split", { direction: "right", pane: id })))
      .child(act("zoom", p.zoomed ? "unzoom" : "zoom", () => run("pane.zoom", { pane: id })))
      .child(act("close", "close", () => run("pane.close", { pane: id })));
  }
}

const ROLES = { orrery: Dome, eyepiece: Eyepiece, plaque: Plaque };

export default class Orrery extends View {
  init(p, cx) {
    const pr = props() ?? p ?? {};
    const R = ROLES[pr.role ?? "orrery"];
    this.role = R ? new R() : null;
    this.role?.init?.(pr, cx);
  }

  render(cx) {
    if (!this.role) return div().child(`orrery: no role "${props()?.role}"`);
    return this.role.render(cx);
  }
}
