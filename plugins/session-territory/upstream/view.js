// Draws Territory: a header, the map, and a dock along its foot. `view` is the mod's View and `frame` says what to show,
// from frame.js. Colours come from `theme()` only. Everything from a file or a terminal is drawn as text.
// Motion is the app's own: .spring moves a dot, .transition fades heat, .loop pulses. None of them run this script.
import { PathBuilder, div } from "gpui-kit";
import { h_flex, v_flex } from "gpui-base";
import { settings } from "herdr";
import { CARD_W, DOCK_H, HEADER_H, HIT } from "./frame.js";

const GLIDE = { response: 520, damping: 0.86 };
const FADE = { duration: 1200, easing: "ease-out" };
const NOTE_WIDE = 760;
const FACTS_WIDE = 640;
const TILE_TEXT_W = 44;
const TILE_TEXT_H = 18;
const FRAME_TEXT_W = 44;

// The agents' colours, leaving out any the collision warning uses.
export const COLOURS = 10;
const paletteOf = (t) => [t.ansi4, t.ansi2, t.ansi5, t.ansi6, t.ansi3, t.ansi12, t.ansi10, t.ansi13, t.ansi14, t.ansi11].filter((c) => c && c !== t.blocked);
const solid = (color) => color.slice(0, 7);
const stop = (_e, cx) => cx.stop_propagation();
const mono = () => settings().font_family ?? "JetBrains Mono";

// A path's numbers are the app's own pixels, as `.w()` and `.left()` are, and the app scales them by the zoom itself.
function trail(line, color, width, height) {
  const path = PathBuilder.stroke(1.5).dash_array([3, 5]);
  line.points.forEach((p, i) => (i ? path.line_to(p.x, p.y) : path.move_to(p.x, p.y)));
  return window.paint_path(path.build(), `${solid(color)}80`).absolute().top(0).left(0).w(width).h(height);
}

function tileView(view, tile, frame, t, colorOf) {
  const heat = frame.heat.get(tile.key), visitor = frame.seen.get(tile.key), clash = frame.clash.has(tile.key);
  const picked = frame.card?.tile === tile, words = tile.w >= TILE_TEXT_W && tile.h >= TILE_TEXT_H;
  return div().id(tile.key).absolute().left(tile.x).top(tile.y).w(tile.w).h(tile.h).overflow_hidden().rounded(3).bg(t.raised)
    .cursor_pointer().hover((s) => s.bg(t.hover)).accessibility_label(tile.path || tile.label).on_click((_e, cx) => view.pressTile(tile.key, cx))
    .when(Boolean(heat), (e) => e.child(div().id(`heat-${tile.key}`).absolute().left(0).top(0).w(tile.w).h(tile.h).bg(solid(colorOf(heat.agent)))
      .opacity(0.14 + 0.76 * heat.level).transition("opacity", FADE)))
    .when(!heat && Boolean(visitor), (e) => e.child(div().absolute().left(0).top(0).w(tile.w).h(tile.h).bg(solid(colorOf(visitor))).opacity(0.14)))
    .when(clash, (e) => e.child(div().absolute().left(0).top(0).w(tile.w).h(tile.h).bg(t.blocked).loop("opacity", { from: 0.3, to: 1, period: 900 })))
    .when(clash || picked, (e) => e.child(div().absolute().left(0).top(0).w(tile.w).h(tile.h).rounded(3).border(2).border_color(clash ? t.blocked : t.foreground)))
    .when(words, (e) => e.child(div().absolute().left(6).top(3).w(tile.w - 10).truncate().text_size(11)
      .text_color(heat || clash ? t.foreground : t.muted).child(tile.label)));
}

function folderView(block, t) {
  return div().absolute().left(block.x).top(block.y).w(block.w).h(block.h).rounded(4).bg(t.surface)
    .when(block.w >= FRAME_TEXT_W, (e) => e.child(div().absolute().left(6).top(1).w(block.w - 10).truncate().text_size(11).font_medium().text_color(t.muted).child(block.label)));
}

// A halo and a core. A working agent's halo breathes, a blocked one's beats in the warning colour, and anyone else's sits
// still, so a quiet map has nothing to redraw.
function dotView(view, dot, size, t, colorOf) {
  const color = colorOf(dot.key), halo = size * 2.8, ring = size * 1.7, middle = (s) => HIT / 2 - s / 2;
  const working = dot.status === "working", blocked = dot.status === "blocked", done = dot.status === "done";
  const docked = dot.where === "dock", core = docked ? size * 0.7 : dot.sub ? size * 0.75 : size;
  const fill = dot.readable ? color : t.dim;
  return div().id(`dot-${dot.key}`).absolute().w(HIT).h(HIT).flex().items_center().justify_center().rounded_full().cursor_pointer()
    .left(dot.x - HIT / 2).top(dot.y - HIT / 2).spring("left", GLIDE).spring("top", GLIDE)
    .accessibility_label(dot.name).on_mouse_down("left", stop).on_click(() => view.pressDot(dot.key))
    .when((working || blocked) && !docked, (e) => e
      .child(div().absolute().left(middle(halo)).top(middle(halo)).size(halo).rounded_full().bg(blocked ? t.blocked : color)
        .loop("opacity", blocked ? { from: 0.2, to: 0.75, period: 800 } : { from: 0.14, to: 0.4, period: 2200 }))
      .child(div().absolute().left(middle(ring)).top(middle(ring)).size(ring).rounded_full().bg(blocked ? t.blocked : color).opacity(0.45)))
    .child(div().size(core).flex_none().rounded_full().bg(fill).border(2).border_color(blocked ? t.blocked : done ? t.done : t.foreground)
      .when(!working && !blocked, (e) => e.opacity(0.65)))
    .child(h_flex().absolute().left(dot.labelLeft).top(dot.labelTop).w(dot.labelWidth).when(!docked, (e) => e.justify_center())
      .child(div().min_w(0).px(5).rounded(4).truncate().bg(docked ? t.canvas : `${solid(t.canvas)}d9`).text_size(12).font_medium()
        .text_color(blocked ? t.blocked : t.foreground).child(dot.name))
      // Why it waits in the dock, quieter than the name, so a row of them reads as names.
      .when(docked, (e) => e.gap(1).child(div().flex_none().text_size(12).text_color(t.muted).child(dot.word))));
}

