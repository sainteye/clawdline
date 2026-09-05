import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

import { createImageLightbox, createImageZoom } from "./transcript-images.js";

/*
 * What a finger is allowed to do to an enlarged picture.
 *
 * Four screenshots arrived on a phone on 2026-09-05 and there was no way to read any of them: the
 * lightbox drew the picture fitted to the screen and that was the whole of it, and the page-wide
 * `user-scalable=no` in `index.html` means the browser's own pinch is not there to fall back on.
 * So the zoom is the lightbox's own, and this file is the part of it that can be checked without
 * a browser: the arithmetic, and then the same arithmetic driven through the gesture handlers with
 * stand-in nodes.
 *
 * The arithmetic is worth holding separately from the handlers because every mistake it can make
 * is invisible in a screenshot and obvious in a number. A picture that drifts a few pixels under
 * the fingers looks like a picture; one that cannot be dragged back from the far corner looks like
 * a bug in the picture rather than in a `Math.max`.
 */

let checks = 0;
function ok(condition, message) { assert.ok(condition, message); checks += 1; }
function equal(actual, expected, message) { assert.equal(actual, expected, message); checks += 1; }
function close(actual, expected, message, tolerance = 0.001) {
    assert.ok(Math.abs(actual - expected) <= tolerance,
        message + " (expected " + expected + ", got " + actual + ")");
    checks += 1;
}

/* ---- the arithmetic ------------------------------------------------------ */

// A frame 400x300 with a 16:9 picture fitted into it by width, and a source four times that wide.
// Every number below is derived from these four, so a changed fixture cannot leave an assertion
// quietly agreeing with itself.
const FRAME = { left: 0, top: 0, width: 400, height: 300 };
const CONTENT = { width: 400, height: 225 };
const NATURAL = { width: 1600, height: 900 };
const CENTRE = { x: FRAME.left + FRAME.width / 2, y: FRAME.top + FRAME.height / 2 };

function fittedZoom() {
    const zoom = createImageZoom();
    zoom.layout({ frame: FRAME, content: CONTENT, natural: NATURAL });
    return zoom;
}

/**
 * Where a picture-local point — measured from the picture's middle at scale 1 — is on screen.
 *
 * Both of these take a `{ scale, x, y }` rather than a zoom, because the second half of this file
 * reads exactly the same three numbers back out of the transform the handlers wrote. The anchor
 * question is then asked once and answered against the model and against the DOM.
 */
function onScreen(view, offsetX, offsetY) {
    return {
        x: CENTRE.x + view.x + view.scale * offsetX,
        y: CENTRE.y + view.y + view.scale * offsetY
    };
}

/** And the inverse: which picture-local point is under a point on screen right now. */
function underPointer(view, clientX, clientY) {
    return {
        x: (clientX - CENTRE.x - view.x) / view.scale,
        y: (clientY - CENTRE.y - view.y) / view.scale
    };
}

const fresh = fittedZoom();
equal(fresh.state().scale, 1, "a picture the stylesheet has already fitted starts at exactly fit");
equal(fresh.zoomed(), false, "and fit is not zoomed");
equal(fresh.limits().min, 1, "there is nothing below fit worth showing, so that is the floor");
equal(fresh.limits().max, 4, "the ceiling is the source's own pixels — 1600 across a 400 box");

// The ceiling is bounded at both ends. A picture no larger than its frame would otherwise cap at
// 1x and never zoom at all; a 6000px photo would cap somewhere no finger can steer.
const small = createImageZoom();
small.layout({ frame: FRAME, content: CONTENT, natural: { width: 200, height: 113 } });
equal(small.limits().max, 4, "a source smaller than the frame still zooms to the standing ceiling");
const huge = createImageZoom();
huge.layout({ frame: FRAME, content: CONTENT, natural: { width: 12000, height: 6750 } });
equal(huge.limits().max, 8, "and an enormous one stops where a finger can still find its way back");

const bounded = fittedZoom();
bounded.scaleTo(99, CENTRE.x, CENTRE.y);
equal(bounded.state().scale, 4, "zooming past the ceiling stops at the ceiling");
bounded.scaleTo(0.2, CENTRE.x, CENTRE.y);
equal(bounded.state().scale, 1, "and zooming below the fit stops at the fit");
equal(bounded.zoomed(), false, "which is the state the double tap has to be able to come back to");

