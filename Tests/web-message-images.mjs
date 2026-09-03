import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

import {
    artifactPresentation, connectArtifactTile, createImageLightbox, reconcileArtifactTiles,
    releaseArtifactTile
} from "../Resources/web/app/js/view/transcript-images.js";

class FakeElement {
    constructor() {
        this.dataset = {};
        this.hidden = false;
        this.disabled = false;
        this._textContent = "";
        this.textWrites = 0;
        this._src = "";
        this.srcWrites = 0;
        this.listeners = {};
        this.focuses = 0;
        this.children = {};
        this.replacement = null;
        this.attributes = {};
        this.attributeWrites = {};
    }
    get textContent() { return this._textContent; }
    set textContent(value) { this._textContent = value; this.textWrites += 1; }
    get src() { return this._src; }
    set src(value) { this._src = value; this.srcWrites += 1; }
    addEventListener(kind, fn) { (this.listeners[kind] ||= []).push(fn); }
    emit(kind, event = {}) {
        event.target ||= this;
        for (const fn of this.listeners[kind] || []) fn(event);
    }
    querySelector(selector) { return this.children[selector] || null; }
    contains(node) {
        return this === node || Object.values(this.children).some(child => child.contains(node));
    }
    replaceWith(node) { this.replacement = node; }
    setAttribute(name, value) {
        this.attributes[name] = value;
        this.attributeWrites[name] = (this.attributeWrites[name] || 0) + 1;
    }
    removeAttribute(name) { if (name === "src") this._src = ""; }
    focus() { this.focuses += 1; }
}

const liveArtifact = {
    id: "11111111-2222-4333-8444-555555555555",
    media_type: "image/png", byte_count: 73, width: 640, height: 360,
    expires_at: 1_800_000_100
};

const transcriptSource = await readFile(
    new URL("../Resources/web/app/js/view/transcript.js", import.meta.url), "utf8");
const tileBuilder = transcriptSource.split("function artifactTilesHTML")[1]
    .split("function hydrateArtifactImages")[0];
assert.ok(tileBuilder.includes("artifactRenderQueue.push(artifact)"));
assert.ok(!/artifact\.(id|media_type|width|height|expires_at)/.test(tileBuilder),
    "attachment fields are queued as data and never interpolated into HTML");
assert.ok(!tileBuilder.includes('role="status"'),
    "an inert replacement placeholder cannot make a premature live announcement");
const reconciliationSource = transcriptSource.split("function replaceTranscriptContents")[1];
assert.ok(reconciliationSource.includes("reconcileArtifactTiles("));
assert.ok(reconciliationSource.includes("return { images: reconciliation.fresh"),
    "reconciliation returns only genuinely new image tiles to the render scheduler");
assert.ok(!reconciliationSource.includes("hydrateArtifactImages(reconciliation.fresh)"),
    "reconciliation cannot start image listeners or requests before meaningful paint");
const schedulerSource = transcriptSource.split("scheduleTranscriptRender({")[1]
    .split("// Said out loud")[0];
assert.ok(schedulerSource.includes("hydrate: hydrateArtifactImages"),
    "the scheduler owns the single post-meaningful-paint image connector");
assert.ok(reconciliationSource.includes("box.appendChild(template.content)"));
assert.ok(reconciliationSource.includes("child.remove()"),
    "old transcript rows leave only after reusable image nodes move into the new rows");

const live = artifactPresentation(liveArtifact, 1_800_000_000);
assert.equal(live.state, "loading");
assert.equal(live.url,
    "/v1/artifacts/images/11111111-2222-4333-8444-555555555555",
    "a client derives one relative same-origin retrieval route from the opaque id");
assert.ok(!live.url.startsWith("http") && !live.url.startsWith("data:"));

assert.equal(artifactPresentation(liveArtifact, liveArtifact.expires_at).state, "expired",
    "expires_at is expired at the boundary, before any image request");
assert.equal(artifactPresentation({ ...liveArtifact, id: "</button><script>" }, 1).state,
    "expired", "an unavailable or malformed reference fails visibly closed");

function tileFixture() {
    const tile = new FakeElement();
    tile.children[".message-image"] = new FakeElement();
    tile.children[".message-image-state"] = new FakeElement();
    return tile;
}

