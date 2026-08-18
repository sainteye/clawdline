#!/usr/bin/env node
// Film a web page, frame by frame, the way `clawdline://filmstrip` films the panel.
//
// The rest of the README is drawn by the app itself — see tools/shoot-assets.sh, which opens a
// `clawdline://` URL and lets AppKit render offscreen. Two of the pictures cannot come from there,
// because what they show is a **browser**: the interface you reach from a phone, and the
// notification that lands on it. So this is the same idea aimed at Chrome. It drives a throwaway
// Chrome profile over the DevTools protocol, emulates a phone, plays a fixed storyboard, and
// writes `f0000.png…` for ffmpeg — exactly the handover the app's filmstrip already uses.
//
// Why a **throwaway profile** and not the Chrome you have open:
//
//   - Notification permission has to be *granted* rather than asked for, and granting it in
//     somebody's real profile is changing their browser to take a screenshot.
//   - A real profile carries extensions, a theme, and whatever is pinned — none of which should
//     end up in a repository's README.
//   - It has to be reproducible on a machine that has never run this. A fresh profile is the same
//     profile everywhere.
//
// Nothing here needs Screen Recording permission: the frames come from the renderer over the
// protocol, not from the screen, so what is on your display at the time does not matter. That is
// the same property tools/shoot-assets.sh is built around.
//
//   node tools/shoot-web.js --url "file://$PWD/Resources/web/index.html?write=1" \
//        --dir .shoot/web --script web --fps 16
//
// It is not usually run by hand: tools/shoot-assets.sh has `web`, `web-wide` and `web-push`
// targets that call it with the arguments the README's pictures were taken with.
//
// Storyboards live under `SCRIPTS`, about two thirds of the way down. Their timings are fixed on
// purpose — a README picture has to come out the same on the next machine that reshoots it — and
// where they wait for the page rather than for a clock, it is because the alternative is a
// picture of a loading skeleton on the day somebody's machine is busy.

const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

/* ---- arguments ----------------------------------------------------------- */

const argv = (() => {
    const out = {};
    const a = process.argv.slice(2);
    for (let i = 0; i < a.length; i++) {
        if (!a[i].startsWith("--")) continue;
        const key = a[i].slice(2);
        const next = a[i + 1];
        if (next === undefined || next.startsWith("--")) { out[key] = "1"; continue; }
        out[key] = next; i++;
    }
    return out;
})();

const OPT = {
    url: argv.url || "http://127.0.0.1:7717/?mock=1&write=1",
    dir: argv.dir || ".shoot/web",
    script: argv.script || "web",
    // A **minimum**, not a length. The storyboard takes as long as it takes — it waits on the
    // page rather than on a clock — so a fixed length either cuts the ending off or pads it.
    // Given, this holds the last frame until the clip is at least this long.
    seconds: Number(argv.seconds || 0),
    fps: Number(argv.fps || 20),
    width: Number(argv.width || 390),
    height: Number(argv.height || 844),
    scale: Number(argv.scale || 2),
    // Headless keeps a window off somebody's screen while this runs, which matters more here than
    // it does for the app: this thing subscribes to push, and a headful Chrome would put a real
    // banner over whatever the reader is doing. `--headful` is the escape hatch for debugging.
    headful: argv.headful === "1",
    chrome: argv.chrome || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    keepProfile: argv["keep-profile"] === "1",
    grant: (argv.grant || "").split(",").filter(Boolean),   // e.g. --grant notifications
    settle: Number(argv.settle || 1500),
    // The page's words come from `GET /v1/strings`, which answers on **`Accept-Language`** rather
    // than on the app's own setting — so the language of a picture is set here, per shot, and
    // shooting an English README does not mean reaching into somebody's config and putting their
    // Mac back into English while they are using it.
    lang: argv.lang || "en-US",
    // Run an expression against the page just before the recording stops and print what it
    // answered. For working out why a storyboard is looking at the wrong thing without having to
    // guess from the frames.
    eval: argv.eval || "",
    saying: argv.saying || "",     // for the `open` storyboard below
    dwell: Number(argv.dwell || 4000),
    // A still instead of a filmstrip. The page is the same page; what changes is that a laptop
    // gets both panes at once, which is worth one picture and not worth a second animation.
    png: argv.png || "",
    desktop: argv.desktop === "1",
};

