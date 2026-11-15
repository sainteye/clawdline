// Draws the board. It only reads what main.js hands it. Yellow on black is the point of this layout, so the colors
// are its own and don't follow the theme.

import { div, svg } from "gpui-kit";
import { h_flex, v_flex } from "gpui-base";
import { SIDE, columnsOf } from "./logic.js";

const FONT = "JetBrains Mono";
const INK = "#050505"; // behind the cells, and the hairline
const CELL = "#191917";
const FLAP = "#2b2a24"; // a falling flap catches the light
const YELLOW = "#ffd21f";
const AMBER = "#c9a214"; // rows that matter less
const DIM = "#927c16"; // rows that are only waiting
const SHEEN = "#ffffff0a"; // the top half of every cell
const PRESS = "#2a2406"; // behind a row under the pointer

const inkOf = (row) => {
  const remark = row.flight?.remark;
  if (remark === "AT GATE") return DIM;
  if (remark === "DEPARTED" || remark === "CONNECTING") return AMBER;
  return YELLOW;
};

const stop = (_e, cx) => cx.stop_propagation();

function face(size, ch) {
  return h_flex().w(size.cell).h(size.cellH).flex_none().items_center().justify_center().when(ch !== " ", (el) => el.child(ch));
}

// A flap on its way down: the new letter's top half over the old one's bottom half.
function falling(size, top, bottom, paper) {
  const half = size.half ?? Math.floor(size.cellH / 2);
  return div().relative().w(size.cell).h(size.cellH).flex_none().rounded(2).bg(paper).overflow_hidden()
    .child(div().absolute().left(0).top(0).w(size.cell).h(half).bg(FLAP).overflow_hidden().child(face(size, top)))
    .child(div().absolute().left(0).top(half).w(size.cell).h(size.cellH - half).overflow_hidden()
      .child(div().absolute().left(0).top(-half).child(face(size, bottom))));
}

function cell(size, glyph, paper) {
  if (glyph.top !== glyph.bottom) return falling(size, glyph.top, glyph.bottom, paper);
  return face(size, glyph.top).rounded(2).bg(paper);
}

// One physical row of the board. The sheen runs a column at a time and the hairline the whole row, so a cell at rest
// costs one element.
function boardRow(v, i) {
  const { shape, flaps } = v;
  const row = v.rows[i];
  const flight = row.kind === "flight" ? row.flight : null;
  const delayed = flight?.remark === "DELAYED";
  const columns = columnsOf(shape.dest);
  const remarks = columns.at(-1).start;
  const starts = new Set(columns.slice(1).map((c) => c.start));
  const cells = [];
  for (let c = 0; c < shape.cells; c += 1) {
    const glyph = flaps.at(i, c);
    // DELAYED turns its letters over: black on yellow, the one thing on the board that isn't yellow on black.
    // A cell turns over once it has landed, so the old remark doesn't show black on yellow while the wave comes.
    const loud = delayed && c >= remarks && glyph.top !== " " && flaps.settled(i, c);
    const el = cell(shape, glyph, loud ? YELLOW : CELL).when(loud, (e) => e.text_color(INK));
    cells.push(starts.has(c) ? el.ml(shape.gap) : el);
  }
  const half = shape.half;
  const left = (c, n) => c.start * shape.pitch + n * shape.gap;
  return h_flex().id(`row-${i}`).relative().w(left(columns.at(-1), columns.length - 1) + columns.at(-1).width * shape.pitch - shape.cellGap).h(shape.cellH).flex_none().gap(shape.cellGap).text_color(inkOf(row))
    .when(Boolean(row.flight), (el) => el.cursor_pointer().hover((s) => s.bg(PRESS))
      .on_mouse_down("left", stop).on_click(() => v.go(row.flight)))
    .children(cells)
    .children(columns.map((c, n) => div().absolute().left(left(c, n)).top(0).w(c.width * shape.pitch - shape.cellGap).h(half).bg(SHEEN)))
    .child(div().absolute().left(0).right(0).top(half).h(1).bg(INK))
    .when(delayed, (el) => el.child(div().absolute().left(-10).top(0).w(4).h(shape.cellH).bg(YELLOW)
      .loop("opacity", { from: 0.25, to: 1, period: 1200 })));
}

function headings(shape) {
  const columns = columnsOf(shape.dest);
  return h_flex().h(shape.label + 8).flex_none().px(SIDE).items_center().text_size(shape.label).text_color(AMBER).children(columns.map((c, i) =>
    div().w(c.width * shape.pitch).flex_none().when(i > 0, (el) => el.ml(shape.gap)).child(c.name)));
}

function clock(v) {
  const size = { cell: Math.round(v.shape.cell * 0.72), cellH: Math.round(v.shape.cellH * 0.72) };
  const half = Math.floor(size.cellH / 2);
  return h_flex().relative().h(size.cellH).flex_none().gap(2).text_size(Math.round(size.cell * 1.08)).text_color(YELLOW)
    .children(Array.from({ length: v.clock.cells }, (_, c) => cell(size, v.clock.at(0, c), CELL)))
    .child(div().absolute().left(0).right(0).top(half).h(1).bg(INK));
}

// Folds the board out of the terminals' way, and brings it back.
function foldButton(v) {
  return div().id("fold").h(26).flex_none().px(10).flex().items_center().rounded(2).border(1).border_color(AMBER)
    .text_size(12).text_color(YELLOW).cursor_pointer().hover((s) => s.bg(PRESS))
    .on_mouse_down("left", stop).on_click(() => v.fold()).child(v.folded ? "BOARD" : "TERMINALS");
}

function header(v) {
  const { shape } = v;
  return h_flex().h(shape.head).flex_none().pl(v.inset).pr(SIDE).gap(14).items_center()
    .child(svg("mods/departures/plane.svg").size(Math.round(shape.title * 0.95)).flex_none().text_color(YELLOW))
    .child(div().flex_1().min_w(0).text_size(shape.title).text_color(YELLOW).truncate().child("DEPARTURES"))
    .when(v.pages > 1, (el) => el.child(div().flex_none().text_size(12).text_color(AMBER).child(`PAGE ${v.page + 1}/${v.pages}`)))
    .child(clock(v))
    .child(foldButton(v));
}

export function board(v) {
  const { shape } = v;
  return v_flex().id("root").size_full().bg(INK).overflow_hidden().font_family(FONT).font_weight(700)
    .on_size_change(v.onSize).on_hover(v.onHover)
    .child(header(v))
    .child(headings(shape))
    .child(v_flex().flex_none().px(SIDE).gap(shape.rowGap).text_size(shape.font).children(v.rows.map((_, i) => boardRow(v, i))));
}