// Anchoring: whatever is under the finger stays under the finger. This is the one piece of the
// arithmetic a person notices immediately when it is wrong — zoom in on the top left corner of a
// screenshot and arrive at the middle of it — and the one a screenshot cannot show.
const anchored = fittedZoom();
const held = underPointer(anchored.state(), 350, 210);
anchored.scaleTo(2, 350, 210);
equal(anchored.state().scale, 2, "the anchored zoom reaches the scale it was asked for");
close(onScreen(anchored.state(), held.x, held.y).x, 350, "the point under the finger stays under it (x)");
close(onScreen(anchored.state(), held.x, held.y).y, 210, "the point under the finger stays under it (y)");

const stepped = fittedZoom();
const cornerHeld = underPointer(stepped.state(), 40, 30);
stepped.scaleBy(1.5, 40, 30);
stepped.scaleBy(1.5, 40, 30);
close(stepped.state().scale, 2.25, "two relative steps compose into one absolute scale");
close(onScreen(stepped.state(), cornerHeld.x, cornerHeld.y).x, 40, "and the anchor survives both (x)");
close(onScreen(stepped.state(), cornerHeld.x, cornerHeld.y).y, 30, "and the anchor survives both (y)");

// Panning. At scale 2 the picture is 800x450 inside a 400x300 frame, so there are exactly 200
// pixels of slack each way horizontally and 75 vertically: enough that an edge never comes inside
// the frame, and not one pixel more.
const panned = fittedZoom();
panned.scaleTo(2, CENTRE.x, CENTRE.y);
equal(panned.panBy(1000, 1000), true, "a zoomed picture moves under a drag");
assert.deepEqual(
    { x: panned.state().x, y: panned.state().y }, { x: 200, y: 75 },
    "and stops with its own edges on the frame's, never inside them");
checks += 1;
panned.panBy(-10000, -10000);
assert.deepEqual({ x: panned.state().x, y: panned.state().y }, { x: -200, y: -75 },
    "the far corner is the mirror of the near one");
checks += 1;
equal(panned.panBy(-10, 0), true, "a drag that has room still reports that it moved something");
equal(panned.panBy(-10000, 0), true, "a drag that runs out of room part-way still moved");
equal(panned.panBy(-10000, 0), false,
    "and one against a bound it is already on moved nothing, which is how a drag is told from a tap");

// The picture the reader is looking at fits the screen. Dragging it can only slide it out from
// under them, so it does not move — and that is also what leaves the tap free to close the preview.
const still = fittedZoom();
equal(still.panBy(120, 120), false, "an unzoomed picture refuses to pan");
assert.deepEqual({ x: still.state().x, y: still.state().y }, { x: 0, y: 0 },
    "and stays exactly where the stylesheet put it");
checks += 1;

// Coming back down recentres, whatever it was dragged to on the way up. Without this the reader
// zooms out and gets the fitted picture hanging off the side of the frame.
const returned = fittedZoom();
returned.scaleTo(3, 380, 280);
returned.panBy(-200, -100);
returned.scaleTo(1, 380, 280);
assert.deepEqual(returned.state(), { scale: 1, x: 0, y: 0 },
    "the fit is centred by construction rather than by whatever the last drag left behind");
checks += 1;

const toggled = fittedZoom();
equal(toggled.toggle(340, 200), true, "a double tap on a fitted picture zooms it");
close(toggled.state().scale, 2.5, "to a scale that is a reading distance, not the ceiling");
const toggleHeld = underPointer(toggled.state(), 340, 200);
close(onScreen(toggled.state(), toggleHeld.x, toggleHeld.y).x, 340, "anchored where the tap landed");
toggled.toggle(340, 200);
assert.deepEqual(toggled.state(), { scale: 1, x: 0, y: 0 },
    "and a second double tap comes all the way back");
checks += 1;

const forgotten = fittedZoom();
forgotten.scaleTo(3, 380, 280);
forgotten.panBy(-50, -50);
forgotten.reset();
assert.deepEqual(forgotten.state(), { scale: 1, x: 0, y: 0 },
    "reset is the whole state, so the next picture cannot inherit this one's");
checks += 1;

