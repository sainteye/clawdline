import { esc } from "../core/esc.js";
import { T, fill } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { toast } from "../core/util.js";
import { closingID } from "../view/list.js";
import { renderComposer } from "../view/composer.js";

/* ---- pictures ------------------------------------------------------------ */

/**
 * Photograph a whiteboard, or the error on the other screen, and send it into the session.
 *
 * Everything goes up as a `data:` URL beside the text, and the server decodes it, re-encodes to
 * PNG and hands Claude Code a real image — it arrives as `[Image #1]`, not as a path somebody
 * has to go and read.
 *
 * **The picture is shrunk here rather than sent as it was taken.** A modern phone photograph is
 * three to six megabytes, base64 adds a third to that, and the server's body limit is twenty:
 * two photographs from a 48-megapixel camera would simply fail, and slowly, over the mobile
 * connection somebody is standing on. A long edge of 1600px is more than Claude Code can use —
 * text on a whiteboard is legible well below that — and it turns a forty-second upload into an
 * instant one. Screenshots are kept as PNG unless that comes out too big to send, because a
 * screenshot is text, and JPEG is exactly the wrong thing to do to text.
 */
export var Shots = (function () {
    var LONG_EDGE = 1600;
    var QUALITY = 0.82;
    var MAX_COUNT = 6;
    // Measured on the wire, which is what the limit is about: these are the lengths of the
    // `data:` strings, base64 and all. The server refuses a body over 20MB, so this leaves room
    // for the message, the JSON around it and the headers.
    var MAX_EACH = 5 << 20;
    var MAX_TOTAL = 15 << 20;

    var list = [];
    var seq = 0;
    var busy = 0;

    function total() {
        return list.reduce(function (sum, shot) { return sum + shot.url.length; }, 0);
    }

    /// A bitmap from a file, however this browser is willing to give one.
    function decode(file) {
        if (window.createImageBitmap) {
            // `from-image` so a photograph taken sideways is not drawn sideways: the orientation
            // lives in EXIF, and a canvas does not read EXIF.
            return createImageBitmap(file, { imageOrientation: "from-image" })
                .catch(function () { return createImageBitmap(file); });
        }
        return new Promise(function (done, fail) {
            var url = URL.createObjectURL(file);
            var img = new Image();
            img.onload = function () { URL.revokeObjectURL(url); done(img); };
            img.onerror = function () { URL.revokeObjectURL(url); fail(new Error("not a picture")); };
            img.src = url;
        });
    }

    function asDataURL(file) {
        return new Promise(function (done, fail) {
            var reader = new FileReader();
            reader.onload = function () { done(String(reader.result)); };
            reader.onerror = function () { fail(new Error("could not be read")); };
            reader.readAsDataURL(file);
        });
    }

    function shrink(file) {
        return decode(file).then(function (bitmap) {
            var scale = Math.min(1, LONG_EDGE / Math.max(bitmap.width, bitmap.height));
            var canvas = document.createElement("canvas");
            canvas.width = Math.max(1, Math.round(bitmap.width * scale));
            canvas.height = Math.max(1, Math.round(bitmap.height * scale));
            canvas.getContext("2d").drawImage(bitmap, 0, 0, canvas.width, canvas.height);
            if (bitmap.close) bitmap.close();
            var url = file.type === "image/png" ? canvas.toDataURL("image/png") : "";
            if (!url || url.length > MAX_EACH) url = canvas.toDataURL("image/jpeg", QUALITY);
            return { url: url, w: canvas.width, h: canvas.height };
        }).catch(function () {
            // The browser cannot decode it — a HEIC from an iPhone is this, in everything but
            // Safari. The bytes still go: the server re-encodes whatever it is given, and that
            // re-encoding is also how it checks the file really is an image.
            return asDataURL(file).then(function (url) { return { url: url, w: 0, h: 0 }; });
        });
    }

    function draw() {
        els.shots.innerHTML = list.map(function (shot) {
            return '<div class="shot"><img src="' + shot.url + '" alt="' + esc(shot.name) + '">' +
                '<button type="button" class="drop" data-shot="' + shot.id +
                '" aria-label="' + esc(fill(T.webRemoveShot, { name: shot.name })) + '">×</button></div>';
        }).join("");
        renderComposer();
    }

    els.shots.addEventListener("click", function (ev) {
        var handle = ev.target.closest ? ev.target.closest("[data-shot]") : null;
        if (!handle) return;
        var id = handle.getAttribute("data-shot");
        list = list.filter(function (shot) { return String(shot.id) !== id; });
        draw();
    });

    return {
        count: function () { return list.length; },
        busy: function () { return busy > 0; },
        urls: function () { return list.map(function (shot) { return shot.url; }); },
        clear: function () { list = []; draw(); },

        add: function (files) {
            var wanted = Array.prototype.filter.call(files || [], isPicture);
            if (!wanted.length) { toast(T.webShotsOnlyPictures, true); return; }
            if (list.length + wanted.length > MAX_COUNT) {
                toast(fill(T.webShotsTooMany, { n: MAX_COUNT }), true);
                wanted = wanted.slice(0, Math.max(0, MAX_COUNT - list.length));
                if (!wanted.length) return;
            }
            busy += wanted.length;
            renderComposer();
            wanted.forEach(function (file) {
                shrink(file).then(function (shot) {
                    if (shot.url.length > MAX_EACH) {
                        toast(T.webShotTooBig, true);
                        return;
                    }
                    if (total() + shot.url.length > MAX_TOTAL) {
                        toast(T.webShotsTooBig, true);
                        return;
                    }
                    list.push({ id: ++seq, url: shot.url, name: file.name || "picture" });
                }).catch(function () {
                    toast(T.webShotUnreadable, true);
                }).then(function () {
                    busy -= 1;
                    draw();
                });
            });
        }
    };
})();

