/**
 * The chip at the foot of the page, when what is running is a test or a build on this Mac.
 *
 * Four things are worth holding here, and every one of them is a decision somebody could
 * plausibly undo without noticing:
 *
 * - **A local run beats a deploy.** There is one chip. A deploy in somebody's cloud can wait for
 *   the Links sheet; the run holding up the person at this keyboard cannot.
 * - **A phase is drawn where the percentage goes.** The bar already says how far along this is;
 *   "compiling" answers the question a percentage cannot.
 * - **No `url` leaves no `href`.** A local run's `log` is a filesystem path, not a web page, and
 *   the deploy chip's `guard let url` is exactly why a local run written in the `ghrun-` shape
 *   reaches the Mac footer and never the phone. The row must survive having nowhere to go.
 * - **An unrecognised state draws nothing.** Not a cross: a red mark that is always wrong is
 *   worse than an empty slot, and new states will be invented by producers nobody has met.
 *
 * `input/status-line.js` reaches the browser through nine imports, two of which start timers the
 * moment they are loaded. So it is read as source and its imports are replaced with stand-ins —
 * the same shape `Tests/web-session-closeability.mjs` uses — and `esc` alone stays real, because
 * escaping producer text is part of what is being checked.
 */
import { readFile } from "node:fs/promises";

const statusLineURL = new URL("../Resources/web/app/js/input/status-line.js", import.meta.url);
const escURL = new URL("../Resources/web/app/js/core/esc.js", import.meta.url);
const source = await readFile(statusLineURL, "utf8");

const stubs = [
    ['import { esc } from "../core/esc.js";', `import { esc } from "${escURL.href}";`],
    ['import { T } from "../core/i18n.js";', 'const T = { webLinkRunning: "running" };'],
    ['import { S } from "../core/state.js";', "const S = { openId: null };"],
    ['import { els } from "../core/dom.js";', "const els = {};"],
    ['import { assistantLogo, assistantName } from "../core/pixels.js";',
        "const assistantLogo = () => \"\"; const assistantName = () => \"\";"],
    ['import { api } from "../net/api.js";', "const api = {};"],
    ['import { byId } from "../view/derive.js";', "const byId = () => null;"],
    ['import { listUnknown } from "../view/waits.js";', "const listUnknown = () => false;"],
    ['import { Diagnostics } from "../core/layout-diagnostics.js";',
        "const Diagnostics = { note() {} };"],
    ['import { createTieredSessionFacts } from "../session/transcript-requests.js";',
        "const createTieredSessionFacts = () => ({ tier: () => null, peek: () => null });"],
];
let standalone = source;
for (const [from, to] of stubs) {
    if (!standalone.includes(from)) {
        console.error(`FAIL: status-line.js no longer imports through \`${from}\``);
        process.exit(1);
    }
    standalone = standalone.replace(from, to);
}
const { runningDeploy, deployProgress, drawDeploy } = await import(
    "data:text/javascript;base64," + Buffer.from(standalone).toString("base64"));