// Nothing has been measured: no frame, no picture, no idea where either edge is. Zooming is
// harmless and panning is not — a bound computed from a zero-width frame is unbounded.
const unmeasured = createImageZoom();
unmeasured.scaleTo(3, 200, 150);
equal(unmeasured.panBy(500, 500), false, "an unmeasured picture cannot be panned anywhere");
assert.deepEqual({ x: unmeasured.state().x, y: unmeasured.state().y }, { x: 0, y: 0 },
    "and holds no offset it could not have clamped");
checks += 1;

/* ---- the handlers -------------------------------------------------------- */

class Node {
    constructor(name) {
        this.name = name;
        this.dataset = {};
        this.style = {};
        this.hidden = false;
        this.listeners = {};
        this.focuses = 0;
        this._src = "";
    }
    get src() { return this._src; }
    set src(value) { this._src = value; }
    addEventListener(kind, fn) { (this.listeners[kind] ||= []).push(fn); }
    emit(kind, event = {}) {
        event.target ||= this;
        event.preventDefault ||= function () { event.defaultPrevented = true; };
        for (const fn of this.listeners[kind] || []) fn(event);
        return event;
    }
    removeAttribute(name) { if (name === "src") this._src = ""; }
    focus() { this.focuses += 1; }
}

/** A lightbox whose frame really is 400x300 with a 400x225 picture centred in it. */
function lightboxFixture() {
    const dialog = new Node("dialog");
    const frame = new Node("frame");
    const image = new Node("image");
    const closeButton = new Node("close");
    const trigger = new Node("trigger");
    const doc = new Node("document");
    let clock = 10_000;

    dialog.hidden = true;
    dialog.querySelector = (selector) => (selector === ".image-lightbox-frame" ? frame : null);
    frame.getBoundingClientRect = () => ({ ...FRAME, right: 400, bottom: 300 });
    image.offsetWidth = CONTENT.width;
    image.offsetHeight = CONTENT.height;
    image.naturalWidth = NATURAL.width;
    image.naturalHeight = NATURAL.height;

    const lightbox = createImageLightbox(dialog, image, closeButton, doc, {
        now: () => clock
    });
    return {
        dialog, frame, image, closeButton, trigger, doc, lightbox,
        tick(ms) { clock += ms; },
        open(failed) {
            lightbox.open(trigger, "/v1/artifacts/images/one", failed);
            image.emit("load");
        },
        view() {
            const match = /^translate\((\S+)px, (\S+)px\) scale\((\S+)\)$/
                .exec(image.style.transform || "");
            assert.ok(match, "the picture carries a transform this test can read: "
                + image.style.transform);
            return { x: Number(match[1]), y: Number(match[2]), scale: Number(match[3]) };
        }
    };
}

function touch(x, y) { return { clientX: x, clientY: y }; }
function touchEvent(touches, changed) {
    return { touches: touches, changedTouches: changed || touches };
}

const fit = lightboxFixture();
fit.open();
assert.deepEqual(fit.view(), { x: 0, y: 0, scale: 1 },
    "an opened picture is drawn fitted and centred before anything is touched");
checks += 1;
equal(fit.image.dataset.zoomed, "false", "and says so, because the cursor and the CSS read it");
equal(fit.closeButton.focuses, 1, "opening still moves focus to the close control");

// The desktop half: a wheel notch away from the reader zooms in around the pointer.
const wheeled = lightboxFixture();
wheeled.open();
const wheelHeld = underPointer(wheeled.view(), 340, 200);
wheeled.dialog.emit("wheel", { deltaY: -240, deltaMode: 0, clientX: 340, clientY: 200 });
ok(wheeled.view().scale > 1, "a wheel notch away from the reader zooms in");
close(onScreen(wheeled.view(), wheelHeld.x, wheelHeld.y).x, 340,
    "around the pointer rather than the middle (x)");
close(onScreen(wheeled.view(), wheelHeld.x, wheelHeld.y).y, 200,
    "around the pointer rather than the middle (y)");
const afterOneNotch = wheeled.view().scale;
wheeled.dialog.emit("wheel", { deltaY: 240, deltaMode: 0, clientX: 340, clientY: 200 });
close(wheeled.view().scale, 1, "and the opposite notch comes back to the fit");
const wheelStopped = wheeled.dialog.emit("wheel",
    { deltaY: -240, deltaMode: 0, clientX: 340, clientY: 200 });