function cardRow(view, row, t, colorOf) {
  return h_flex().id(`card-${row.agent}`).h(28).px(12).gap(8).items_center().cursor_pointer().hover((s) => s.bg(t.hover))
    .on_mouse_down("left", stop).on_click(() => view.pressDot(row.agent))
    .child(div().size(10).flex_none().rounded_full().bg(colorOf(row.agent)))
    .child(div().flex_1().min_w(0).truncate().text_size(12).font_medium().child(row.name))
    .child(div().flex_none().text_size(12).text_color(t.muted).child(row.when));
}

function cardView(view, card, t, colorOf) {
  return v_flex().id("card").absolute().left(card.x).top(card.y).w(CARD_W).rounded(8).bg(t.popover).border(1).border_color(t.input).overflow_hidden()
    .on_mouse_down("left", stop).on_click((_e, cx) => cx.stop_propagation())
    .child(v_flex().flex_none().h(58).px(12).justify_center().gap(2)
      .child(div().truncate().font_family(mono()).text_size(12).font_medium().child(card.title))
      .child(div().truncate().text_size(12).text_color(t.muted).child(card.note)))
    .children(card.rows.length
      ? card.rows.map((row) => cardRow(view, row, t, colorOf))
      : [h_flex().h(28).px(12).items_center().text_size(12).text_color(t.muted).child("No agent here in the last 15 minutes")]);
}

function emptyNote(note, width, zone, t) {
  return v_flex().absolute().left(0).top(0).w(width).h(zone).items_center().justify_center().gap(4)
    .child(div().text_size(15).font_semibold().child(note.title))
    .when(Boolean(note.detail), (e) => e.child(div().max_w(width - 40).truncate().text_size(12).text_color(t.muted).child(note.detail)));
}

function dockRow(view, frame, width, t) {
  const words = view.problem ?? view.seeing(width < NOTE_WIDE);
  return h_flex().absolute().left(0).top(frame.zone).w(width).h(DOCK_H).pr(14).items_center().justify_end().gap(10)
    .when(frame.more > 0, (e) => e.child(div().flex_none().text_size(12).text_color(t.muted).child(`+${frame.more}`)))
    .child(div().min_w(0).truncate().text_size(12).text_color(view.problem ? t.blocked : t.dim).child(words));
}

function mapView(view, frame, width, height, t, colorOf) {
  const zone = frame.zone, map = frame.map, size = Math.max(11, Math.min(17, Math.round(width / 85)));
  return div().id("map").relative().w(width).h(height - HEADER_H).flex_none().overflow_hidden().on_mouse_down("left", stop)
    .children((map?.folders ?? []).map((block) => folderView(block, t)))
    .children((map?.tiles ?? []).map((tile) => tileView(view, tile, frame, t, colorOf)))
    .when(!map?.tiles.length, (e) => e.child(emptyNote(view.note(), width, zone, t)))
    .children(frame.trails.map((line) => trail(line, colorOf(line.agent), width, zone)))
    .child(dockRow(view, frame, width, t))
    .children(frame.dots.map((dot) => dotView(view, dot, size, t, colorOf)))
    .when(Boolean(frame.card), (e) => e.child(cardView(view, frame.card, t, colorOf)));
}

function clashLine(view, frame, t) {
  return h_flex().id("clash").flex_1().min_w(0).h(28).px(10).gap(8).items_center().rounded(6).bg(t.blocked).text_color(t.background).cursor_pointer()
    .on_mouse_down("left", stop).on_click((_e, cx) => view.pressTile(frame.firstClash, cx))
    .child(div().size(8).flex_none().rounded_full().bg(t.background).loop("opacity", { from: 0.25, to: 1, period: 900 }))
    .child(div().min_w(0).truncate().text_size(13).font_semibold().child(frame.line));
}

function header(view, frame, width, inset, folded, t) {
  return h_flex().flex_none().w(width).h(HEADER_H).pl(inset).pr(10).gap(12).items_center()
    .child(div().flex_none().text_size(15).font_semibold().child(view.title()))
    .when(width >= FACTS_WIDE, (e) => e.child(div().flex_none().text_size(12).text_color(t.muted).child(view.facts())))
    .child(frame.line ? clashLine(view, frame, t) : div().flex_1())
    .child(h_flex().id("fold").flex_none().h(26).px(10).items_center().rounded(6).border(1).border_color(t.border).cursor_pointer()
      .text_size(12).text_color(t.muted).hover((s) => s.bg(t.hover)).on_mouse_down("left", stop).on_click(() => view.toggleFold())
      .child(folded ? "Map" : "Terminals"));
}

export function territory(view, frame, { width, height, inset, folded, showMap, onSize }, t) {
  const colors = paletteOf(t), colorOf = (key) => colors[(view.slots.get(key) ?? 0) % colors.length] ?? t.accent;
  return v_flex().id("root").size_full().bg(t.canvas).text_color(t.foreground).on_size_change(onSize)
    .child(header(view, frame, width, inset, folded, t))
    .when(showMap, (e) => e.child(mapView(view, frame, width, height, t, colorOf)));
}
