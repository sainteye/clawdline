// 怪兽快讯：后台 Agent 拆完了（done）或在等你（blocked）时，右下角浮出新闻条
//   规则同 veggie 收获卡片：首次见到不算；最多显示 3 条；点一条跳过去并移除；
//   自己切过去、状态变化或 Agent 消失时自动移除；× 清空；没有条目不渲染
import { div, image, svg } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { agents, props, run } from "herdr";
import { asset, assign, monsterOf } from "./state.js";

const HOT = (s) => s === "done" || s === "blocked";
const MAX = 3;
const RED = "#d9573f";

// 根据 agents() 的变化更新 view.news（每次 render 调用）
function track(view) {
  const n = (view.news ??= { items: [], last: new Map() });
  const list = agents();
  const live = new Map(list.map((a) => [a.address, a]));
  for (const a of list) {
    const prev = n.last.get(a.address);
    if (prev && !HOT(prev) && HOT(a.status) && !a.focused) {
      n.items = n.items.filter((it) => it.address !== a.address);
      n.items.unshift({ address: a.address, pane: a.pane, fresh: true });
    }
  }
  n.last = new Map(list.map((a) => [a.address, a.status]));
  n.items = n.items.filter((it) => {
    const a = live.get(it.address);
    return a && !a.focused && HOT(a.status);
  });
  return n;
}

// props().demo：截图检查用的假条目 [{address, status, space_name, program: {name}, monster}]
export function news(view, t, cx) {
  const demo = props()?.demo;
  const n = demo ? { items: demo } : track(view);
  if (!n.items.length) return div();
  const live = new Map(agents().map((a) => [a.address, a]));
  const mids = demo ? null : assign(agents());
  // 新条目先画高一点，下一帧落回原位，弹簧带出回弹
  if (!demo && n.items.some((it) => it.fresh)) {
    cx.timer.after(40, (cx) => { n.items.forEach((it) => (it.fresh = false)); cx.notify(); });
  }

  const row = (it) => {
    const a = live.get(it.address) ?? it;
    const m = monsterOf(demo ? it.monster : mids.get(it.address));
    const where = a.space_name ?? "";
    const title = a.status === "blocked" ? `${m.name}在 ${where} 区等你` : `${m.name}拆完了 ${where} 区`;
    return h_flex().id(`news-${it.address}`).h(48).px(6).gap(8).items_center().cursor_pointer()
      .hover((s) => s.bg(t.hover))
      .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
      .on_click((_e, cx) => {
        if (demo) return;
        n.items = n.items.filter((x) => x.address !== it.address);
        run("pane.focus", { pane: it.pane });
        cx.notify();
      })
      .child(div().relative().size(32).flex_none()
        .child(div().absolute().left(0).top(it.fresh ? -12 : 0).spring("top", { response: 420, damping: 0.42 })
          .child(image(asset(t, m.face)).size(32))))
      .child(v_flex().flex_1().min_w(0)
        .child(h_flex().min_w(0).text_size(12)
          .child(div().flex_none().text_color(RED).child("快讯｜"))
          .child(div().min_w(0).truncate().text_color(t.foreground).child(title)))
        .child(div().truncate().text_size(12).text_color(t.dim).child(a.program?.name ?? a.app ?? "Agent")))
      .when(a.status === "blocked", (el) => el.child(div().size(6).flex_none().bg("#d8a548")));
  };

  const more = n.items.length - MAX;
  // 浮层外框（底色、边框、圆角、阴影）由 Herdr 画；这里只画内容：顶部 3px 红色像素条 + 条目
  return v_flex().w(286)
    .child(div().h(3).mx(6).bg(RED))
    .child(v_flex().p(4).gap(2)
      .child(h_flex().h(20).px(6).items_center().justify_between()
        .child(div().text_size(12).text_color(t.dim).child("怪兽快讯"))
        .child(div().id("news-clear").p(3).cursor_pointer().text_color(t.dim)
          .hover((s) => s.bg(t.hover).text_color(t.foreground))
          .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
          .on_click((_e, cx) => { if (!demo) { n.items = []; cx.notify(); } })
          .child(svg("mods/workspace-chrome/x.svg").size(12))))
      .children(n.items.slice(0, MAX).map(row))
      .when(more > 0, (el) => el.child(div().px(6).pb(2).text_size(12).text_color(t.dim).child(`还有 ${more} 条`))));
}