equal(wheelStopped.defaultPrevented, true,
    "a wheel over the preview is the preview's, or the page behind it scrolls away");

// A trackpad pinch arrives as a wheel with ctrlKey and a much smaller delta. Reading it at the
// scroll rate makes the gesture crawl, which is what "pinch does nothing" feels like.
const pinchWheel = lightboxFixture();
pinchWheel.open();
pinchWheel.dialog.emit("wheel",
    { deltaY: -12, deltaMode: 0, ctrlKey: true, clientX: 200, clientY: 150 });
const pinchStep = pinchWheel.view().scale;
const scrollWheel = lightboxFixture();
scrollWheel.open();
scrollWheel.dialog.emit("wheel", { deltaY: -12, deltaMode: 0, clientX: 200, clientY: 150 });
ok(pinchStep > scrollWheel.view().scale,
    "the same delta with ctrl held is a pinch and moves further than a scroll of it");
ok(pinchStep < 2, "and still lands somewhere a reader can steer from");

// Firefox reports the wheel in lines. Read as pixels it zooms by a factor of sixteen per notch.
const lines = lightboxFixture();
lines.open();
lines.dialog.emit("wheel", { deltaY: -15, deltaMode: 1, clientX: 200, clientY: 150 });
close(lines.view().scale, afterOneNotch,
    "fifteen lines and two hundred and forty pixels are the same gesture");

// Two fingers, opened from 100 apart to 200 apart around the same middle.
const pinched = lightboxFixture();
pinched.open();
pinched.dialog.emit("touchstart", touchEvent([touch(150, 150), touch(250, 150)]));
const pinchMove = pinched.dialog.emit("touchmove",
    touchEvent([touch(100, 150), touch(300, 150)]));
close(pinched.view().scale, 2, "fingers twice as far apart is a picture twice as large");
close(pinched.view().x, 0, "and a middle that did not move leaves the picture where it was");
equal(pinchMove.defaultPrevented, true, "a two-finger move is the lightbox's, not the page's");

// The same pinch, carried sideways: the scale keeps what is between the fingers still and the
// fingers then take the picture with them.
const carried = lightboxFixture();
carried.open();
carried.dialog.emit("touchstart", touchEvent([touch(150, 150), touch(250, 150)]));
carried.dialog.emit("touchmove", touchEvent([touch(60, 150), touch(260, 150)]));
close(carried.view().scale, 2, "the span still decides the scale");
close(carried.view().x, -40,
    "and the middle moving 40px left carries the picture exactly 40px left with it");

// One finger, twice, in the same place: the gesture the phone has instead of a wheel.
const tapped = lightboxFixture();
tapped.open();
function tapAt(fixture, x, y) {
    fixture.dialog.emit("touchstart", touchEvent([touch(x, y)]));
    return fixture.dialog.emit("touchend", touchEvent([], [touch(x, y)]));
}
tapAt(tapped, 340, 200);
equal(tapped.view().scale, 1, "one tap is not a gesture and changes nothing");
const secondTap = tapAt(tapped, 341, 201);
close(tapped.view().scale, 2.5, "a second tap in the same place zooms to reading distance");
equal(secondTap.defaultPrevented, true,
    "and is consumed, or the browser's synthesised click closes what was just zoomed");
const thirdTap = tapAt(tapped, 341, 201);
equal(tapped.view().scale, 2.5, "a third tap starts a new pair rather than finishing the old one");
ok(!thirdTap.defaultPrevented, "so the third tap leaves the page's own click alone");
tapAt(tapped, 341, 201);
assert.deepEqual(tapped.view(), { x: 0, y: 0, scale: 1 }, "and its own second tap comes back");
checks += 1;

const slow = lightboxFixture();
slow.open();
tapAt(slow, 200, 150);
slow.tick(900);
tapAt(slow, 200, 150);
equal(slow.view().scale, 1, "two taps a second apart are two taps");

const apart = lightboxFixture();
apart.open();
tapAt(apart, 60, 60);
tapAt(apart, 360, 260);
equal(apart.view().scale, 1, "and two taps at opposite corners are two taps");