const log = (...m) => console.error("[shoot-web]", ...m);

/* ---- a very small DevTools protocol client ------------------------------- */
//
// Node 24 has `WebSocket` and `fetch` built in, so this file has no dependencies and nothing to
// install. That is deliberate: a shooting tool that needs `npm install` before it works is a
// shooting tool that stops being run.

class CDP {
    constructor(ws) {
        this.ws = ws;
        this.next = 1;
        this.pending = new Map();
        this.listeners = [];
        ws.addEventListener("message", (ev) => {
            const msg = JSON.parse(ev.data);
            if (msg.id && this.pending.has(msg.id)) {
                const { resolve, reject } = this.pending.get(msg.id);
                this.pending.delete(msg.id);
                if (msg.error) reject(new Error(msg.method + ": " + msg.error.message));
                else resolve(msg.result);
                return;
            }
            for (const fn of this.listeners) fn(msg);
        });
    }

    static async open(url) {
        const ws = new WebSocket(url);
        await new Promise((resolve, reject) => {
            ws.addEventListener("open", resolve, { once: true });
            ws.addEventListener("error", () => reject(new Error("could not open " + url)), { once: true });
        });
        return new CDP(ws);
    }

    send(method, params = {}, sessionId) {
        const id = this.next++;
        const message = { id, method, params };
        if (sessionId) message.sessionId = sessionId;
        this.ws.send(JSON.stringify(message));
        return new Promise((resolve, reject) => this.pending.set(id, { resolve, reject }));
    }

    on(fn) { this.listeners.push(fn); return () => { this.listeners = this.listeners.filter((f) => f !== fn); }; }

    /** Wait for one event, with a deadline — a hang here is a shoot that never finishes. */
    once(method, ms = 15000) {
        return new Promise((resolve, reject) => {
            const off = this.on((msg) => { if (msg.method === method) { off(); clearTimeout(timer); resolve(msg.params); } });
            const timer = setTimeout(() => { off(); reject(new Error("timed out waiting for " + method)); }, ms);
        });
    }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/* ---- the browser --------------------------------------------------------- */

async function launch() {
    const profile = fs.mkdtempSync(path.join(os.tmpdir(), "clawdline-shoot-chrome-"));
    const port = 9333 + Math.floor(Math.random() * 400);
    const args = [
        "--remote-debugging-port=" + port,
        "--user-data-dir=" + profile,
        "--no-first-run", "--no-default-browser-check", "--no-service-autorun",
        "--disable-background-timer-throttling",
        "--disable-renderer-backgrounding",
        "--disable-backgrounding-occluded-windows",
        "--hide-scrollbars",
        "--force-color-profile=srgb",
        "--window-size=" + OPT.width + "," + OPT.height,
        "about:blank",
    ];
    if (!OPT.headful) args.unshift("--headless=new");
    log("chrome", OPT.headful ? "(headful)" : "(headless)", "profile", profile);
    const child = spawn(OPT.chrome, args, { stdio: ["ignore", "ignore", "pipe"] });
    child.stderr.on("data", (d) => {
        const s = String(d);
        if (/ERROR|FATAL/.test(s) && !/Failed to (fetch|read)/.test(s)) log("chrome:", s.trim().split("\n")[0]);
    });

    // Poll rather than sleep: the port is open when it is open, and a fixed wait is either too
    // long every time or too short on the day somebody is watching.
    let version = null;
    for (let i = 0; i < 100; i++) {
        try { version = await (await fetch("http://127.0.0.1:" + port + "/json/version")).json(); break; }
        catch (e) { await sleep(100); }
    }
    if (!version) throw new Error("Chrome did not open a debugging port");
    return { child, profile, port, version };
}

/* ---- frames -------------------------------------------------------------- */
//
// `Page.startScreencast` pushes a frame whenever the renderer paints one, which is not a fixed
// rate: a still page sends nothing at all, and a spring sends a burst. So the frames are kept with
// the timestamps Chrome gives them and resampled onto an even timeline at the end — every tick
// takes the last frame painted at or before it. That is what makes a 20fps GIF come out at 20fps
// rather than at whatever the page felt like.

class Recorder {
    constructor(cdp, sessionId) { this.cdp = cdp; this.sessionId = sessionId; this.frames = []; }