function reconcileFixture(previous, artifacts, now, activeElement = null) {
    const placeholders = artifacts.map(function (_, index) {
        const next = tileFixture();
        next.dataset.artifactSlot = String(index);
        return next;
    });
    const result = reconcileArtifactTiles(previous, placeholders, artifacts, {
        now: now, activeElement: activeElement
    });
    for (const fresh of result.fresh) {
        connectArtifactTile(fresh, artifacts[Number(fresh.dataset.artifactSlot)], {
            now: now, loadingLabel: "Loading…", expiredLabel: "Image expired"
        });
    }
    result.restoreFocus();
    return { ...result, tiles: placeholders.map(tile => tile.replacement || tile) };
}

const tile = tileFixture();
let opened = 0;
connectArtifactTile(tile, liveArtifact, {
    now: 1_800_000_000,
    loadingLabel: "Loading…",
    expiredLabel: "Image expired",
    open: function () { opened += 1; }
});
const thumb = tile.children[".message-image"];
assert.equal(tile.dataset.imageState, "loading");
assert.equal(thumb.hidden, true, "a request cannot expose a broken-image icon while pending");
assert.equal(thumb.src, live.url);
thumb.emit("load");
assert.equal(tile.dataset.imageState, "live");
assert.equal(thumb.hidden, false);
assert.equal(tile.disabled, false);
tile.emit("click");
assert.equal(opened, 1, "only a loaded thumbnail opens the preview");

const firstRequestCount = thumb.srcWrites;
const firstAnnouncementCount = tile.children[".message-image-state"].textWrites;
const firstLiveRoleCount = tile.children[".message-image-state"].attributeWrites.role;
const unchanged = reconcileFixture([tile], [liveArtifact], 1_800_000_001, tile);
assert.equal(unchanged.tiles[0], tile,
    "an unchanged artifact keeps the exact tile and image nodes");
assert.equal(unchanged.tiles[0].children[".message-image"], thumb);
assert.equal(thumb.srcWrites, firstRequestCount,
    "an unchanged render does not create another image request");
assert.equal(tile.children[".message-image-state"].textWrites, firstAnnouncementCount,
    "an unchanged render does not repeat the live status text transition");
assert.equal(tile.children[".message-image-state"].attributeWrites.role, firstLiveRoleCount,
    "an unchanged render does not recreate the role=status announcement");
assert.equal(tile.focuses, 1,
    "reconciliation restores focus to the unchanged tile after the transcript replacement");

const replacementArtifact = {
    ...liveArtifact, id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
};
const changed = reconcileFixture([tile], [replacementArtifact], 1_800_000_001);
assert.notEqual(changed.tiles[0], tile, "a changed opaque id gets a different tile node");
assert.equal(changed.tiles[0].children[".message-image"].srcWrites, 1,
    "a changed opaque id starts exactly one new request");

const metadataChange = reconcileFixture([tile], [{ ...liveArtifact, width: 641 }], 1_800_000_001);
assert.notEqual(metadataChange.tiles[0], tile,
    "changed safe semantic metadata cannot reuse the old tile");

const failed = tileFixture();
connectArtifactTile(failed, liveArtifact, {
    now: 1_800_000_000, loadingLabel: "Loading…", expiredLabel: "Image expired"
});
failed.children[".message-image"].emit("error");
assert.equal(failed.dataset.imageState, "expired",
    "404, 410 and conservative request failures share the explicit expired state");
assert.equal(failed.children[".message-image"].hidden, true);
assert.equal(failed.children[".message-image"].src, "",
    "the browser's broken-image rendering is removed");
assert.equal(failed.children[".message-image-state"].textContent, "Image expired");

const repeatedFailure = reconcileFixture([failed], [liveArtifact], 1_800_000_001);
assert.equal(repeatedFailure.tiles[0], failed,
    "a request failure stays visible without retrying on an unchanged render");
assert.equal(repeatedFailure.tiles[0].dataset.imageState, "expired");
assert.equal(repeatedFailure.tiles[0].children[".message-image"].srcWrites, 1);