// A finger that wanders is a drag, not a tap, even when it comes back.
const wandered = lightboxFixture();
wandered.open();
wandered.dialog.emit("touchstart", touchEvent([touch(200, 150)]));
wandered.dialog.emit("touchmove", touchEvent([touch(260, 150)]));
wandered.dialog.emit("touchend", touchEvent([], [touch(200, 150)]));
tapAt(wandered, 200, 150);
equal(wandered.view().scale, 1, "a wandering finger cannot be half of a double tap");

// Dragging: only once there is something to drag.
const dragged = lightboxFixture();
dragged.open();
dragged.dialog.emit("touchstart", touchEvent([touch(200, 150)]));
dragged.dialog.emit("touchmove", touchEvent([touch(120, 150)]));
assert.deepEqual(dragged.view(), { x: 0, y: 0, scale: 1 },
    "a finger dragged across a fitted picture moves nothing");
checks += 1;
dragged.dialog.emit("touchend", touchEvent([], [touch(120, 150)]));
dragged.dialog.emit("wheel", { deltaY: -600, deltaMode: 0, clientX: 200, clientY: 150 });
const zoomedScale = dragged.view().scale;
ok(zoomedScale > 1, "zoom in first");
dragged.dialog.emit("touchstart", touchEvent([touch(200, 150)]));
const dragMove = dragged.dialog.emit("touchmove", touchEvent([touch(150, 150)]));
close(dragged.view().x, -50, "and now the same finger carries the picture one for one");
equal(dragMove.defaultPrevented, true, "a drag on a zoomed picture is not the page's to scroll");
dragged.dialog.emit("touchmove", touchEvent([touch(-9000, 150)]));
close(dragged.view().x, -(zoomedScale * CONTENT.width - FRAME.width) / 2,
    "a finger that leaves the screen still stops the picture on the frame's edge");

// The mouse's own drag, which is the same arithmetic through a different set of events.
const mouseDragged = lightboxFixture();
mouseDragged.open();
mouseDragged.dialog.emit("pointerdown", { pointerType: "mouse", clientX: 200, clientY: 150 });
mouseDragged.doc.emit("pointermove", { pointerType: "mouse", clientX: 100, clientY: 150 });
equal(mouseDragged.view().x, 0, "a held button on a fitted picture drags nothing");
mouseDragged.doc.emit("pointerup", { pointerType: "mouse" });
mouseDragged.dialog.emit("wheel", { deltaY: -600, deltaMode: 0, clientX: 200, clientY: 150 });
mouseDragged.dialog.emit("pointerdown", { pointerType: "mouse", clientX: 200, clientY: 150 });
equal(mouseDragged.image.dataset.panning, "true", "the cursor says the picture is being held");
mouseDragged.doc.emit("pointermove", { pointerType: "mouse", clientX: 170, clientY: 150 });
close(mouseDragged.view().x, -30, "and the picture follows the pointer");
mouseDragged.doc.emit("pointerup", { pointerType: "mouse" });
equal(mouseDragged.image.dataset.panning, "false", "and lets go when the button does");
mouseDragged.doc.emit("pointermove", { pointerType: "mouse", clientX: 60, clientY: 150 });
close(mouseDragged.view().x, -30, "a pointer that keeps moving after the button is not a drag");

// A pointer event that came from a finger is left to the touch handlers, which can do more with
// it. Handling both is a picture that pans twice as fast as the finger.
const notTwice = lightboxFixture();
notTwice.open();
notTwice.dialog.emit("wheel", { deltaY: -600, deltaMode: 0, clientX: 200, clientY: 150 });
notTwice.dialog.emit("touchstart", touchEvent([touch(200, 150)]));
notTwice.dialog.emit("pointerdown", { pointerType: "touch", clientX: 200, clientY: 150 });
notTwice.dialog.emit("touchmove", touchEvent([touch(180, 150)]));
notTwice.doc.emit("pointermove", { pointerType: "touch", clientX: 180, clientY: 150 });
close(notTwice.view().x, -20, "one finger moves the picture once");

/* ---- everything the lightbox already did --------------------------------- */

const kept = lightboxFixture();
kept.open();
kept.dialog.emit("click", { target: kept.dialog });
equal(kept.dialog.hidden, true, "pressing the backdrop still closes the preview");
equal(kept.trigger.focuses, 1, "and still gives focus back to the thumbnail that opened it");

kept.open();
let stopped = 0;
kept.doc.emit("keydown", {
    key: "Escape", preventDefault() { stopped += 1; }, stopPropagation() { stopped += 1; }
});
equal(kept.dialog.hidden, true, "Escape still closes it");
equal(stopped, 2, "and the app's own Escape handler still cannot also consume the close");