    async start() {
        this.off = this.cdp.on((msg) => {
            if (msg.method !== "Page.screencastFrame") return;
            const p = msg.params;
            this.frames.push({ at: p.metadata.timestamp, data: p.data });
            this.cdp.send("Page.screencastFrameAck", { sessionId: p.sessionId }, this.sessionId).catch(() => {});
        });
        // **One frame of the page as it is, before anything happens.**
        //
        // A screencast only sends a frame when the renderer paints one, so a page sitting still
        // sends nothing at all — and the first frame to arrive is the first frame of the *change*.
        // The clip then opens on a notification that is already there, with the arrival it was
        // supposed to be about missing entirely. Taking one deliberately is the whole fix: the
        // quiet beat before the thing happens is a frame like any other and has to be asked for.
        const opening = await this.cdp.send("Page.captureScreenshot", { format: "png" }, this.sessionId);
        this.began = Date.now() / 1000;
        this.frames.push({ at: this.began, data: opening.data });

        await this.cdp.send("Page.startScreencast", {
            format: "png",
            maxWidth: Math.round(OPT.width * OPT.scale),
            maxHeight: Math.round(OPT.height * OPT.scale),
            // **Not every frame.** A phone-sized page painting at 60fps is a megabyte of base64 PNG
            // sixty times a second coming back up one WebSocket, and it does not arrive: the socket
            // fills, the protocol stops answering, and the whole run hangs with no error anywhere —
            // which is exactly how the first version of this failed. Chrome paints at 60, the GIF
            // is 20, so ask for one in three and the rest is never encoded at all.
            everyNthFrame: Math.max(1, Math.round(60 / OPT.fps)),
        }, this.sessionId);
    }

    async stop() {
        await this.cdp.send("Page.stopScreencast", {}, this.sessionId).catch(() => {});
        if (this.off) this.off();
    }

