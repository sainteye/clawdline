/**
 * Which page, and the drawer that names them.
 *
 * Before this, the web app was one page with things laid over it: Usage hid `#app` by hand from
 * inside its own module, and Settings was a modal sheet. Nothing decided *where you are*, so
 * nothing could be linked to, and every new destination had to invent its own appearing and its
 * own way of putting the page back. `core/pages.js` is that decision, and this is what holds it.
 *
 * Two halves, and the second is the one that would otherwise rot. The first drives the real
 * module against a stand-in document: one page on screen at a time, `enter` and `leave` in the
 * right order and only on a real move, focus, the address, and the delegated `data-page-to` that
 * makes the next page's link a line of markup. The second holds the document, `main.js` and the
 * registry against each other — a row in the drawer naming a page nobody registered is exactly
 * the mistake this shape makes easy, it breaks nothing at load time, and it is invisible in a
 * diff that touches two files.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const read = (relative) => readFileSync(join(root, relative), "utf8");

let checks = 0;
let failed = false;
function check(condition, message) {
    checks += 1;
    if (condition) return;
    failed = true;
    console.error(`FAIL: ${message}`);
}
/** A value said back in a message. An element is its id: the node itself is a cycle. */
function say(value) {
    if (value && typeof value === "object" && typeof value.id === "string") return `#${value.id}`;
    return JSON.stringify(value);
}
function equal(actual, expected, message) {
    check(actual === expected, `${message} — expected ${say(expected)}, got ${say(actual)}`);
}

/* ==========================================================================
   A document, in as much as this module reads one
   ========================================================================== */

class FakeNode {
    constructor(doc, id) {
        this.ownerDocument = doc;
        this.id = id;
        this.hidden = false;
        this.attributes = {};
        this.parentNode = null;
        this.children = [];
        this.focusCalls = [];
    }
    getAttribute(name) {
        return Object.prototype.hasOwnProperty.call(this.attributes, name) ? this.attributes[name] : null;
    }
    setAttribute(name, value) { this.attributes[name] = String(value); }
    removeAttribute(name) { delete this.attributes[name]; }
    focus(options) { this.focusCalls.push(options || null); this.ownerDocument.activeElement = this; }
    append(child) { child.parentNode = this; this.children.push(child); return child; }
}

class FakeDocument {
    constructor() {
        this.byId = {};
        this.listeners = {};
        this.activeElement = null;
        this.documentElement = new FakeNode(this, "html");
        this.documentElement.parentNode = this;
    }
    make(id, parent) {
        const node = new FakeNode(this, id);
        node.parentNode = parent || this.documentElement;
        this.byId[id] = node;
        return node;
    }
    getElementById(id) { return this.byId[id] || null; }
    addEventListener(name, handler) { (this.listeners[name] ||= []).push(handler); }
    click(target) {
        const event = { target, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; } };
        for (const handler of this.listeners.click || []) handler(event);
        return event;
    }
}

/* ==========================================================================
   The mechanism
   ========================================================================== */

const { Pages, pageInHash, hashForPage } = await import("../Resources/web/app/js/core/pages.js");

equal(pageInHash("#page=usage"), "usage", "a fragment naming a page is read");
equal(pageInHash("#session=abc&page=settings"), "settings", "even beside a session");
equal(pageInHash("#session=abc"), null, "a fragment naming only a session names no page");
equal(pageInHash(""), null, "and neither does no fragment at all");
equal(pageInHash("#page="), null, "an empty name is not a page name");
equal(pageInHash("#page=a%20b"), "a b", "the name is decoded");
equal(hashForPage("usage"), "#page=usage", "and written back the same way");
equal(pageInHash(hashForPage("a b")), "a b", "which round-trips through the encoding");

// Before `bind`, every entry point is inert rather than throwing: several Node suites import
// modules that reach this one, and a module that needs a document to be imported cannot be.
equal(Pages.current(), "", "an unbound router is on no page");
equal(Pages.go("usage"), false, "and going somewhere does nothing");

const doc = new FakeDocument();
const sessions = doc.make("app");
const usage = doc.make("usage-analytics");
const settings = doc.make("settings");
const usageClose = doc.make("usage-close", usage);
const settingsClose = doc.make("settings-close", settings);
const row = doc.make("usage-open");
const rowLabel = doc.make("usage-open-label", row);
row.setAttribute("data-page-to", "usage");
const nowhere = doc.make("nav-nowhere");
nowhere.setAttribute("data-page-to", "atlantis");

// Two pages start visible, which is what a document that has never been told looks like.
usage.hidden = false;
settings.hidden = false;

const events = [];
const written = [];
const announced = [];
Pages.bind({
    document: doc,
    root: doc.documentElement,
    pages: [
        { name: "sessions", element: sessions },
        {
            name: "usage", element: usage, focus: "usage-close",
            enter: () => events.push("usage.enter"), leave: () => events.push("usage.leave"),
        },
        {
            name: "settings", element: settings, focus: "settings-close",
            enter: () => events.push("settings.enter"),
        },
    ],
    onChange: (to, from) => announced.push(`${from || "-"}>${to}`),
    writeHash: (hash) => written.push(hash),
});

