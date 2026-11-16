// 窗格头部 28px：怪兽头像（或砖头）+ 程序名 + 目录（~ 缩写）；等你时有标签；悬停出现分屏 / 放大 / 关闭
import { div, svg } from "gpui-kit";
import { h_flex } from "gpui-base";
import { panes, agents, props, run, home } from "herdr";
import { headIcon } from "./top.js";
import { BADGE } from "./roster.js";

function folder(cwd) {
  if (!cwd) return "";
  const h = home();
  return cwd === h ? "~" : cwd.startsWith(h + "/") ? "~" + cwd.slice(h.length) : cwd;
}

export function paneHeader(view, t) {
  const pane = props()?.pane;
  const p = panes().find((x) => x.pane === pane);
  if (!p) return div();
  const a = agents().find((x) => x.pane === pane);
  const name = ((p.program?.agent || p.program?.shell) && p.program.name) || p.title || p.program?.name || "shell";
  const hovered = view.hovered === pane;
  const button = (id, icon, command, args) => div().id(id).size(20).flex_none().flex().items_center().justify_center()
    .text_color(t.dim).cursor_pointer()
    .hover((s) => s.bg(t.hover).text_color(t.foreground))
    .on_click((_e, cx) => { cx.stop_propagation(); run(command, args); })
    .child(svg(`mods/pane/${icon}.svg`).size(14));
  return h_flex().id(`kj-head-${pane}`).size_full().px(6).gap(8).items_center()
    .bg(t.background).border_b(2).border_color(t.border).text_size(12)
    .on_hover((h, cx) => { view.hovered = h ? pane : null; cx.notify(); })
    .on_click(() => run("pane.focus", { pane }))
    .child(headIcon(t, a))
    .child(div().flex_none().max_w(260).truncate().text_color(t.foreground).child(name))
    .child(div().flex_1().min_w(0).truncate().text_color(t.dim).child(folder(p.cwd)))
    .when(p.status === "blocked", (el) => el.child(div().flex_none().px(4).bg(BADGE.blocked).text_color("#2b2620").child("等你")))
    .when(hovered, (el) => el
      .child(button(`kh-r-${pane}`, "split-right", "pane.split", { direction: "right", pane }))
      .child(button(`kh-d-${pane}`, "split-down", "pane.split", { direction: "down", pane }))
      .child(button(`kh-z-${pane}`, p.zoomed ? "unzoom" : "zoom", "pane.zoom", { pane }))
      .child(button(`kh-x-${pane}`, "close", "pane.close", { pane })));
}
