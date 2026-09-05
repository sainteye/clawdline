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
// The wordmark. It is not on any page — it is in the header above all of them — which is what
// makes it the one control focus can be handed back to whichever page has just been left.
const brand = doc.make("brand");

// Two pages start visible, which is what a document that has never been told looks like.
usage.hidden = false;
settings.hidden = false;

const events = [];
const written = [];
const announced = [];
Pages.bind({
    document: doc,
    root: doc.documentElement,
    focusFallback: "brand",
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
// And the first paint does not take the keyboard either. The fallback below only answers a move
// away from somewhere; a page arriving from nowhere is a page nobody has navigated to yet.
equal(doc.activeElement, null, "the first paint leaves the keyboard where the document put it");

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

/* ---- the way out of a page ------------------------------------------------
   Arriving was held here from the first day and leaving was not, which is where the keyboard was
   being dropped: `go` moves focus to the page's own `focus` id, the session list names none, and
   the control focus was on has just been hidden — a browser answers that by putting focus on
   `<body>`, from where nothing can be given back. The Usage panel used to hand it to the wordmark
   itself; nothing did once the panel became a page. Measured in a browser: `active=BODY` after
   "Back to sessions", after Escape on Usage, and after Close on Settings. */
Pages.go("usage");
equal(doc.activeElement, usageClose, "arriving at a page still lands on the control it names");
equal(Pages.goHome(), true, "goHome is a move like any other — and nothing here had ever called it");
equal(Pages.current(), "sessions", "to the first page in the registry");
equal(doc.activeElement, brand,
      "and a page naming no control of its own hands the keyboard to the fallback rather than dropping it");

written.length = 0;
Pages.go("usage");
equal(Pages.goHome({ hash: false }), true, "goHome takes the options every other move takes");
equal(written.join(","), "#page=usage",
      "so the address can ask for home without being written back to itself — which is what a fragment naming no page needs");

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

/* ---- the drawer says its words in the reader's language --------------------
   `tools/check-web-strings.py` crosses three places — `T.<name>` in the modules, the fallback in
   `core/i18n.js`, the `/v1/strings` payload — and a label written straight into the markup is in
   none of them. That is not a gap it can close: it reads JavaScript, and an English word in
   `index.html` reads to it exactly like the English fallback that is supposed to be there. So the
   markup keeps the English, as every other label in this document does, and this holds each one
   against the paint in `view/static.js` that writes the translation over the top of it. Without it
   a drawer in Chinese reads 清單 / Usage / 設定 and nothing anywhere goes red. */
const staticSource = read("Resources/web/app/js/view/static.js");
const i18nSource = read("Resources/web/app/js/core/i18n.js");
const painted = [
    ["brand", "webMenu", "the wordmark's accessible name"],
    ["sidebar", "webPages", "the drawer's own landmark name"],
    ["nav-sessions", "webSessions", "the row that leads back to the list"],
    ["usage-open", "webUsage", "the Usage row"],
    ["nav-settings", "webSettings", "the Settings row"],
];
for (const [id, name, what] of painted) {
    check(new RegExp(`els(?:\\.${id}|\\["${id}"\\])[^\\n]*T\\.${name}\\b`).test(staticSource),
          `${what} is painted from T.${name} rather than left as the English in the markup`);
    check(new RegExp(`^ {4}${name}:`, "m").test(i18nSource),
          `and T.${name} has its English fallback in core/i18n.js`);
}
check(/webSessions:/.test(i18nSource) && /webBack:/.test(i18nSource),
      "the drawer's Sessions and the transcript's back button are two strings — one word in English, and the back button's is 清單 in Chinese, which is not what a navigation row says");

/* ---- Settings is a page, not a sheet ------------------------------------- */

const settingsMarkup = /<section class="page page-settings" id="settings"[\s\S]*?\n<\/section>/.exec(page);
check(settingsMarkup, "Settings is a page section");
check(settingsMarkup && !/role="dialog"/.test(settingsMarkup[0]),
      "and not a dialog — there is nothing behind it to be modal over");
/* The name went with the role, and a `<section>` with no accessible name is not a landmark at all.
   Usage is the control group in the same document: it kept `aria-labelledby`. The `aria-label`
   that was left behind is worse than nothing — it is on `#settings-sheet`, which is a plain
   `<div>` since the sheet stopped being one, and `aria-label` on a generic element names nothing.
   A line that looks like it is doing the work is what keeps anybody from doing it. */
check(settingsMarkup && /aria-labelledby="settings-title"/.test(settingsMarkup[0]),
      "Settings names itself with the heading it already draws");
check(/id="usage-analytics"[\s\S]{0,200}?aria-labelledby="usage-title"/.test(page),
      "the way Usage does — the control group for that, in this same document");
check(!/els\["settings-sheet"\][^\n]*aria-label/.test(staticSource),
      "and nothing writes aria-label onto the generic <div> inside it, where it never became a name");
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
check(!/doc\.addEventListener\(\s*"keydown"/.test(usageSource),
      "view/usage.js binds no keydown on the document — a second listener on one document is a second answer to one press");
check(/enter:\s*enter,\s*leave:\s*leave/.test(usageSource),
      "and Usage hands back its arrival and departure for Pages to call");

/* ---- the address --------------------------------------------------------- */

check(routeSource.includes("pageInHash"), "the fragment router reads the page out of the fragment");
check(/Pages\.go\(page,\s*\{\s*hash:\s*false\s*\}\)/.test(routeSource),
      "and applies it without writing the address it was just read from");
check(page.indexOf("/app/css/pages.css") < page.indexOf("/app/css/usage.css"),
      "pages.css is linked before usage.css, so the older and more particular sheet wins where they overlap");

/* The other half of the address: a fragment that stops naming a page. `routeTo` had no `else`, so
   clearing the fragment left `data-page="usage"` on the root with `#app` hidden and nothing in the
   address saying so — a reload from there landed on the session list instead. On a phone that is
   what the back gesture does: `session/open.js` pushes one entry, the pop closes the session
   underneath, and the hashchange that followed changed nothing. */
const routeBody = /export function routeTo\(hash\) \{[\s\S]*?\n\}/.exec(routeSource);
check(routeBody, "route.js has the fragment router this guards");
check(/Pages\.goHome\(\{\s*hash:\s*false\s*\}\)/.test(routeBody ? routeBody[0] : ""),
      "a fragment naming no page puts the home page back, without writing the address it was just read from");
check(/Pages\.knows\(page\)/.test(routeBody ? routeBody[0] : ""),
      "and a name this build has no page for still leaves you where you were rather than on a blank rectangle");

/* ==========================================================================
   Three fixes a browser found, and the guards they went without

   All three of these were fixed in a browser and green in Node at the same moment, which is the
   whole reason this section exists: each of them was re-broken on a copy of this tree and all 25
   web suites plus both Python guards stayed green. A fix nothing can go red for is a fix that
   comes back. Every pattern below is calibrated against a known positive first — a zero from a
   pattern nobody has seen match is a statement about the pattern, not about the file.
   ========================================================================== */

/* ---- one press, one thing -------------------------------------------------
   The drawer open over Usage, one Escape, and both the drawer and the page went. `input/keys.js`
   orders the whole of Escape and `return`s when a layer takes it — but a `return` ends its own
   listener and nothing else, and `view/usage.js` had a second `keydown` on the same document. The
   same three steps over Settings were right, which is what said this was an edge that had not been
   connected rather than a design. Nothing in `Tests/` had ever read `input/keys.js`. */
const keysSource = read("Resources/web/app/js/input/keys.js");
// The whole chain and not the confirmation's own Escape four lines above it: anchored on the
// listener's own indent, because a pattern that finds the wrong block reads as a passing one.
const escapeBlock = /\n {4}if \(key === "Escape"\) \{[\s\S]*?\n {4}\}/.exec(keysSource);
check(escapeBlock, "input/keys.js has the one Escape chain");
const escape = escapeBlock ? escapeBlock[0] : "";
const layers = [...escape.matchAll(/!els(?:\.|\[")([a-z-]+)/g)].map((match) => match[1]);
check(layers.length >= 5, `the pattern reads the chain in order: ${JSON.stringify(layers)}`);
for (const over of ["info", "start", "command", "schedule-form", "sidebar", "keys"]) {
    check(layers.includes(over), `Escape is offered to ${over} while it is open`);
}
const leaves = escape.indexOf("Pages.goHome()");
check(leaves > 0, "leaving a page is one line in that chain rather than a listener somewhere else");
for (const over of ["sidebar", "keys"]) {
    const asked = escape.indexOf(`els.${over}.hidden`);
    check(asked > 0 && asked < leaves,
          `${over} is over whatever page you are on, so it takes the press first — closing both for one press would take somebody off the page when all they wanted was ${over} shut`);
}
check(/dialog\[open\]/.test(escape),
      "and a dialog opened over a page answers its own Escape, so leaving as well would still be two things for one press");

/* ---- the first list must not take a deep link away ------------------------
   `#page=usage` in a fresh tab came up on Usage and then, a second later, went back to the list and
   rewrote the address on the way out: the first list to arrive opens the top session on a desk, and
   opening a session means being on the sessions page. Two ports and a control tree to find it.
   Removing this one condition left all 25 suites and both Python guards green. */
const listSource = read("Resources/web/app/js/view/list.js");
const courtesy = /if \(firstList && S\.sessions\.length\) \{[\s\S]*?\n {4}\}/.exec(listSource);
check(courtesy, "view/list.js has the first-list courtesy this guards");
check(/Pages\.current\(\) === Pages\.home\(\)/.test(courtesy ? courtesy[0] : ""),
      "which is offered only while the app is on the home page — a deep link asked a different question");

/* ---- neither half of the drawer moves -------------------------------------
   An animation sits at its first keyframe for as long as it is `running`, and a renderer that
   throttles animations never runs it. The panel lost its `translateX` for that reason; the scrim
   kept a `fade`, and `opacity` applies to the subtree — so the same throttle left the whole drawer
   invisible, over the page, and still taking clicks. Worse than what was removed, not better. */
const pagesCss = read("Resources/web/app/css/pages.css");
const sheetsCss = read("Resources/web/app/css/sheets.css");
const uncommented = (css) => css.replace(/\/\*[\s\S]*?\*\//g, "");
check(uncommented(pagesCss).length < pagesCss.length,
      "the CSS comment strip removed something — the reasons in this sheet name the properties below by hand");
const rule = (css, pattern) => {
    const found = pattern.exec(uncommented(css));
    return found ? found[1] : null;
};
const moves = /(?:^|[\s;])(?:transform|animation)\s*:/;
const overlay = rule(sheetsCss, /(?:^|\})\s*\.overlay\s*\{([^}]*)\}/);
check(overlay !== null && moves.test(overlay),
      "the pattern finds an animated fixed layer where there is one — `.overlay` in sheets.css is this app's existing one");
const scrim = rule(pagesCss, /(?:^|\})\s*\.sidebar\s*\{([^}]*)\}/);
const panel = rule(pagesCss, /(?:^|\})\s*\.sidebar-panel\s*\{([^}]*)\}/);
check(scrim !== null && panel !== null, "pages.css has both halves of the drawer");
check(!moves.test(scrim || ""),
      "the scrim is not animated: `opacity` applies to the subtree, so a fade that never runs hides the panel with it and leaves a fixed layer eating clicks");
check(!moves.test(panel || ""),
      "and the panel does not slide in — the same failure, found in an iframe Chrome had decided not to animate");

/* ---- the id in the fragment is written encoded, and read both ways ---------
   A notification about a session carries `/#session=<id>`, and on this Mac an id is usually a
   tmux pane — `%141`. Written raw, the fragment reads `#session=%141`, and the reader below
   answers it with `decodeURIComponent`, which does not throw on that: `%14` is a complete escape,
   so `%141` decodes to U+0014 followed by `1` — a session id that has never existed. `byId` found
   nothing, the first list cleared the request, and the tap stopped on the session list. Nothing
   was red: every fixture in this file used ids like `abc`, which survive the round trip unchanged.

   So the address is written percent-encoded (`WebPush.sessionURL(forSessionID:)`), and read as
   two candidates rather than one — the decoding, and the text exactly as written — because the
   notifications already delivered to a phone carry the old spelling and are tapped days later.

   The module is imported with its three imports and its three browser globals replaced, because
   `session/open.js` reaches the whole app and importing it here never returns. */
const routeStandalone =
    "const window = globalThis.__routeEnv.window;\n" +
    "const location = globalThis.__routeEnv.location;\n" +
    "const navigator = globalThis.__routeEnv.navigator;\n" +
    routeSource
        .replace('import { Pages, pageInHash } from "../core/pages.js";',
            "const Pages = globalThis.__routeEnv.Pages;\n" +
            "const pageInHash = globalThis.__routeEnv.pageInHash;")
        .replace('import { byId } from "../view/derive.js";',
            "const byId = globalThis.__routeEnv.byId;")
        .replace('import { openSession } from "../session/open.js";',
            "const openSession = globalThis.__routeEnv.openSession;");
check(!/^import /m.test(routeStandalone),
      "every import in route.js was replaced — one left behind would pull the whole app in and hang");

const listed = new Set();
const opened = [];
globalThis.__routeEnv = {
    Pages: { knows: () => true, go: () => {}, goHome: () => {}, current: () => "sessions",
             home: () => "sessions" },
    pageInHash: () => null,
    byId: (id) => (listed.has(id) ? { id: id } : null),
    openSession: (id) => { opened.push(id); },
    window: { addEventListener: () => {} },
    location: { hash: "" },
    navigator: {},
};
const route = await import(
    "data:text/javascript;base64," + Buffer.from(routeStandalone).toString("base64"));

// The pane really is what this Mac watches: `Sources/Tmux.swift` calls `%12` "stable for the life
// of the pane", and `%141` is what `tmux list-panes` prints here.
const pane = "%141";
equal(decodeURIComponent(encodeURIComponent(pane)), pane,
      "the encoding this rests on round-trips the id a tmux pane really has");

listed.add(pane);
opened.length = 0;
route.routeTo("#session=%25141");
equal(opened.join(","), pane,
      "a notification written today opens the pane it names — its per-cent arrives as %25");

// The half that cannot be re-issued: a notification already sitting on a phone was written before
// this, and tapping it a day later has to land in the same place.
opened.length = 0;
route.routeTo("#session=%141");
equal(opened.join(","), pane,
      "and a notification delivered before the encoding still opens it, read as written");

// Nothing about an id with no per-cent in it changes: iTerm's ids are the control group.
const iterm = "w0t0p0:1234-ABCD";
listed.add(iterm);
opened.length = 0;
route.routeTo("#session=" + iterm);
equal(opened.join(","), iterm, "an id that needs no encoding is untouched by either road");

// A cold start routes before it knows what sessions exist, so both candidates have to survive the
// wait — `openWanted` is called again with every list.
listed.clear();
opened.length = 0;
route.routeTo("#session=%141");
equal(opened.length, 0, "a session the list has not brought yet is not opened");
check(!!route.wantedSession,
      "but it is held — `view/list.js` reads this to know somebody asked for a session");
equal(route.openWanted(), false, "and asking again while it is still missing is still no");
listed.add(pane);
equal(route.openWanted(), true, "the list arrives, and the request is answered");
equal(opened.join(","), pane, "with the id as written, not as decoded");

// A second request replaces the first, both halves of it. `routeTo` runs again on every
// hashchange, and a raw candidate left over from the request before it would answer a question
// nobody is asking any more — with a different session, which is worse than answering nothing.
listed.clear();
route.routeTo("#session=%141");            // held: nothing is in the list yet
listed.add(pane);                          // and now the pane it named is
opened.length = 0;
route.routeTo("#session=ghost");           // but somebody has asked for a session that is not
equal(opened.length, 0,
      "a request naming a session the list does not have opens nothing — not the one asked for before it");

// And letting go lets go. `view/list.js` clears the request when the first whole list does not
// contain the session it names — a tab somebody has since closed — so that a session which is
// never coming does not hold the default open hostage.
listed.clear();
route.routeTo("#session=%25141");
route.setWantedSession(null);
listed.add(pane);
opened.length = 0;
equal(route.openWanted(), false, "a request that was let go stays let go");
equal(opened.length, 0, "and nothing is opened behind it");

// The raw text is a rescue for links written before the encoding, not a second guess at the ones
// written after it. `%252` is how `%2` is spelled now, and pane ids count up from 1 — so a machine
// that has reached pane `%252` holds both readings of that link as real, live sessions. The day
// `%2` closes, the raw reading names a stranger, and the screen would say nothing about it.
listed.clear();
route.setWantedSession(null);
listed.add("%252");
opened.length = 0;
route.routeTo("#session=%252");
equal(opened.length, 0,
      "a link whose pane has closed opens nothing — not the pane whose id is how that one is now spelled");

// And the request it did make is the right one, still waiting for a list that has it.
listed.add("%2");
equal(route.openWanted(), true, "the pane it actually named arrives, and the request is answered");
equal(opened.join(","), "%2", "with the id the link meant");

// The old spelling keeps its second candidate, because decoding it produces something no session
// could be called — U+0014 and a `1` — and that impossibility is the evidence the text is raw.
listed.clear();
route.setWantedSession(null);
listed.add(pane);
opened.length = 0;
route.routeTo("#session=%141");
equal(opened.join(","), pane, "an old notification is still read as written, on the strength of that");

console.log(`${failed ? "not ok" : "ok"}: web pages and sidebar, ${checks} checks`);
if (failed) process.exit(1);