els.attach.addEventListener("click", function () { els.pick.click(); });
els.pick.addEventListener("change", function () {
    Shots.add(els.pick.files);
    // Cleared so that picking the same file twice in a row still counts as a change.
    els.pick.value = "";
});

/** A file the attachment list would take. A HEIC sometimes arrives with an empty type, so the
 *  extension gets a say. Shared, because a paste is sorted by this same question in two places
 *  and the two answers disagreeing is how a paste ends up belonging to neither of them. */
function isPicture(file) {
    return /^image\//.test(file.type) || /\.(hei[cf]|jpe?g|png|gif|webp)$/i.test(file.name || "");
}

export function carriesPicture(data) {
    var files = data && data.files;
    return !!files && Array.prototype.some.call(files, isPicture);
}

// Paste, because copying a screenshot and pressing paste is how this is done everywhere else.
// Not while the filter box has the focus: that one takes text and nothing else.
document.addEventListener("paste", function (ev) {
    if (!S.openId || !S.write || closingID === S.openId) return;
    if (document.activeElement === els.filter) return;
    // The composer runs first and takes anything with words in it. Cancelling here after that
    // would cancel the paste it just accepted, so a paste already spoken for is left alone.
    if (ev.defaultPrevented) return;
    // A picture, and not merely a file: a clipboard often carries something alongside the words
    // that is no use here, and cancelling the paste for it left the words nowhere to go and a
    // complaint about pictures on the screen instead.
    if (!carriesPicture(ev.clipboardData)) return;
    ev.preventDefault();
    Shots.add(ev.clipboardData.files);
});

// Drag and drop. The whole detail pane is the target — somebody dragging a photograph at this
// page is aiming at the conversation, not at a 30-pixel button — and the document swallows the
// drops that miss, because the browser's own answer to those is to navigate away from the page.
(function dropPictures() {
    var pane = els["pane-detail"];
    var depth = 0;
    function carriesFiles(ev) {
        var dt = ev.dataTransfer;
        if (!dt) return false;
        if (dt.types && Array.prototype.indexOf.call(dt.types, "Files") >= 0) return true;
        return !!(dt.files && dt.files.length);
    }
    document.addEventListener("dragover", function (ev) { if (carriesFiles(ev)) ev.preventDefault(); });
    document.addEventListener("drop", function (ev) { if (carriesFiles(ev)) ev.preventDefault(); });
    pane.addEventListener("dragenter", function (ev) {
        if (!carriesFiles(ev) || !S.openId || !S.write || closingID === S.openId) return;
        depth += 1;
        pane.classList.add("dropping");
    });
    pane.addEventListener("dragleave", function () {
        depth = Math.max(0, depth - 1);
        if (!depth) pane.classList.remove("dropping");
    });
    pane.addEventListener("drop", function (ev) {
        depth = 0;
        pane.classList.remove("dropping");
        if (!carriesFiles(ev)) return;
        ev.preventDefault();
        if (!S.openId || !S.write || closingID === S.openId) {
            toast(T.webShotNeedsSession, true); return;
        }
        Shots.add(ev.dataTransfer.files);
    });
})();
