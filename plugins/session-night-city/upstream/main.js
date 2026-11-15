import { View, div, svg, image } from "gpui-kit";
import { h_flex, v_flex } from "gpui-base";
import { agents, spaces, panes, terminals, focused, props, run, theme,
  shared, setShared, setAgentOrder, window, exec } from "herdr";
import { scene, remember, confirm, ghosts, towers, skylineGeometry, agentTitle } from "./model.js";

const ART = "mods/night-city/";
const GLASS = { working: "#f1cf83", blocked: "#b63b50", done: "#c5f4dc",
  idle: "#3b5880", shell: "#756f70", mod: "#9cb8cb", dead: "#11182a", unavailable: "#29334b" };
const LABEL = { working: "working", blocked: "NEEDS YOU", done: "done, unseen",
  idle: "idle", shell: "shell", mod: "mod", dead: "dead, confirmed exit", unavailable: "unavailable" };
const readScene = () => scene(spaces(), panes(), agents(), terminals());
const identity = a => `${a.title || a.program?.name || a.address}\n${a.space_name || ""} / ${a.tab_name || ""}\n${a.address || a.pane}\n${LABEL[a.status] || "unreported"}`;
const icon = (p, size = 12) => !p?.icon ? div().w(size).h(size).child("·")
  : (p.tint ? svg(p.icon).text_color(p.color ?? "#192344") : image(p.icon)).w(size).h(size).flex_none();

export default class NightCity extends View {
  init(_props, cx) {
    this.size = { width: 1200, height: 240 };
    this.history = [];
    this.busy = false;
    this.lastCheck = 0;
    this.lastSaved = 0;
    if (props()?.role) return;
    try {
      const stored = JSON.parse(localStorage.getItem("night-city.history.v1") ?? "[]");
      if (Array.isArray(stored)) this.history = stored;
    } catch (_) { this.history = []; }
    cx.timer.every(10000, cx => {
      if (!window()?.active) return;
      this.update(readScene());
      this.checkExits(cx);
      cx.notify();
    });
  }

  update(data) {
    this.history = remember(this.history, data.list, [...data.rawByAddress.values()], Date.now());
    if (Date.now() - this.lastSaved >= 5000) this.save();
  }

  save() {
    localStorage.setItem("night-city.history.v1", JSON.stringify(this.history));
    this.lastSaved = Date.now();
  }

  checkExits(cx) {
    if (this.busy || !window()?.active || Date.now() - this.lastCheck < 10000 ||
      !this.history.some(a => a.missing)) return;
    this.busy = true;
    this.lastCheck = Date.now();
    cx.spawn(async cx => {
      let listing = null;
      try {
        // Inherit the app's core session from its launch environment.
        // This list includes connected machines with qualified addresses.
        const r = await exec("hc", ["ls", "--all", "--json"]);
        if (r.code === 0) listing = JSON.parse(r.stdout);
      } catch (_) { /* Missing, offline or malformed is unavailable, never proof of exit. */ }
      this.history = confirm(this.history, listing, Date.now());
      this.save();
      this.busy = false;
      cx.notify();
    });
  }

  motion() { return shared("night-city.still") !== true && window()?.active === true; }

  control(id, label, action) {
    return div().id(id).px(9).h(24).flex_none().rounded(5).flex().items_center()
      .text_size(11).text_color(theme().foreground).hover(s => s.bg(theme().raised))
      .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
      .on_click((_e, cx) => { cx.stop_propagation(); action(); }).child(label);
  }

  open(a) {
    if (a.status === "dead" || a.status === "unavailable") return;
    if (a.child) run("terminal.peek", { terminal: a.address });
    else if (a.address && !["shell", "mod"].includes(a.status)) run("agent.focus", { terminal: a.address });
    else if (a.pane) run("pane.focus", { pane: a.pane });
  }