    /** Resample onto an even timeline and write f0000.png… */
    write(dir, seconds, fps) {
        fs.rmSync(dir, { recursive: true, force: true });
        fs.mkdirSync(dir, { recursive: true });
        if (!this.frames.length) throw new Error("no frames were painted");
        const first = this.began;
        const total = Math.round(seconds * fps);
        let cursor = 0;
        for (let i = 0; i < total; i++) {
            const want = first + i / fps;
            while (cursor + 1 < this.frames.length && this.frames[cursor + 1].at <= want) cursor++;
            fs.writeFileSync(path.join(dir, "f" + String(i).padStart(4, "0") + ".png"),
                             Buffer.from(this.frames[cursor].data, "base64"));
        }
        return total;
    }
}

/* ---- the storyboard vocabulary ------------------------------------------- */

function makeStage(cdp, session) {
    const evaluate = async (expression, awaitPromise = true) => {
        const r = await cdp.send("Runtime.evaluate", {
            expression, awaitPromise, returnByValue: true,
        }, session);
        if (r.exceptionDetails) {
            throw new Error("page: " + (r.exceptionDetails.exception?.description || r.exceptionDetails.text));
        }
        return r.result.value;
    };

    const stage = {
        evaluate,
        wait: (ms) => sleep(ms),

        /** Wait for a selector to exist and be visible, rather than sleeping and hoping. */
        async until(selector, ms = 10000) {
            log("  · until", selector);
            const deadline = Date.now() + ms;
            for (;;) {
                const ok = await evaluate(`(() => { const e = document.querySelector(${JSON.stringify(selector)});
                    return !!(e && e.getClientRects().length); })()`);
                if (ok) return;
                if (Date.now() > deadline) throw new Error("never appeared: " + selector);
                await sleep(80);
            }
        },

        /** Tap the one whose text says this — the list sorts itself, so position is not identity. */
        async tapSaying(selector, text) {
            const n = await evaluate(`(() => {
                const list = [...document.querySelectorAll(${JSON.stringify(selector)})];
                return list.findIndex((e) => (e.textContent || "").indexOf(${JSON.stringify(text)}) >= 0);
            })()`);
            if (n < 0) throw new Error("nothing saying " + JSON.stringify(text) + " in " + selector);
            return stage.tap(selector, n);
        },

        /** Wait until an expression in the page goes true. Returns false rather than throwing. */
        async untilTrue(expression, ms = 10000) {
            const deadline = Date.now() + ms;
            for (;;) {
                if (await evaluate("!!(" + expression + ")")) return true;
                if (Date.now() > deadline) return false;
                await sleep(120);
            }
        },

        /** Wait until something's text says this. */
        async untilSays(selector, text, ms = 8000) {
            const deadline = Date.now() + ms;
            for (;;) {
                const said = await evaluate(`(document.querySelector(${JSON.stringify(selector)}) || {}).textContent || ""`);
                if (said.indexOf(text) >= 0) return true;
                if (Date.now() > deadline) return false;
                await sleep(80);
            }
        },

        /**
         * Open the session with this label, and make sure that is the one that opened.
         *
         * **The list moves underneath the tap.** The fixtures re-sort every second — a session
         * that starts working climbs above one that stopped — so the row measured a frame ago is
         * not always the row still under that point when the touch lands, and the session that
         * opens is then somebody else's. It comes out as a transcript that never matches the
         * storyboard, a thousand frames later, with nothing in the log to say why.
         *
         * So: press it, check the pane's own heading, and press it again if it went astray.
         * Three goes and then it gives up loudly, because a shooting tool that quietly films the
         * wrong thing is the whole problem this repository's asset scripts exist to avoid.
         */
        async openSaying(label, tries = 3) {
            for (let i = 0; i < tries; i++) {
                await stage.tapSaying("#rows li", label);
                if (await stage.untilSays("#detail-name", label, 4000)) return;
                log("  · opened the wrong session — going back and trying again");
                await stage.tap("#back");
                await sleep(700);
            }
            throw new Error("could not open the session labelled " + JSON.stringify(label));
        },

        /** A real press at the element's centre, so `:active`, ripples and touch handlers all fire. */
        async tap(selector, index = 0) {
            log("  · tap", selector);
            const box = await evaluate(`(() => {
                const list = document.querySelectorAll(${JSON.stringify(selector)});
                const e = list[${index}];
                if (!e) return null;
                const r = e.getBoundingClientRect();
                return { x: r.left + r.width / 2, y: r.top + Math.min(r.height / 2, 30) };
            })()`);
            if (!box) throw new Error("nothing to tap: " + selector + "[" + index + "]");
            // Touch, not mouse. `Input.dispatchMouseEvent` against a page in mobile emulation goes
            // through the gesture machinery, and the call does not come back until whatever it
            // started has settled — which for a list that scrolls is never. A touch pair is what a
            // phone sends anyway, and it answers immediately.
            const touch = [{ x: box.x, y: box.y, radiusX: 12, radiusY: 12, force: 1 }];
            await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: touch }, session);
            await sleep(70);
            await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] }, session);
        },

        /** Type into whatever has focus, a character at a time, at a human rate. */
        async type(text, perChar = 55) {
            log("  · type", JSON.stringify(text.slice(0, 24) + (text.length > 24 ? "…" : "")));
            for (const ch of text) {
                await cdp.send("Input.insertText", { text: ch }, session);
                await sleep(perChar + Math.random() * 30);
            }
        },

        async focus(selector) {
            await evaluate(`document.querySelector(${JSON.stringify(selector)}).focus()`);
        },

        /**
         * Scroll something into the middle of the screen and make sure it stayed there.
         *
         * **The transcript follows new lines while it is at the bottom**, and a refetch lands
         * every second — so a smooth scroll away from the end can be overtaken and snapped back
         * before it arrives, leaving the storyboard pressing a button that is no longer on screen.
         * Scroll, look, and scroll again.
         */
        async reveal(selector, index = 0, ms = 800) {
            log("  · reveal", selector, index);
            for (let i = 0; i < 3; i++) {
                const found = await evaluate(`(() => {
                    const e = document.querySelectorAll(${JSON.stringify(selector)})[${index}];
                    if (!e) return false;
                    e.scrollIntoView({ block: "center", behavior: ${i ? '"auto"' : '"smooth"'} });
                    return true;
                })()`);
                if (!found) throw new Error("nothing to reveal: " + selector + "[" + index + "]");
                await sleep(ms);
                const settled = await evaluate(`(() => {
                    const e = document.querySelectorAll(${JSON.stringify(selector)})[${index}];
                    if (!e) return false;
                    const r = e.getBoundingClientRect();
                    return r.top > 40 && r.bottom < innerHeight - 40;
                })()`);
                if (settled) return;
            }
            throw new Error("would not stay in view: " + selector + "[" + index + "]");
        },

        /** Back to the newest, the way somebody who has finished reading gets to the box. */
        async bottom(selector, ms = 1100) {
            log("  · bottom", selector);
            await evaluate(`(() => {
                const e = document.querySelector(${JSON.stringify(selector)});
                e.scrollTo({ top: e.scrollHeight, behavior: "smooth" });
            })()`);
            await sleep(ms);
        },

        /** Smooth scroll, because a jump cut in a GIF reads as a dropped frame. */
        async scroll(selector, by, ms = 700) {
            log("  · scroll", selector, by);
            await evaluate(`(() => {
                const e = document.querySelector(${JSON.stringify(selector)});
                e.scrollBy({ top: ${by}, behavior: "smooth" });
            })()`);
            await sleep(ms);
        },
    };
    return stage;
}

