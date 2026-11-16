// Kaiju City 布局的部件分发，由 props() 决定画哪个：
//   city：左上城市沙盘（space = 楼，Agent = 怪兽），见 city.js
//   roster：左下怪兽名册卡片；侧栏收起时画窄栏，见 roster.js
//   top：顶栏胶囊 + tab 胶囊
//   news：右下怪兽快讯
//   窗格头部：props 带 pane 时

import { View, div } from "gpui-kit";
import { props, theme, nextCommand } from "herdr";
import { Damage } from "./state.js";
import { city } from "./city.js";
import { roster, toggleSidebar } from "./roster.js";
import { top } from "./top.js";
import { paneHeader } from "./pane.js";
import { news } from "./news.js";

export default class Kaiju extends View {
  init(_props, cx) {
    this.damage = new Damage();
    // 只有名册这一份接快捷键命令（keys.json: "cmd-b": "kaiju.toggle"），避免多份抢命令
    if (props()?.part === "roster") {
      cx.spawn(async (cx) => {
        for (;;) {
          const c = await nextCommand();
          if (c.endsWith("toggle")) toggleSidebar();
          cx.notify();
        }
      });
    }
  }

  render(cx) {
    const t = theme();
    if (props()?.pane) return paneHeader(this, t);
    const part = props()?.part;
    if (part === "city") return city(this, t, cx);
    if (part === "roster") return roster(this, t, cx);
    if (part === "top") return top(this, t);
    if (part === "news") return news(this, t, cx);
    return div();
  }
}