  glass(a, width, data, small = false, k = 1) {
    const state = a.status ?? "shell", blocked = state === "blocked";
    const g = div().id(`glass-${a.address || a.pane}`).relative().w(width).h((small ? 10 : 18) * k).flex_none()
      .bg(GLASS[state] ?? GLASS.shell).border(1).border_color(a.focused ? "#fff3cd" : "#63708b")
      .tooltip(identity(a)).flex().items_center().justify_center()
      .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
      .on_click((_e, cx) => { cx.stop_propagation(); this.open(a); });
    if (state === "dead") g.child(div().text_size(Math.max(10, (small ? 10 : 15) * k)).text_color("#8490a5").child("X"));
    else if (state === "unavailable") g.child(div().text_size(10 * k).text_color("#c4ccdc").child("?"));
    else if (state === "shell") g.children([0, 1].map(i => div().absolute().left(i * width / 2).top(k).w(width / 2 - 2 * k).h((small ? 7 : 15) * k).bg("#535364")));
    else if (state === "mod") g.child(div().w(width - 4 * k).h(2 * k).bg("#e1eef0"));
    else if (!small) {
      const iconSize = Math.min(11 * k, width - 2 * k), typingSize = Math.max(10, 8 * k);
      g.child(icon(a.program, iconSize));
      if (state === "working" && width >= iconSize + typingSize + 2 * k) {
        const typing = div().text_size(typingSize).text_color("#3b3541");
        if (this.motion()) typing.frames(["_", "▏"], 600); else typing.child("_");
        g.child(typing);
      }
    }
    if (state === "done") g.child(div().absolute().right(0).bottom(0).size(4 * k).bg("#65be95"));
    if (blocked) {
      // The identity label stays static. Only a three-pixel lamp pulses.
      const lamp = div().id(`neon-lamp-${a.address}`).absolute().left(k).top(0).w(3 * k).h((small ? 8 : 16) * k).bg("#ffd3d6");
      if (this.motion() && data.blinking.has(a.address)) lamp.loop("opacity", { from: 0.45, to: 1, period: 1800 });
      g.child(lamp);
    }
    return g;
  }

  tower(s, data, k) {
    const t = theme(), blocked = data.blockers.filter(a => a.space === s.id);
    const tower = div().id(`tower-${s.id}`).relative().w(s.width * k).h((s.height + 54) * k).flex_none();
    if (blocked.length) {
      const beam = image(ART + "search-beam.svg").id(`beam-${s.id}`).absolute().left((s.width / 2 - 32) * k).top(0).w(64 * k).h(92 * k);
      if (this.motion()) beam.loop("opacity", { from: 0.35, to: 0.75, period: 3200 });
      tower.child(beam);
    }
    tower.child(image(ART + "tower-body.svg").absolute().left(0).top(54 * k).w(s.width * k).h(s.height * k).opacity(s.machine ? 0.65 : 1));
    const body = v_flex().absolute().left(0).top(54 * k).w(s.width * k).h(s.height * k).justify_between();
    const floors = v_flex().pt(12 * k).gap(0);
    for (const f of s.floors) {
      const cold = f.windows.some(a => a.status === "dead" && Date.now() - a.ended >= 600000);
      const floor = h_flex().id(`floor-${f.id}`).relative().h(f.height * k).w(s.width * k).px(7 * k).pb(3 * k).gap(4 * k).items_end().flex_none()
        .when(f.active && s.active, el => el.border_l(3 * k).border_color("#f0d99c"))
        .when(cold, el => el.bg("#080e20"))
        .tooltip(`${s.name} / ${f.name}${f.settled ? " (settled, blinds down)" : ""}`);
      if (f.settled) floor.child(div().absolute().left(0).top(0).w(5 * k).h(24 * k)
        .children([1, 6, 11, 16, 21].map(y => div().absolute().left(0).top(y * k).w(5 * k).h(2 * k).bg("#77829b"))));
      const width = k * Math.max(10, Math.min(24, (s.width - 22 - 4 * Math.max(0, f.windows.length - 1)) / Math.max(1, f.windows.length)));
      for (const a of f.windows) {
        const apartment = div().relative().w(width).h(18 * k).flex_none().child(this.glass(a, width, data, false, k));
        const kids = data.descendants.get(a.address) ?? [];
        if (kids.length) apartment.child(v_flex().absolute().left(0).bottom(23 * k).gap(2 * k)
          .children(kids.map(child => this.glass(child, Math.max(10 * k, Math.min(14 * k, width)), data, true, k))));
        floor.child(apartment);
      }
      // Never put an opaque blind over a blocked window.
      floor.on_click(() => { run("space.select", { id: s.id }); run("tab.select", { id: f.id }); });
      floors.child(floor);
    }
    body.child(floors).child(div().h(7 * k).mx(9 * k).mb(3 * k).bg(s.active ? "#efcf8f" : "#2c3a59"));
    tower.child(body);
    const sign = div().id(`roof-${s.id}`).absolute().left(0).top(31 * k).w(s.width * k).h(23 * k)
      .child(image(ART + "rooftop-sign.svg").absolute().size_full())
      .child(div().relative().px(8 * k).text_size(12 * k).text_color(s.machine ? t.muted : t.foreground).truncate().child(s.name))
      .tooltip(`${s.name}${s.machine ? " / " + s.machine : " / local"}`)
      .on_click(() => run("space.select", { id: s.id }));
    tower.child(sign);
    {
      const sign = div().id(`needs-sign-${s.id}`).absolute().top(8 * k).left(0).w(s.width * k).h(blocked.length ? 20 * k : 0).overflow_hidden()
        .child(image(ART + "neon-sign.svg").absolute().size_full())
        .child(div().relative().px(5 * k).text_size(10 * k).font_semibold().text_color("#ffe7ea").child(`NEEDS YOU · ${blocked.length}`));
      if (this.motion()) sign.transition("height", { duration: 300 });
      tower.child(sign);
    }
    return tower;
  }