equal(Pages.current(), "sessions", "binding lands on the first page in the list");
equal(Pages.home(), "sessions", "which is also the one every Close goes back to");
equal(sessions.hidden, false, "the home page is showing");
equal(usage.hidden, true, "and a page the markup left visible is put away");
equal(settings.hidden, true, "all of them");
equal(doc.documentElement.getAttribute("data-page"), "sessions",
      "which page it is, written where anything can read it");
equal(written.length, 0, "the first paint does not write to the address");
equal(announced.join(","), "->sessions", "but it does say where it landed");

check(Pages.knows("usage") && Pages.knows("settings") && Pages.knows("sessions"),
      "the registry knows the pages it was given");
check(!Pages.knows("projects"),
      "and does not know one nobody registered — the Projects page is the next slice, not this one");

equal(Pages.go("usage"), true, "going to a page it knows succeeds");
equal(Pages.current(), "usage", "and that is where it is");
equal(usage.hidden, false, "the page is on screen");
equal(sessions.hidden, true, "and the one before it is not");
equal(events.join(","), "usage.enter", "arriving runs the page's own arrival");
equal(doc.activeElement, usageClose, "and the keyboard lands where the page asked");
equal(usageClose.focusCalls[0] && usageClose.focusCalls[0].preventScroll, true,
      "without scrolling the page it has just shown");
equal(written.join(","), "#page=usage", "a deliberate move is written to the address");
equal(announced.join(","), "->sessions,sessions>usage", "and announced with where it came from");

equal(Pages.go("usage"), false, "asking for the page already on screen is not a move");
equal(events.join(","), "usage.enter",
      "so it does not run the arrival again — a second tap must not re-fetch the portfolio");
equal(written.length, 1, "nor write the same address twice");

equal(Pages.go("settings"), true, "moving between two pages that are not home");
equal(events.join(","), "usage.enter,usage.leave,settings.enter",
      "the page being left is told before the page being entered");
equal(usage.hidden, true, "the old page goes");
equal(settings.hidden, false, "the new one arrives");
equal(doc.activeElement, settingsClose, "with the keyboard on its own way out");

equal(Pages.go("atlantis"), false, "a page that does not exist is refused");
equal(Pages.current(), "settings", "and leaves you where you were");
equal(settings.hidden, false, "with the page you were on still on screen");

written.length = 0;
equal(Pages.go("sessions", { hash: false }), true, "a move the address itself asked for still moves");
equal(Pages.current(), "sessions", "to the page it named");
equal(written.length, 0, "and does not write the address back to itself");

// The delegated click: this is what makes the next page's link a line of markup rather than a
// listener. It has to work from a node *inside* the control, because that is what a tap on a
// label inside a button actually hits.
doc.click(rowLabel);
equal(Pages.current(), "usage", "a click inside a control naming a page goes there");
equal(written.join(","), "#page=usage", "by the ordinary route, address and all");
doc.click(nowhere);
equal(Pages.current(), "usage", "a control naming a page that does not exist changes nothing");

Pages.go("sessions");
const stray = doc.make("stray");
doc.click(stray);
equal(Pages.current(), "sessions", "and a click on anything else is not navigation");

/* ==========================================================================
   The document, `main.js`, and the registry — held against each other
   ========================================================================== */

/* The comments come out first, and they are not an inconvenience — the document explains where
   the Projects page plugs in by writing the markup it would need, and a scan that counted that
   would report a page nobody can reach. It is the same trap `tools/check-web-ids.py` names: a
   comment describing markup vouches for an element that is not there. */
const raw = read("Resources/web/index.html");
const page = raw.replace(/<!--[\s\S]*?-->/g, "");
check(page.length < raw.length, "the comment strip actually removed something");
check(!page.includes("<!--"), "and left no comment behind for the scans below to read as markup");
const mainSource = read("Resources/web/app/js/main.js");
const usageSource = read("Resources/web/app/js/view/usage.js");
const settingsSource = read("Resources/web/app/js/input/settings.js");
const routeSource = read("Resources/web/app/js/input/route.js");

