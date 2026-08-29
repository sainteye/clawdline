import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

import {
    artifactPresentation, connectArtifactTile, createImageLightbox, reconcileArtifactTiles
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
assert.ok(reconciliationSource.includes("hydrateArtifactImages(reconciliation.fresh)"),
    "only genuinely new image tiles acquire listeners and a request");
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

console.log("web-message-images: ok");