  skyline(cx) {
    const data = readScene(), t = theme();
    this.update(data);
    this.checkExits(cx);
    setAgentOrder(data.list.filter(a => !a.child).map(a => a.address));
    const blocks = towers(data, this.history);
    const { scale: k, width, height } = skylineGeometry(blocks, this.size);
    const street = h_flex().id("night-city-street-scene").relative().w(width).h(height).justify_center().items_end().gap(24 * k).px(24).pb(12)
      .child(div().id("night-city-horizon").absolute().left(0).bottom(12).w(width).h(1).bg("#5c6e9659"))
      .children(Array.from({ length: 18 }, (_, i) => div().absolute().left(28 + (i * 137) % Math.max(1, width - 40))
        .top(6 + (i * 29) % 47).size(i % 4 === 0 ? 2 : 1).bg("#b6c5e580")));
    let crossed = false;
    for (const s of blocks) {
      if (s.machine && !crossed) {
        crossed = true;
        street.child(div().relative().w(92 * k).h(height - 24).flex_none().bg("#142b46")
          .child(div().absolute().bottom(28 * k).left(0).w(92 * k).h(4 * k).bg("#8d9ab5"))
          .child(div().absolute().bottom(43 * k).left(16 * k).text_size(10 * k).text_color(t.muted).child("BRIDGE"))
          .children([12, 32, 52, 72].map(y => div().absolute().left(9 * k).top(y * k).w(64 * k).h(1).bg("#7090a43d"))));
      }
      street.child(this.tower(s, data, k));
    }
    if (!blocks.length) street.child(div().text_size(14).text_color(t.muted).child("Your skyline lights up when a space opens."));
    const moon = h_flex().gap(7).items_center().child(image(ART + "moon.svg").size(24))
      .child(div().text_size(12).text_color(data.blockers.length ? t.blocked : t.foreground).child(`${data.blockers.length} need you`));
    return v_flex().id("night-city-sky").size_full().overflow_hidden()
      .on_size_change((size, cx) => { if (size.width !== this.size.width || size.height !== this.size.height) { this.size = size; cx.notify(); } })
      .child(h_flex().h(40).flex_none().px(24).gap(18).items_center().justify_between()
        .child(div().text_size(11).text_color(t.muted).child(`${data.spaces.length} towers   /   ${data.list.length} windows lit   /   ${ghosts(this.history).filter(a => a.status === "dead").length} exited`))
        .child(moon))
      .child(div().id("night-city-city-scroll").flex_1().min_h(0).overflow_x_scrollbar().overflow_y_scrollbar().child(street));
  }