const expired = reconcileFixture([tile], [liveArtifact], liveArtifact.expires_at);
assert.notEqual(expired.tiles[0], tile, "crossing expires_at replaces the formerly live tile");
assert.equal(expired.tiles[0].dataset.imageState, "expired");
assert.equal(expired.tiles[0].children[".message-image"].srcWrites, 0,
    "an expiry transition is visible without another byte request");
assert.equal(expired.tiles[0].children[".message-image-state"].textContent, "Image expired");

const removed = reconcileFixture([tile], [], 1_800_000_001);
assert.deepEqual(removed.tiles, [], "removing a message retains no stale artifact node");
assert.deepEqual(removed.reused, []);

const documentFixture = new FakeElement();
const dialog = new FakeElement();
const preview = new FakeElement();
const close = new FakeElement();
const trigger = new FakeElement();
const lightbox = createImageLightbox(dialog, preview, close, documentFixture);
lightbox.open(trigger, live.url);
assert.equal(dialog.hidden, false);
assert.equal(preview.src, live.url);
assert.equal(close.focuses, 1, "opening moves keyboard focus to the obvious close control");

let stopped = 0;
documentFixture.emit("keydown", {
    key: "Escape", preventDefault() { stopped += 1; }, stopPropagation() { stopped += 1; }
});
assert.equal(dialog.hidden, true, "Escape closes the lightbox");
assert.equal(trigger.focuses, 1, "Escape restores focus to the thumbnail that opened it");
assert.equal(stopped, 2, "the app's global Escape handler cannot also consume the close");

lightbox.open(trigger, live.url);
dialog.emit("click", { target: dialog });
assert.equal(dialog.hidden, true, "pressing the backdrop closes the lightbox");
assert.equal(trigger.focuses, 2, "backdrop close restores focus too");

lightbox.open(trigger, live.url);
close.emit("click");
assert.equal(dialog.hidden, true, "the labelled close button closes the lightbox");

let previewFailures = 0;
lightbox.open(trigger, live.url, function () { previewFailures += 1; });
preview.emit("error");
assert.equal(previewFailures, 1, "a failed enlarged request expires its source tile");
assert.equal(dialog.hidden, true, "a failed enlarged request closes the lightbox");
assert.equal(trigger.focuses, 4, "request failure restores focus to the source tile");

/* ---- a picture that has to be carried ------------------------------------
   On the cloud path this page is served by a hosted console, whose origin has no artifact route
   and cannot have one — the Mac is not reachable from it. So the transport hands the bytes over
   itself, and the tile has to be able to say why when it cannot. ------------------------- */

function carriedFixture(source, describeFailure) {
    const tile = tileFixture();
    const connected = connectArtifactTile(tile, liveArtifact, {
        now: 1_800_000_000, loadingLabel: "Loading…", expiredLabel: "Image expired",
        source: source, describeFailure: describeFailure
    });
    return { tile, connected, image: tile.children[".message-image"],
        status: tile.children[".message-image-state"] };
}

let released = 0;
const carried = carriedFixture(function (artifact) {
    assert.equal(artifact.id, liveArtifact.id,
        "the transport is asked for the artifact this tile is bound to");
    return Promise.resolve({ url: "blob:carried-1",
        release: function () { released += 1; } });
});
assert.equal(carried.tile.dataset.imageState, "loading",
    "a carried picture announces itself as loading, not as absent");
assert.equal(carried.image.src, "", "and nothing has been requested from this page's own origin");
await carried.connected.carried;
assert.equal(carried.image.src, "blob:carried-1",
    "the transport's own URL is what the tile renders");
assert.notEqual(carried.image.src, live.url,
    "the same-origin route is never built on a transport that supplies its own bytes");
carried.image.emit("load");
assert.equal(carried.tile.dataset.imageState, "live");
assert.equal(carried.tile.disabled, false, "a carried picture opens in the lightbox like any other");

// The whole point of the exercise: a picture that cannot cross says so, in words, where a
// broken-image icon would have said "you did something wrong".
const refused = carriedFixture(
    function () {
        return Promise.reject(Object.assign(new Error("too big"),
            { code: "image_too_large_for_cloud" }));
    },
    function (code, artifact) {
        assert.equal(code, "image_too_large_for_cloud");
        assert.equal(artifact.byte_count, liveArtifact.byte_count,
            "the sentence is given the picture, so it can name its size");
        return "Too large to send (12.0 MB)";
    });