/* ---- storyboards --------------------------------------------------------- */

const SCRIPTS = {
    /**
     * The interface, on a phone.
     *
     * One loop has to answer "what is this for", so it is the four things in order: the list with
     * several projects and one of them waiting, opening that one, the transcript with a run of
     * tool calls folded to a line, and typing an answer back.
     */
    async web(stage) {
        await stage.until("#rows li");
        await stage.wait(1600);                      // read the list — the states move on their own
        // Picked by label, never by position or by state. The fixtures move on their own, so "the
        // second row" and "whichever one is waiting" are both a different session depending on
        // the moment the frame is taken — and one of the sessions that drifts through `waiting`
        // has an empty transcript, which is the one thing this picture must not open.
        //
        // The order is the argument for the feature: **the one that is asking first**, because
        // being told a session wants you is the reason to reach for a phone at all; then a
        // session with real work behind it, where the answer gets typed.
        await stage.openSaying("the signup flow keeps 500ing");
        // Wait for the transcript rather than for a clock, and for a *real* entry: the loading
        // skeleton is drawn out of `.entry` elements too, so that the real lines land where the
        // bars were — wait on `.entry` alone and the storyboard films the placeholder.
        await stage.until("#tx .entry:not(.skel-entry)");
        await stage.wait(2400);
        await stage.tap("#back");
        await stage.wait(900);
        await stage.openSaying("investigate the webhook");
        // The folded run of tool calls: opened, because a pill reading "2 steps · Bash · Read" is
        // only interesting once somebody has seen it turn into the two.
        await stage.until(".entry.folded button.pill");
        await stage.wait(700);
        await stage.reveal(".entry.folded button.pill", 2, 700);
        await stage.tap(".entry.folded button.pill", 2);
        await stage.wait(1500);
        // Back to the end before typing. Not decoration: the transcript only follows new lines
        // while it is already at the bottom — read your way up it and it stays where you left it,
        // which is right, and which is also why the answer would otherwise be sent off-screen.
        await stage.bottom(".tx-scroll", 900);
        await stage.focus("#msg");
        await stage.wait(250);
        await stage.type("run the migration on staging too", 45);
        await stage.wait(600);
        await stage.tap("#send");
        await stage.wait(2200);
    },

    /**
     * The notification, on a phone's home screen.
     *
     * The phone is drawn by tools/phone/index.html. **What is inside the banner is not.** The
     * page subscribes this browser to Web Push for real, against the app's own VAPID key, and
     * asks the Mac to send one; the words on screen are whatever came back down from the push
     * service. If nothing arrives, this fails — there is no drawn stand-in to fall back to,
     * because a picture of a notification the code did not send is a lie that outlives the
     * afternoon somebody took it.
     *
     * Subscribing happens in `prepare`, before the camera is rolling: it takes a second or two
     * of talking to Google, and none of that is worth filming.
     */
    push: {
        async prepare(stage) {
            await stage.until("#phone");
            const endpoint = await stage.evaluate("window.PhoneMock.ready()");
            log("subscribed:", String(endpoint).slice(0, 52) + "…");
        },
        async play(stage) {
            await stage.wait(900);
            log("asked the Mac to send one:", JSON.stringify(await stage.evaluate("window.PhoneMock.fire()")));
            // Wait for the push itself rather than for a clock. It crosses the internet twice —
            // to the push service and back to this browser — and "usually under a second" is not
            // something to build a picture on.
            const landed = await stage.untilTrue("window.PhoneMock.arrived() !== null", 20000);
            if (!landed) throw new Error("no push arrived within 20s — nothing to photograph");
            log("it landed:", JSON.stringify(await stage.evaluate("window.PhoneMock.arrived()")));
            await stage.wait(3600);                     // long enough to read
            await stage.evaluate("window.PhoneMock.hide()");
            await stage.wait(1400);
            log("cleanup:", await stage.evaluate("window.PhoneMock.forget()"));
        },
    },

    /** Open one session and sit on it. For a still of the transcript, and for `--eval`. */
    async open(stage) {
        await stage.until("#rows li");
        await stage.wait(1200);
        if (OPT.saying) await stage.tapSaying("#rows li", OPT.saying);
        await stage.wait(OPT.dwell);
    },

    /** Nothing but the settle — for a still, or for looking at a page by hand. */
    async still(stage) { await stage.wait(1200); },
};

