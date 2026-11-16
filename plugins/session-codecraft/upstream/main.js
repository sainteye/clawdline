// World of Codecraft (village of Valbrume): a pixel-art fantasy village square for Herdr. One mod, several roles (props.role):
//   fronton  the top beam: crest, district and quest, alerts, canonical hour, clock
//   tableau  the left column: districts (spaces) as pennants, the quest board (tabs)
//   place    the bottom strip: the village square, heroes, fountains, chronicle
//   pane     a pane's header: a stitched leather band with the hero working in it
//   commande the command panel as its own block (immersive layout: under the board, beside the terminal)
//   rail     one side of the carved frame around the terminals (props.side: top, bottom, left, right)

import { View, div } from "gpui-kit";
import { props } from "herdr";
import Fronton from "./fronton.js";
import Tableau from "./tableau.js";
import Place from "./place.js";
import PaneHead from "./pane.js";
import { Commande } from "./commande.js";
import { rail } from "./ui.js";

class Rail {
  init(pr) { this.side = pr.side ?? "top"; }
  render() { return rail(this.side); }
}

const ROLES = { fronton: Fronton, tableau: Tableau, place: Place, pane: PaneHead, rail: Rail, commande: Commande };

export default class Valbrume extends View {
  init(p, cx) {
    const pr = props() ?? p ?? {};
    const R = ROLES[pr.role] ?? (pr.pane ? PaneHead : null);
    this.role = R ? new R() : null;
    this.role?.init?.(pr, cx);
    // No role (herdr-app check): every role at once, so the check covers them all.
    if (!this.role) this.all = Object.values(ROLES).map((K) => { const r = new K(); r.init?.(pr, cx); return r; });
  }

  render(cx) {
    if (!this.role) return div().flex().flex_col().size_full().children(this.all.map((r) => div().h(240).child(r.render(cx))));
    return this.role.render(cx);
  }
}