  attention(data) {
    const t = theme(), list = data.blockers;
    return v_flex().size_full().bg(t.chrome).rounded(6).border(1).border_color(t.border).overflow_hidden()
      .child(h_flex().h(36).px(12).flex_none().items_center().justify_between()
        .child(div().text_size(12).font_semibold().text_color(t.foreground).child("NEEDS YOU"))
        .child(div().text_size(12).text_color(list.length ? t.blocked : t.muted).child(String(list.length))))
      .child(v_flex().id("night-city-attention-list").flex_1().min_h(0).overflow_y_scrollbar()
        .children(list.map(a => v_flex().id(`attention-${a.address}`).h(44).flex_none().px(10).gap(2)
          .bg("#361c2d").border_b(1).border_color("#683348").tooltip(identity(a))
          .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
          .on_click((_e, cx) => { cx.stop_propagation(); this.open(a); })
          .child(h_flex().gap(8).items_center().child(div().flex_1().min_w(0).truncate().text_size(12).font_semibold().text_color("#ffe7ea").child(a.title || a.program?.name || a.address))
            .child(div().flex_none().text_size(11).text_color("#ffbac0").child(a.address)))
          .child(div().truncate().text_size(10).text_color("#e9c4d1").child(`${a.space_name} / ${a.tab_name}${a.child ? " / child, peek" : ""}`))))
        .when(!list.length, el => el.child(div().p(12).text_size(12).text_color(t.muted).child("All windows quiet. Nothing needs you."))))
      .child(div().px(10).py(8).text_size(10).text_color(t.muted).child("Every space, every tab, including children."));
  }

  rail() {
    const t = theme(), p = props() ?? {}, data = readScene();
    if (p.part === "attention") return this.attention(data);
    if (p.part === "title") {
      const bar = h_flex().size_full().pl(window()?.platform === "macos" ? 88 : 18).pr(12).gap(8).items_center().bg(t.canvas)
        .child(div().text_size(12).font_semibold().text_color(t.foreground).child("N I G H T   C I T Y"))
        .child(div().flex_1())
        .child(this.control("night-city-motion", shared("night-city.still") === true ? "Still mode: on" : "Still mode: off",
          () => setShared("night-city.still", shared("night-city.still") !== true)));
      if (window()?.buttons) bar.children([["min", "_", "window.minimize"], ["max", "[]", "window.maximize"], ["close", "x", "window.close"]]
        .map(([id, text, command]) => this.control(`night-city-${id}`, text, () => run(command))));
      return bar;
    }
    const s = data.spaces.find(s => s.active), tab = s?.tabs.find(t => t.active);
    const a = data.list.find(a => a.address === focused()?.terminal) ?? data.list.find(a => a.focused);
    return h_flex().size_full().px(18).gap(14).items_center().bg(t.chrome).border_b(1).border_color(t.border)
      .child(div().flex_1().min_w(0).truncate().text_size(12).text_color(t.foreground).child(`${s?.name ?? "No space"} / ${tab?.name ?? "No tab"}${a ? ": " + a.title : ""}`))
      .child(div().flex_none().text_size(10).text_color(t.muted).child("WARM working   MINT done   BLUE idle   X exited"));
  }

  pane() {
    const p = panes().find(p => p.pane === props()?.pane), t = theme();
    if (!p) return div();
    const st = normalPane(p.status);
    const a = agents().find(a => a.address === p.terminal), raw = terminals().find(t => t.address === p.terminal);
    const title = agentTitle(a ?? { title: p.title, program: p.program, address: p.terminal || p.pane }, raw);
    return h_flex().id(`night-city-pane-${p.pane}`).size_full().px(10).gap(8).items_center().bg(t.chrome)
      .on_click(() => run("pane.focus", { pane: p.pane }))
      .child(icon(p.program, 14))
      .child(div().flex_1().min_w(0).truncate().text_size(12).text_color(t.foreground).child(title || p.mod || "Terminal"))
      .child(div().flex_none().text_size(10).text_color(t[st] ?? t.muted).child(LABEL[st] || (p.terminal ? "shell" : "mod")));
  }

  render(cx) {
    const role = props()?.role;
    if (role === "rail") return this.rail();
    if (role === "pane") return this.pane();
    return this.skyline(cx);
  }

  async close() { if (!props()?.role) this.save(); }
}

function normalPane(status) { return status === "error" ? "blocked" : status; }
