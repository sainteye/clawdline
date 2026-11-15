// Departures: every coding agent as a flight on a split-flap board. The rules are in logic.js, the made-up flights
// in demo.js and the drawing in view.js. This holds the state and the timers.
//
// Nothing here runs on its own between changes. The flaps tick only during a flip, the page timer returns at once with
// one page, and the heartbeat redraws nothing unless the clock or a row has moved.

import { View } from "gpui-kit";
import { agents, nextCommand, playSound, props, run, setAgentOrder, settings, terminals, window } from "herdr";
import {
  CLACK_GAP_MS, DONE_CLEAR_MS, FLIP_TICK_MS, Flaps, HEARTBEAT_MS, PAGE_MS, SIDE,
  assignCodes, boardOrder, clockText, fitBoard, noteChanges, pageRows, pagesOf, readFlights,
} from "./logic.js";
import { DEMO_STEP_MS, demoWorld } from "./demo.js";
import { board } from "./view.js";

const CLACK = "mods/departures/clack.mp3";
const BLOCK = "board"; // the board's node in the layout
const OPEN_PX = 400; // its height in the layout
const FIRST_SIZE = { width: 1280, height: OPEN_PX }; // until the first layout says

function load(key) {
  try {
    return JSON.parse(localStorage.getItem(key) ?? "{}");
  } catch {
    return {};
  }
}

export default class Departures extends View {
  init(_props, cx) {
    this.demo = props()?.demo === true;
    this.seen = this.demo ? {} : load("seen"); // address -> { remark, at }
    this.codes = this.demo ? {} : load("codes"); // address -> flight code
    this.openHeight = Number(localStorage.getItem("open")) || OPEN_PX;
    this.size = null;
    this.flaps = null;
    this.clock = new Flaps(1, 5);
    this.rows = [];
    this.page = 0;
    this.pages = 1;
    this.step = 0;
    this.hovered = false;
    this.ticking = false;
    this.clacked = false;
    this.clackAt = 0;
    this.order = "";
    this.onSize = (size, cx) => this.resized(size, cx);
    this.onHover = (hovered) => { this.hovered = hovered; };
    this.go = (flight) => this.goTo(flight);
    this.fold = () => this.toggleFold();
    cx.timer.every(HEARTBEAT_MS, (cx) => this.sync(cx));
    cx.timer.every(PAGE_MS, (cx) => this.turnPage(cx));
    if (this.demo) cx.timer.every(DEMO_STEP_MS, (cx) => { this.step += 1; this.sync(cx); });
    cx.spawn(async () => {
      for (;;) if (await nextCommand() === "fold") this.toggleFold();
    });
  }

  // on_size_change reports window pixels, and everything drawn is in the app's own, scaled by the zoom.
  resized(size, cx) {
    const zoom = settings()?.zoom || 1;
    const next = { width: size.width / zoom, height: size.height / zoom };
    if (next.width === this.size?.width && next.height === this.size?.height) return;
    if (!this.size) this.flaps = null; // the real size: the board fills in from blank at it
    this.size = next;
    cx.notify();
  }

  shape() {
    const { width, height } = this.size ?? FIRST_SIZE;
    return fitBoard(width, height, settings()?.zoom || 1);
  }

  world() {
    return this.demo ? demoWorld(this.step) : { list: agents(), all: terminals(), now: Date.now() };
  }

  // Works out what the board should read and sets the flaps going if that's new. Runs on every render, because that's
  // how the app says its data changed, and from the timers. Only the flaps redraw, and only while they move.
  sync(cx, quiet = false) {
    const { list, all, now } = this.world();
    const read = readFlights(list, all);
    const noted = noteChanges(this.seen, read, now);
    this.seen = noted.seen;
    const coded = assignCodes(read, this.codes);
    if (!this.demo && noted.changed) localStorage.setItem("seen", JSON.stringify(this.seen));
    if (!this.demo && coded) localStorage.setItem("codes", JSON.stringify(this.codes));
    const flights = boardOrder(read, this.seen, now, DONE_CLEAR_MS);

    const shape = this.shape();
    const pages = pagesOf(flights, shape.rows);
    this.pages = pages.length;
    if (this.page >= this.pages) this.page = 0;
    const { lines, rows } = pageRows(pages[this.page], shape, flights.length === 0);
    this.rows = rows;

    const fresh = !this.flaps;
    if (fresh) this.flaps = new Flaps(shape.rows, shape.cells);
    else if (!this.flaps.fits(shape.rows, shape.cells)) this.flaps = new Flaps(shape.rows, shape.cells, lines);
    const moved = this.flaps.aim(lines);
    const ticked = this.clock.aim([clockText(now)]);

    if (moved && !fresh && !quiet) this.clack();
    if (!this.demo) {
      const order = flights.map((f) => f.address).join(" ");
      if (order !== this.order) {
        this.order = order;
        setAgentOrder(flights.map((f) => f.address));
      }
    }
    if ((moved || ticked) && !this.ticking) {
      this.ticking = true;
      cx.timer.after(FLIP_TICK_MS, (cx) => this.flip(cx));
    }
  }

  // One tick of a burst. The next is only asked for while something is still moving.
  flip(cx) {
    const flaps = this.flaps?.tick() ?? false;
    const clock = this.clock.tick();
    if (flaps || clock) cx.timer.after(FLIP_TICK_MS, (cx) => this.flip(cx));
    else {
      this.ticking = false;
      this.clacked = false;
    }
    cx.notify();
  }

  // At most once a burst, and not back to back if bursts run together.
  clack() {
    const now = Date.now();
    if (this.clacked || now - this.clackAt < CLACK_GAP_MS) return;
    this.clacked = true;
    this.clackAt = now;
    if (settings()?.sounds?.enabled !== false) playSound(CLACK);
  }

  // A page turn flips the board without a clack: it's the board's habit, not news. It waits while the pointer is on the
  // board, so a row doesn't change under a press.
  turnPage(cx) {
    if (this.pages < 2 || (this.hovered && !this.demo)) return;
    this.page = (this.page + 1) % this.pages;
    this.sync(cx, true);
  }

  goTo(flight) {
    if (this.demo || !flight) return;
    if (flight.pane) run("pane.focus", { pane: flight.pane });
    else run("terminal.peek", { terminal: flight.address });
  }

  isFolded(shape) {
    return (this.size ?? FIRST_SIZE).height <= shape.foldHeight + 1;
  }

  // Down to the top two rows, or back to the height it had.
  toggleFold() {
    const shape = this.shape();
    if (this.isFolded(shape)) {
      run("layout.resize", { id: BLOCK, px: this.openHeight });
      return;
    }
    this.openHeight = Math.round((this.size ?? FIRST_SIZE).height);
    localStorage.setItem("open", String(this.openHeight));
    run("layout.resize", { id: BLOCK, px: shape.foldHeight });
  }

  render(cx) {
    this.sync(cx);
    const shape = this.shape();
    const w = window();
    return board({
      shape,
      flaps: this.flaps,
      clock: this.clock,
      rows: this.rows,
      page: this.page,
      pages: this.pages,
      folded: this.isFolded(shape),
      inset: w?.platform === "macos" && !w.fullscreen ? 84 : SIDE, // room for the window buttons
      onSize: this.onSize,
      onHover: this.onHover,
      go: this.go,
      fold: this.fold,
    });
  }
}