let checks = 0;
let failed = false;
function check(condition, message) {
    checks += 1;
    if (condition) return;
    failed = true;
    console.error(`FAIL: ${message}`);
}
function equal(actual, expected, message) {
    check(actual === expected,
        `${message} — expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

/** As much of an element as `drawDeploy` touches, and no more. */
function node() {
    return {
        hidden: false, dataset: {}, innerHTML: "", title: "", attributes: {},
        removeAttribute(name) {
            if (name === "href") delete this.href;
            delete this.attributes[name];
        },
        setAttribute(name, value) { this.attributes[name] = value; },
    };
}

const now = Math.floor(Date.now() / 1000);
const deployRow = {
    label: "deploy", kind: "deploy", state: "running", local: false,
    url: "https://github.com/example/atrium/actions/runs/32206093412",
    startedAt: now - 320, typicalSeconds: 800,
};
const runRow = {
    label: "test", kind: "run", state: "running", local: true,
    phase: "compiling", startedAt: now - 144, typicalSeconds: 288,
};

/* ==========================================================================
   The local run takes the chip
   ========================================================================== */

equal(runningDeploy({ links: [deployRow, runRow] }), runRow,
    "a local run is preferred to a deploy that is also running");
equal(runningDeploy({ links: [runRow, deployRow] }), runRow,
    "and the preference is not an accident of which row came first");
equal(runningDeploy({ links: [deployRow] }), deployRow,
    "with nothing running here, the deploy still has the chip");
equal(runningDeploy({ deploy: [runRow] }), runRow,
    "an older client reading the `deploy` field finds the run there too");
equal(runningDeploy({ links: [{ ...runRow, state: "ok" }, deployRow] }), deployRow,
    "a finished run does not hold the chip against a deploy that is still going");
equal(runningDeploy({ links: [] }), null, "an empty project draws nothing");
equal(runningDeploy(null), null, "and neither does a session nobody has read yet");

/* ==========================================================================
   A state nobody recognises
   ========================================================================== */

for (const state of ["paused", "queued", "none", "", undefined]) {
    equal(runningDeploy({ links: [{ ...runRow, state }] }), null,
        `a run in state ${JSON.stringify(state)} is not drawn as anything`);
}
const unknown = node();
unknown.href = "https://example.com/old";
drawDeploy(null, unknown);
equal(unknown.hidden, true, "and the chip is hidden rather than left holding the last row");
equal(unknown.innerHTML, "", "with nothing inside it");
equal(unknown.href, undefined, "and no address left over from whatever was there before");

/* ==========================================================================
   The phase, where the percentage goes
   ========================================================================== */

const running = node();
drawDeploy(runRow, running);
equal(running.hidden, false, "a running local run is on screen");
check(running.innerHTML.includes('<span class="pct">compiling</span>'),
    `the phase is drawn in place of the percentage — got ${running.innerHTML}`);
check(!/<span class="pct">\d+%<\/span>/.test(running.innerHTML),
    "so the percentage is not drawn as well");
check(running.innerHTML.includes('<span class="track"'),
    "and the bar stays: the phase says what, the bar says how far");
check(running.innerHTML.includes("--w:50%"),
    `the bar is still filled from elapsed against typical — got ${running.innerHTML}`);
equal(running.dataset.kind, "run",
    "the row's kind reaches the stylesheet, so a run is not read as a deploy");
equal(running.dataset.known, "true", "and its progress is known");
equal(running.title, "test compiling", "the hover says the phase rather than a percentage");
equal(running.attributes["aria-label"], "test compiling", "and so does the accessible name");

const noPhase = node();
drawDeploy({ ...runRow, phase: "" }, noPhase);
check(noPhase.innerHTML.includes('<span class="pct">50%</span>'),
    `without a phase the percentage comes back — got ${noPhase.innerHTML}`);
const blankPhase = node();
drawDeploy({ ...runRow, phase: "   " }, blankPhase);
check(blankPhase.innerHTML.includes('<span class="pct">50%</span>'),
    "whitespace is not a phase");
const markup = node();
drawDeploy({ ...runRow, phase: '<img src=x onerror="alert(1)">' }, markup);
check(!markup.innerHTML.includes("<img"),
    "producer text is drawn verbatim, which means escaped and never executed");

const deploy = node();
drawDeploy(deployRow, deploy);
equal(deploy.dataset.kind, "deploy", "a deploy keeps saying it is a deploy");
check(deploy.innerHTML.includes('<span class="pct">40%</span>'),
    `and still shows its percentage — got ${deploy.innerHTML}`);

/* ==========================================================================
   Nowhere to go is not a reason to disappear
   ========================================================================== */

const local = node();
local.href = "https://github.com/example/atrium/actions/runs/1";
drawDeploy(runRow, local);
equal(local.href, undefined, "a row with no url leaves no href behind");
check(local.innerHTML.includes('<span class="label">test</span>'),
    "and the chip is drawn anyway, which is the whole point of the row");

const logged = node();
drawDeploy({ ...runRow, url: "/tmp/clawdline-tests-123.log" }, logged);
equal(logged.href, undefined, "a filesystem path never becomes an href");
const scripted = node();
drawDeploy({ ...runRow, url: "javascript:alert(1)" }, scripted);
equal(scripted.href, undefined, "and neither does a script URL");
const linked = node();
drawDeploy(deployRow, linked);
equal(linked.href, deployRow.url, "an http address still opens");

/* ==========================================================================
   Progress arithmetic, shared with the deploy it borrows the chip from
   ========================================================================== */

// `now` was read a moment ago and the clock has moved since, so this one is read to the
// nearest hundredth rather than exactly — the only assertion here that has a clock in it.
const halfway = deployProgress({ startedAt: now - 144, typicalSeconds: 288 });
check(Math.abs(halfway - 0.5) < 0.01,
    `halfway through the typical time is halfway along the bar — got ${halfway}`);
equal(deployProgress({ startedAt: now - 900, typicalSeconds: 288 }), 1,
    "an overrunning run stops at the end of the bar rather than past it");
equal(deployProgress({ startedAt: now - 10 }), null,
    "a run that never said how long it takes has no percentage to draw");
equal(deployProgress({ startedAt: 0, typicalSeconds: 288 }), null,
    "and neither has one that never said when it began");
const waiting = node();
drawDeploy({ label: "build", kind: "run", state: "running" }, waiting);
equal(waiting.dataset.known, "false", "which is a chip that says it does not know");
check(waiting.innerHTML.includes('<span class="pct">…</span>'),
    `and shows that rather than a made-up number — got ${waiting.innerHTML}`);

if (failed) {
    console.error(`web run progress: ${checks} checks, at least one failed`);
    process.exit(1);
}
console.log(`web run progress: ${checks} checks passed`);