kept.open();
kept.closeButton.emit("click");
equal(kept.dialog.hidden, true, "the labelled close button still closes it");

let failures = 0;
kept.open(function () { failures += 1; });
kept.image.emit("error");
equal(failures, 1, "a picture that fails to enlarge still expires its tile");
equal(kept.dialog.hidden, true, "and still closes the preview it cannot fill");

// The one existing behaviour zooming can break, and the reason it can: a drag that starts on the
// picture and ends on the padding around it is delivered as a click on their common ancestor,
// which is the dialog — the exact test `event.target === dialog` was making.
const dragThenClick = lightboxFixture();
dragThenClick.open();
dragThenClick.dialog.emit("wheel", { deltaY: -600, deltaMode: 0, clientX: 200, clientY: 150 });
dragThenClick.dialog.emit("pointerdown", { pointerType: "mouse", clientX: 200, clientY: 150 });
dragThenClick.doc.emit("pointermove", { pointerType: "mouse", clientX: 120, clientY: 150 });
dragThenClick.doc.emit("pointerup", { pointerType: "mouse" });
dragThenClick.dialog.emit("click", { target: dragThenClick.dialog });
equal(dragThenClick.dialog.hidden, false, "a drag that ends on the backdrop is not a press on it");
dragThenClick.dialog.emit("pointerdown", { pointerType: "mouse", clientX: 200, clientY: 150 });
dragThenClick.doc.emit("pointerup", { pointerType: "mouse" });
dragThenClick.dialog.emit("click", { target: dragThenClick.dialog });
equal(dragThenClick.dialog.hidden, true,
    "and the very next press on the backdrop, having moved nothing, closes it");

// Pressing the picture itself never closed the preview and still does not, zoomed or not.
const pressedPicture = lightboxFixture();
pressedPicture.open();
pressedPicture.dialog.emit("wheel", { deltaY: -600, deltaMode: 0, clientX: 200, clientY: 150 });
pressedPicture.dialog.emit("click", { target: pressedPicture.image });
equal(pressedPicture.dialog.hidden, false, "a press on the enlarged picture is not a press to close");

// Closing hands the next picture a clean state. Without this the second screenshot of four opens
// at 2.5x with the first one's corner on screen.
const reopened = lightboxFixture();
reopened.open();
reopened.dialog.emit("wheel", { deltaY: -600, deltaMode: 0, clientX: 340, clientY: 220 });
ok(reopened.view().scale > 1, "zoomed");
reopened.dialog.emit("click", { target: reopened.dialog });
reopened.open();
assert.deepEqual(reopened.view(), { x: 0, y: 0, scale: 1 },
    "and the next picture opens fitted and centred like the first");
checks += 1;

/* ---- the parts of this that live in the stylesheet and the page ---------- */

const css = await readFile(new URL("../../css/transcript.css", import.meta.url), "utf8");
const lightboxCSS = css.slice(css.indexOf(".image-lightbox {"), css.indexOf(".sr-only {"));
ok(/\.image-lightbox\s*\{[^}]*touch-action:\s*none/.test(lightboxCSS),
    "the preview claims every touch: without this iOS Safari takes the two-finger gesture first");
ok(/transform-origin:\s*50%\s+50%/.test(lightboxCSS),
    "the origin the anchor arithmetic assumes is written down rather than inherited");
ok(/user-select:\s*none/.test(lightboxCSS),
    "dragging a picture must not select the text behind it");
ok(/\[data-zoomed="true"\]/.test(lightboxCSS) && /\[data-panning="true"\]/.test(lightboxCSS),
    "the cursor follows the two states the handlers publish");

const page = await readFile(new URL("../../../index.html", import.meta.url), "utf8");
ok(page.includes(
    '<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">'),
    "the page-wide viewport lock is untouched — the zoom is this lightbox's, not the browser's");
ok(/<img id="image-lightbox-image"[^>]*draggable="false"/.test(page),
    "the enlarged picture cannot be picked up by the desktop's own image drag");

console.log("transcript-images: ok (" + checks + " checks) — zoom arithmetic, pinch, "
    + "double tap, drag, wheel, and everything the lightbox already did");