await refused.connected.carried;
assert.equal(refused.tile.dataset.imageState, "unavailable",
    "a picture that did not cross is its own state, distinct from an expired one");
assert.equal(refused.status.textContent, "Too large to send (12.0 MB)");
assert.equal(refused.status.attributes.role, "status",
    "and it is announced rather than only drawn");
assert.equal(refused.image.src, "",
    "no src is left behind for the browser to draw its broken-image icon from");
assert.equal(refused.image.hidden, true);
assert.equal(refused.tile.disabled, true, "there is nothing to enlarge");

// Expiry keeps its own wording. A code the page has no sentence for falls back to it rather than
// printing an English identifier onto a page in one of thirteen other languages.
const fellBack = carriedFixture(
    function () {
        return Promise.reject(Object.assign(new Error("gone"), { code: "artifact_expired" }));
    },
    function () { return ""; });
await fellBack.connected.carried;
assert.equal(fellBack.tile.dataset.imageState, "expired");
assert.equal(fellBack.status.textContent, "Image expired");

const unnamed = carriedFixture(function () { return Promise.resolve({ }); },
    function () { return "Image did not arrive"; });
await unnamed.connected.carried;
assert.equal(unnamed.tile.dataset.imageState, "unavailable",
    "a delivery with no URL in it is a failure, not a blank tile");

// Carried bytes are this page's to give back. A tile the next render had no use for is about to
// be removed, and its object URL would otherwise hold the picture until a reload.
released = 0;
const holder = carriedFixture(function () {
    return Promise.resolve({ url: "blob:carried-2", release: function () { released += 1; } });
});
await holder.connected.carried;
const keptTiles = reconcileArtifactTiles([holder.tile],
    [Object.assign(tileFixture(), { dataset: { artifactSlot: "0" } })],
    [liveArtifact], { now: 1_800_000_001 });
assert.equal(keptTiles.reused[0], holder.tile, "an unchanged picture keeps its tile");
assert.equal(released, 0, "and keeps its bytes, because it is still on screen");
const droppedTiles = reconcileArtifactTiles([holder.tile], [], [], { now: 1_800_000_001 });
assert.deepEqual(droppedTiles.dropped, [holder.tile]);
assert.equal(released, 1, "a tile the next render did not want hands its picture back");
releaseArtifactTile(holder.tile);
assert.equal(released, 1, "and releasing twice is not two revocations");

released = 0;
const expiring = carriedFixture(function () {
    return Promise.resolve({ url: "blob:carried-3", release: function () { released += 1; } });
});
await expiring.connected.carried;
expiring.connected.expire();
assert.equal(released, 1, "an expiring tile hands its picture back on the way out");

// And the wiring in the view above it, which decides *whether* to carry at all.
const hydrationSource = transcriptSource.split("function carriedArtifactSource")[1]
    .split("function artifactTilesHTML")[0];
assert.ok(hydrationSource.includes('typeof api.image !== "function"'),
    "the direct path is chosen by asking the transport, not by guessing at the URL");
assert.ok(hydrationSource.includes("URL.createObjectURL"),
    "carried bytes become an object URL rather than a data: URL four thirds their size");
assert.ok(hydrationSource.includes("URL.revokeObjectURL"),
    "and the tile is given the way to hand them back");
assert.ok(hydrationSource.includes("T.webImageTooLarge")
    && hydrationSource.includes("T.webImageUnavailable"),
    "both sentences come from the string table, never from a literal on this page");
assert.ok(/artifact_expired[\s\S]*return ""/.test(hydrationSource),
    "expiry is handed back to the expired branch instead of being renamed");
const hydrator = transcriptSource.split("function hydrateArtifactImages")[1]
    .split("\n}")[0];
assert.ok(hydrator.includes("source: source") && hydrator.includes("describeFailure:"),
    "every fresh tile is given the transport's delivery and its vocabulary together");
assert.ok(transcriptSource.includes("artifactRenderSession = S.openId"),
    "and the pictures are asked of the session whose transcript is on screen");

console.log("web-message-images: ok, including pictures carried over a transport of their own");