const registryBlock = /Pages\.bind\(\{[\s\S]*?pages:\s*\[([\s\S]*?)\n {4}\],/.exec(mainSource);
check(registryBlock, "main.js declares the page registry Pages.bind reads");
const registered = [...(registryBlock ? registryBlock[1] : "").matchAll(/name:\s*"([^"]+)"/g)]
    .map((match) => match[1]);
check(registered.length >= 3,
      `the registry names at least the three pages of this slice: ${JSON.stringify(registered)}`);
equal(registered[0], "sessions", "home is first, because that is how Pages decides which one it is");
for (const name of ["usage", "settings"]) {
    check(registered.includes(name), `${name} is a registered page`);
}

const views = [...page.matchAll(/data-page-view="([^"]+)"/g)].map((match) => match[1]);
for (const name of registered) {
    check(views.includes(name), `the document carries a section for the registered page ${name}`);
}
for (const name of views) {
    check(registered.includes(name),
          `the section data-page-view="${name}" is registered in main.js — a page the router does not know is a page nothing can reach`);
}

const destinations = [...page.matchAll(/data-page-to="([^"]+)"/g)].map((match) => match[1]);
check(destinations.length >= 4,
      `the document has controls naming pages: ${JSON.stringify(destinations)}`);
for (const name of destinations) {
    check(registered.includes(name),
          `the control naming data-page-to="${name}" names a registered page — this is the mistake the shape makes easy, and it breaks nothing at load time`);
}

/* ---- the drawer ---------------------------------------------------------- */

const sidebar = /<nav class="sidebar" id="sidebar"[\s\S]*?<\/nav>/.exec(page);
check(sidebar, "the document has the drawer");
const drawer = sidebar ? sidebar[0] : "";
check(/hidden/.test(drawer), "which comes up closed");
check(/id="usage-open"[^>]*data-page-to="usage"/.test(drawer),
      "Usage is reached from the drawer now, not from a button at the bottom of the settings sheet");
check(/id="nav-settings"[^>]*data-page-to="settings"/.test(drawer), "and so is Settings");
check(/id="nav-sessions"[^>]*data-page-to="sessions"/.test(drawer), "and the way back to the list");
check(/id="brand"[\s\S]{0,400}?aria-controls="sidebar"/.test(page),
      "the wordmark says in the markup what it opens");
check(/id="brand"[\s\S]{0,400}?aria-expanded="false"/.test(page),
      "and that it is closed to begin with");

/* Found in a browser and nowhere else, which is why it is written down here.
   The sheets in this app close on a tap outside by putting `stopPropagation` on the sheet, and
   the drawer was built the same way — but every page link in it is answered by one delegated
   listener on the document, so the panel swallowing its own clicks swallowed the navigation with
   them. Pressing Settings lit the row and did nothing, with nothing in the console. No fake
   document catches it, because a fake document has no scrim over it to swallow anything. */
const sidebarSource = read("Resources/web/app/js/input/sidebar.js");
const swallows = /\.stopPropagation\s*\(/;
// Calibrated against a known positive first. A zero from a pattern nobody has seen match is a
// statement about the pattern, and this one has to survive the comment above it in `sidebar.js`
// naming the very thing it forbids — a pattern cannot tell doing from mentioning unless it is
// written to.
check(swallows.test(read("Resources/web/app/js/input/command.js")),
      "the pattern finds a swallowed click where there really is one");
check(!swallows.test(sidebarSource),
      "the drawer must not swallow clicks inside itself — the delegated page links travel through it");
check(/ev\.target === els\.sidebar/.test(sidebarSource),
      "it tells a tap on the scrim from a tap on a row by asking what was hit");

/* ---- Settings is a page, not a sheet ------------------------------------- */

const settingsMarkup = /<section class="page page-settings" id="settings"[\s\S]*?\n<\/section>/.exec(page);
check(settingsMarkup, "Settings is a page section");
check(settingsMarkup && !/role="dialog"/.test(settingsMarkup[0]),
      "and not a dialog — there is nothing behind it to be modal over");
check(!/<div class="overlay" id="settings"/.test(page),
      "the settings overlay is gone rather than left behind beside the page that replaced it");
check(/id="settings-close"[^>]*data-page-to="sessions"/.test(page),
      "its Close leads back to the list through the same attribute every other one uses");
check(/open:\s*function\s*\(\)\s*\{\s*Pages\.go\("settings"\)/.test(settingsSource),
      "and opening Settings from anywhere means going to that page");
check(/enter:\s*function/.test(settingsSource),
      "with what it draws on arrival kept as its own arrival, so the address bar draws it too");

/* ---- Usage stopped deciding which page is on screen ---------------------- */

check(/id="usage-analytics"[\s\S]{0,200}?data-page-view="usage"/.test(page),
      "the Usage panel is a page");
check(/id="usage-close"[^>]*data-page-to="sessions"/.test(page),
      "and its Back to sessions is a page link");
for (const reach of ["elements.app", "elements.settings", "elements.brand"]) {
    check(!usageSource.includes(reach),
          `view/usage.js no longer reaches for ${reach} — hiding the rest of the app was the part that moved out`);
}
check(/navigate:\s*function/.test(mainSource),
      "main.js hands Usage the router, which is how Escape over that page still reaches the list");
check(/enter:\s*enter,\s*leave:\s*leave/.test(usageSource),
      "and Usage hands back its arrival and departure for Pages to call");

/* ---- the address --------------------------------------------------------- */

check(routeSource.includes("pageInHash"), "the fragment router reads the page out of the fragment");
check(/Pages\.go\(page,\s*\{\s*hash:\s*false\s*\}\)/.test(routeSource),
      "and applies it without writing the address it was just read from");
check(page.indexOf("/app/css/pages.css") < page.indexOf("/app/css/usage.css"),
      "pages.css is linked before usage.css, so the older and more particular sheet wins where they overlap");

console.log(`${failed ? "not ok" : "ok"}: web pages and sidebar, ${checks} checks`);
if (failed) process.exit(1);