/* ---- main ---------------------------------------------------------------- */

let browser = null;

(async () => {
    if (!SCRIPTS[OPT.script]) throw new Error("no storyboard called " + OPT.script);
    if (!fs.existsSync(OPT.chrome)) throw new Error("Chrome is not at " + OPT.chrome + " — pass --chrome");

    // A shoot that hangs is worse than a shoot that fails: it holds a Chrome open, writes nothing,
    // and the person who started it finds out two minutes later from a timeout somewhere else.
    const watchdog = setTimeout(() => {
        log("failed: watchdog — nothing finished within " + (OPT.seconds + 120) + "s");
        try { browser.child.kill("SIGKILL"); } catch (e) {}
        process.exit(1);
    }, (OPT.seconds + 120) * 1000);
    watchdog.unref();

    browser = await launch();
    let failed = null;
    try {
        const cdp = await CDP.open(browser.version.webSocketDebuggerUrl);

        // Permission before the page loads, not after: a page that asks on load gets the answer
        // it was going to get, and a prompt in a screenshot is not the feature.
        const origin = new URL(OPT.url).origin;
        if (OPT.grant.length) {
            await cdp.send("Browser.grantPermissions", { origin, permissions: OPT.grant });
            log("granted", OPT.grant.join(", "), "for", origin);
        }

        const { targetId } = await cdp.send("Target.createTarget", { url: "about:blank" });
        const { sessionId } = await cdp.send("Target.attachToTarget", { targetId, flatten: true });

        await cdp.send("Page.enable", {}, sessionId);
        await cdp.send("Runtime.enable", {}, sessionId);
        await cdp.send("Emulation.setDeviceMetricsOverride", {
            width: OPT.width, height: OPT.height, deviceScaleFactor: OPT.scale, mobile: !OPT.desktop,
            screenWidth: OPT.width, screenHeight: OPT.height,
        }, sessionId);
        await cdp.send("Emulation.setTouchEmulationEnabled",
                       { enabled: !OPT.desktop, maxTouchPoints: 5 }, sessionId);
        // `acceptLanguage` alone is what `/v1/strings` reads. The user agent has to be passed with
        // it because this call replaces both, and an empty one would announce a browser that is
        // not there.
        await cdp.send("Emulation.setUserAgentOverride", {
            userAgent: browser.version["User-Agent"], acceptLanguage: OPT.lang,
        }, sessionId);

        // Anything the page logs is worth seeing while a storyboard is being written, and worth
        // seeing even more when one stops working a year from now.
        cdp.on((msg) => {
            if (msg.method !== "Runtime.consoleAPICalled" || msg.sessionId !== sessionId) return;
            const text = (msg.params.args || []).map((a) => a.value ?? a.description ?? "").join(" ");
            if (text) log("page:", text);
        });

        log("open", OPT.url);
        await cdp.send("Page.navigate", { url: OPT.url }, sessionId);
        await cdp.once("Page.loadEventFired").catch(() => {});
        await sleep(OPT.settle);

        const stage = makeStage(cdp, sessionId);
        // A storyboard is either a function or a pair. `prepare` runs before the camera does,
        // for the setting-up that is real work and dull footage.
        const script = SCRIPTS[OPT.script];
        const prepare = typeof script === "function" ? null : script.prepare;
        const play = typeof script === "function" ? script : script.play;
        if (prepare) await prepare(stage);

        // A still takes the same storyboard and keeps only the end of it. Same page, same
        // fixtures, same words — one frame instead of two hundred.
        if (OPT.png) {
            await play(stage);
            const shot = await cdp.send("Page.captureScreenshot", { format: "png" }, sessionId);
            fs.mkdirSync(path.dirname(path.resolve(OPT.png)), { recursive: true });
            fs.writeFileSync(path.resolve(OPT.png), Buffer.from(shot.data, "base64"));
            log("wrote", path.resolve(OPT.png));
            clearTimeout(watchdog);
            try { browser.child.kill("SIGTERM"); } catch (e) {}
            await sleep(300);
            try { browser.child.kill("SIGKILL"); } catch (e) {}
            if (!OPT.keepProfile) fs.rmSync(browser.profile, { recursive: true, force: true });
            return;
        }

        const recorder = new Recorder(cdp, sessionId);
        await recorder.start();
        const began = Date.now();
        await play(stage);
        if (OPT.eval) log("eval:", JSON.stringify(await stage.evaluate(OPT.eval)));
        const played = (Date.now() - began) / 1000;
        // Hold the last frame rather than cutting on the final action: a GIF that loops straight
        // out of a keypress has no beat at the end and reads as truncated.
        if (played < OPT.seconds) await sleep((OPT.seconds - played) * 1000);
        await recorder.stop();

        const dir = path.resolve(OPT.dir);
        const n = recorder.write(dir, Math.max(OPT.seconds, played), OPT.fps);
        log("wrote", n, "frames to", dir, "(storyboard ran", played.toFixed(1) + "s)");
    } catch (e) {
        failed = e;
    } finally {
        try { browser.child.kill("SIGTERM"); } catch (e) {}
        await sleep(300);
        try { browser.child.kill("SIGKILL"); } catch (e) {}
        if (!OPT.keepProfile) fs.rmSync(browser.profile, { recursive: true, force: true });
    }
    clearTimeout(watchdog);
    if (failed) { log("failed:", failed.message); process.exit(1); }
})();
