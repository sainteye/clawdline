import { T, fill } from "../core/i18n.js";
import { els } from "../core/dom.js";
import { toast } from "../core/util.js";
import { drawSpinner, setVoiceSpin, spinPhase } from "../core/pixels.js";
import { api } from "../net/api.js";
import { renderComposer } from "../view/composer.js";
import { appendMsg } from "./composer.js";

/* ---- dictation ----------------------------------------------------------- */

/**
 * Say it instead of typing it.
 *
 * The phone records, the Mac transcribes, the words land in the box — and stop there. Nothing
 * on this path sends; see `appendMsg` for why that is the whole design rather than a step that
 * was left out.
 *
 * **The recording is not what gets uploaded.** A `MediaRecorder` blob is mp4/aac on iOS Safari
 * and webm/opus on Android Chrome, and the Mac has no ffmpeg — an Opus file would arrive as
 * bytes nothing there can open. So the browser is made to decode what it just recorded, which
 * is the one format question every browser can answer about itself, and what goes up is raw
 * PCM: little-endian Int16, mono, 16 kHz, base64. Sixteen is not a preference, it is the only
 * rate whisper.cpp takes.
 *
 * The resample is done by an `OfflineAudioContext` and not by dropping every third sample.
 * Decimation without a filter folds everything above 8 kHz back down into the speech as a
 * warble, and the failure is invisible from here — it does not sound broken, it just transcribes
 * slightly wrong, which is the worst way for this to be wrong.
 *
 * **Three minutes is the ceiling, and it is enforced here rather than by the server.** A minute
 * of 16 kHz Int16 is 1.9MB, 2.6MB once base64 has had it; the body limit at the other end is
 * twenty. Somebody talking for eight minutes should be stopped at three with their words kept,
 * not allowed to reach the limit and told at the end that all of it was wasted.
 */
export var Voice = (function () {
    var RATE = 16000;
    var MAX_SECONDS = 180;
    /// Under a quarter of a second is a mis-tap. The Mac drops these silently for the same
    /// reason — see "Silence costs nothing" in `docs/whisper.md` — and spinning up a 600MB
    /// model to read a button press is worse than saying nothing happened.
    var MIN_SECONDS = 0.25;
    /// When the count starts saying it is nearly out of room, rather than only finding out.
    var NEAR_SECONDS = 30;
    /// How long a transcription has to run before the row explains itself. The first one after a
    /// reboot spends twelve seconds reading the model off disk before it hears a word, and eight
    /// is comfortably past every ordinary one and comfortably short of that.
    var SLOW_SECONDS = 8;

    var state = "off";          // off | opening | recording | reading
    var stream = null;
    var recorder = null;
    var chunks = [];
    var since = 0;              // when the *current* count started, which is not when it began
    var ticker = null;
    var drawn = "";             // the state the row was built for
    /**
     * Which recording this is.
     *
     * Everything below is asynchronous — a permission prompt, a decode, a request that can take
     * twelve seconds — and Cancel has to be able to mean it while any of them is outstanding.
     * Rather than trying to abort each one, the number moves on and every callback checks
     * whether it still belongs to the recording that is happening. A cancelled transcription
     * still arrives; it simply arrives for nobody.
     */
    var token = 0;
    /**
     * The Mac's answer to "there is no Whisper here", kept for exactly one more press.
     *
     * Without this, the second attempt costs another minute of talking to be told the same
     * thing. With it kept forever, somebody who installs Whisper while the page is open can
     * never use it. So it is said once and then forgotten, and the press after that tries again.
     */
    var whisperless = null;

    /* ---- can this happen at all -------------------------------------------- */

    /**
     * Why not, in the reader's own language, or "" for no reason at all.
     *
     * **Over plain http `navigator.mediaDevices` does not exist.** Not "fails", not "asks and is
     * refused" — the object is absent, so a page that only checked for the function would report
     * a browser too old to record when what is actually wrong is the address it was opened from.
     * That is a sentence somebody can act on and the other one is not, so the protocol is asked
     * about first.
     */
    function why() {
        var secure = window.isSecureContext !== false;
        if (!secure) return T.webVoiceInsecure;
        if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) return T.webVoiceUnsupported;
        if (typeof window.MediaRecorder === "undefined") return T.webVoiceUnsupported;
        if (!window.AudioContext && !window.webkitAudioContext) return T.webVoiceUnsupported;
        if (!window.OfflineAudioContext && !window.webkitOfflineAudioContext) return T.webVoiceUnsupported;
        if (typeof api.voice !== "function") return T.webVoiceUnsupported;
        return "";
    }

    /// What the browser refused with. The names are the ones in the spec; the older WebKit and
    /// Firefox spellings are there because a device with no microphone at all is one of the
    /// four states this has to be able to tell apart, and it is the one that predates the rename.
    function refused(e) {
        var name = (e && e.name) || "";
        if (name === "NotAllowedError" || name === "PermissionDeniedError" || name === "SecurityError") {
            return T.webVoiceDenied;
        }
        if (name === "NotFoundError" || name === "DevicesNotFoundError") return T.webVoiceNoMic;
        if (name === "NotReadableError" || name === "TrackStartError") return T.webVoiceInUse;
        return T.webVoiceFailed;
    }

    /// What the Mac refused with. `reason` is carried through `jsonFetch` for this one route:
    /// "install Whisper" and "Whisper is here but has no model" are two different afternoons.
    function complain(e) {
        var code = e && e.code;
        if (code === "busy") return T.webVoiceBusy;
        if (code === "no_whisper") {
            return e.reason === "no_model" ? T.webVoiceNoModel : T.webVoiceNoBinary;
        }
        // `bad_request` from this route means the audio was not what the server would take, and
        // the audio was built here — so it is this page's fault and this page's sentence, not a
        // server message to pass along untranslated.
        if (code === "bad_request") return T.webVoiceFailed;
        return (e && e.message) || T.webVoiceFailed;
    }

    /// An error with words somebody can read and a code this file can branch on. The browser's
    /// own `DOMException` messages are English and are about codecs; neither is any use here.
    function fresh(said, code) {
        var e = new Error(said);
        e.code = code;
        return e;
    }

    /* ---- the recording ------------------------------------------------------ */

    function press() {
        if (state === "recording") { stop(); return; }
        // Mid-transcription the button is not the way out — the row's Cancel is, and it says so.
        if (state !== "off") return;
        if (whisperless) {
            var said = whisperless;
            whisperless = null;
            toast(said, true);
            return;
        }
        var no = why();
        if (no) { toast(no, true); return; }
        open();
    }

    /**
     * Ask for the microphone.
     *
     * Called straight out of the click, with nothing awaited in between: `getUserMedia` is only
     * allowed to prompt from inside a gesture, and a permission prompt that never appears is
     * indistinguishable from a button that does nothing.
     */
    function open() {
        var mine = ++token;
        state = "opening";
        show();
        navigator.mediaDevices.getUserMedia({ audio: true }).then(function (got) {
            // Cancelled, or a second press, while the prompt was on screen. The stream still
            // arrives and it still has to be handed back, or the recording light stays on.
            if (mine !== token) { silence(got); return; }
            begin(got, mine);
        }).catch(function (e) {
            if (mine !== token) return;
            state = "off";
            show();
            toast(refused(e), true);
        });
    }

    function begin(got, mine) {
        stream = got;
        chunks = [];
        try {
            recorder = new MediaRecorder(got);
            recorder.ondataavailable = function (ev) {
                if (ev.data && ev.data.size) chunks.push(ev.data);
            };
            recorder.onstop = function () { landed(mine); };
            // A recorder that falls over mid-sentence: whatever it managed to collect is still
            // worth reading, so this ends the same way the button does rather than throwing the
            // words away and complaining.
            recorder.onerror = function () { stop(); };
            recorder.start();
        } catch (e) {
            // A browser that has `MediaRecorder` and will not build one for this stream. Rare,
            // and reported rather than swallowed: the microphone was granted, so nothing else on
            // screen would explain why the count never started.
            release();
            state = "off";
            show();
            toast(T.webVoiceUnsupported, true);
            return;
        }
        since = Date.now();
        state = "recording";
        show();
        // Four times a second. The count is in whole seconds, and a one-second timer drawing a
        // one-second number lands visibly late about half the time.
        ticker = setInterval(function () { beat(mine); }, 250);
    }

    function beat(mine) {
        if (mine !== token) return;
        // **The composer has gone.** Opening an agent's transcript hides it — see
        // `renderAgentHead` — and a microphone left open behind a hidden row is one with no
        // button left to shut it. Nothing is lost that was not already only a moment old.
        if (els.composer.hidden) { cancel(); return; }
        if (state === "recording" && elapsed() >= MAX_SECONDS) {
            toast(fill(T.webVoiceLimit, { n: Math.round(MAX_SECONDS / 60) }));
            stop();
            return;
        }
        say();
    }

    function elapsed() { return since ? (Date.now() - since) / 1000 : 0; }

    /**
     * Stop listening and start reading.
     *
     * The count restarts here rather than carrying on, because it stops being the same number:
     * up to this point it was how long you have been talking, and after it, it is how long the
     * Mac has been thinking. One counter that meant both would mean neither.
     */
    function stop() {
        if (state !== "recording") return;
        state = "reading";
        since = Date.now();
        show();
        try {
            recorder.stop();
        } catch (e) {
            // Already stopped, or never really started. `onstop` will not fire now, so the next
            // step is taken by hand — and unhooked first, so it cannot also fire late.
            if (recorder) recorder.onstop = null;
            landed(token);
        }
    }

    /**
     * The recording, finished.
     *
     * The microphone is handed back first and before anything else can throw: on a phone the
     * indicator in the status bar is the reader's own evidence that this page has stopped
     * listening, and it should go out when the listening stops rather than when the request
     * comes back twelve seconds later.
     */
    function landed(mine) {
        var got = chunks;
        chunks = [];
        release();
        if (mine !== token) return;
        prepare(new Blob(got)).then(function (audio) {
            if (mine !== token) return null;
            return api.voice(audio, RATE);
        }).then(function (answer) {
            if (mine !== token || !answer) return;
            quit();
            // **An empty `text` is an answer, not a failure.** The Mac heard the recording and
            // there were no words in it, which happens to a pocket and to a room that went quiet
            // — and a red banner for that would be the page reporting a fault that did not occur.
            var said = String(answer.text || "").trim();
            if (!said) { toast(T.webVoiceEmpty); return; }
            appendMsg(said);
        }).catch(function (e) {
            if (mine !== token) return;
            quit();
            if (e && e.code === "no_whisper") whisperless = complain(e);
            toast(complain(e), !e || e.code !== "too_short");
        });
    }

    /// Back to nothing, with the microphone already handed back or never taken.
    function quit() {
        clearInterval(ticker);
        ticker = null;
        since = 0;
        state = "off";
        show();
    }

    /**
     * Changed your mind.
     *
     * Offered while it is reading as well as while it is listening, because that wait is the
     * long one: the first transcription after a reboot is twelve seconds, and somebody who has
     * decided to type it instead should not have to watch the rest of them go by. The request
     * carries on at the Mac and its answer is dropped when it lands — there is no way to recall
     * a transcription that is already running, and pretending otherwise would mean waiting for
     * it to confirm before the button appeared to work.
     */
    function cancel() {
        token += 1;
        if (recorder && state === "recording") {
            try { recorder.stop(); } catch (e) { /* it is going either way */ }
        }
        release();
        quit();
    }

    function release() {
        if (recorder) recorder.ondataavailable = recorder.onstop = recorder.onerror = null;
        recorder = null;
        silence(stream);
        stream = null;
    }

    function silence(got) {
        if (!got || !got.getTracks) return;
        got.getTracks().forEach(function (track) {
            try { track.stop(); } catch (e) { /* already gone */ }
        });
    }

    /* ---- turning a recording into something whisper.cpp can read ------------ */

    function prepare(blob) {
        if (!blob.size) return Promise.reject(fresh(T.webVoiceTooShort, "too_short"));
        return bytesOf(blob).then(decode).then(function (audio) {
            if (audio.duration < MIN_SECONDS) throw fresh(T.webVoiceTooShort, "too_short");
            return resample(audio);
        }).then(function (mono) {
            return base64(pcm16(mono));
        });
    }

    /// `Blob.arrayBuffer` is the modern spelling and `FileReader` is the one every browser that
    /// can record at all already had.
    function bytesOf(blob) {
        if (blob.arrayBuffer) return blob.arrayBuffer();
        return new Promise(function (done, fail) {
            var reader = new FileReader();
            reader.onload = function () { done(reader.result); };
            reader.onerror = function () { fail(fresh(T.webVoiceFailed, "unreadable")); };
            reader.readAsArrayBuffer(blob);
        });
    }

    /**
     * The bytes this browser just wrote, decoded by the same browser.
     *
     * Which is the entire trick: nobody has to know whether that was aac in an mp4 or opus in a
     * webm, because the only thing being asked is "what did you record", and it is being asked
     * of the recorder.
     *
     * Both spellings of `decodeAudioData` are wired. Safari answered it with callbacks for years
     * before it answered with a promise, and the callback form returns `undefined` — so a page
     * that only used the promise would hang on it forever with a counter running.
     */
    function decode(buffer) {
        var Ctx = window.AudioContext || window.webkitAudioContext;
        var ctx;
        try { ctx = new Ctx(); } catch (e) { return Promise.reject(fresh(T.webVoiceFailed, "no_audio")); }
        return new Promise(function (done, fail) {
            var maybe;
            try { maybe = ctx.decodeAudioData(buffer, done, fail); } catch (e) { fail(e); return; }
            if (maybe && typeof maybe.then === "function") maybe.then(done, fail);
        }).then(function (audio) {
            shut(ctx);
            return audio;
        }, function () {
            shut(ctx);
            throw fresh(T.webVoiceFailed, "undecodable");
        });
    }

    function shut(ctx) {
        try { if (ctx && ctx.close) ctx.close(); } catch (e) { /* it was closing anyway */ }
    }

    /**
     * Whatever rate the microphone ran at, at 16 kHz and in one channel.
     *
     * The graph does both jobs and neither is written out by hand: the destination of a
     * one-channel context downmixes a stereo source the way a browser downmixes anything, and
     * rendering into a 16 kHz context resamples with the filter that comes with it. The
     * alternative — reading every third sample — is four lines and produces speech with an
     * audible warble folded into it, and worse, one that still transcribes, slightly wrong.
     *
     * WebKit refused to build one of these below 44.1 kHz for years. That is old enough now that
     * the fallback would be dead code, so the throw is caught and *said* instead: a sentence
     * about a browser too old is the honest answer, and a decimated recording would not be.
     */
    function resample(audio) {
        var Off = window.OfflineAudioContext || window.webkitOfflineAudioContext;
        var frames = Math.max(1, Math.ceil(audio.duration * RATE));
        var off;
        try { off = new Off(1, frames, RATE); }
        catch (e) { return Promise.reject(fresh(T.webVoiceUnsupported, "no_resampler")); }
        var source = off.createBufferSource();
        source.buffer = audio;
        source.connect(off.destination);
        source.start(0);
        return new Promise(function (done, fail) {
            off.oncomplete = function (ev) { done(ev.renderedBuffer); };
            var maybe;
            try { maybe = off.startRendering(); } catch (e) { fail(e); return; }
            if (maybe && typeof maybe.then === "function") maybe.then(done, fail);
        }).then(function (out) { return out.getChannelData(0); },
                function () { throw fresh(T.webVoiceFailed, "no_render"); });
    }

    /// Float32 in [-1, 1] to little-endian Int16. **Clamped before it is scaled**: a sample can
    /// come back a hair outside the range after resampling, and 1.0001 × 32767 wraps around to a
    /// large negative number — which is a click in the audio at exactly the loudest moment.
    function pcm16(samples) {
        var out = new Uint8Array(samples.length * 2);
        var view = new DataView(out.buffer);
        for (var i = 0; i < samples.length; i++) {
            var s = samples[i];
            s = s < -1 ? -1 : (s > 1 ? 1 : s);
            view.setInt16(i * 2, Math.round(s * 32767), true);
        }
        return out;
    }

    /// Base64, a block at a time. `String.fromCharCode.apply(null, bytes)` puts every byte on
    /// the argument stack, and three minutes of this is 5.7 million of them — which is not slow,
    /// it is a `RangeError`. The blocks are joined before `btoa` rather than encoded separately,
    /// because base64 is only splittable on multiples of three and getting that subtly wrong
    /// produces a body the server decodes into noise rather than one it refuses.
    function base64(bytes) {
        var BLOCK = 0x8000;
        var parts = [];
        for (var i = 0; i < bytes.length; i += BLOCK) {
            parts.push(String.fromCharCode.apply(null, bytes.subarray(i, i + BLOCK)));
        }
        return btoa(parts.join(""));
    }

    /* ---- what it looks like ------------------------------------------------- */

    /**
     * The row, built once per state and then left alone.
     *
     * Rebuilding it on every tick would take the Cancel button out from under a thumb four
     * times a second, which is the same lesson `renderWaiting` learned about its own buttons.
     * So only the count is written after this, and it is written into a text node that is
     * already on screen.
     */
    function show() {
        var live = state === "recording";
        els.composer.dataset.voice = state;
        els.mic.setAttribute("aria-label", live ? T.webVoiceStop : T.webVoiceStart);
        els.mic.setAttribute("title", live ? T.webVoiceStop : T.webVoiceStart);
        els.mic.setAttribute("aria-pressed", live ? "true" : "false");

        var box = els.voice;
        // `opening` is the browser's own permission sheet, which is on top of the page and says
        // more than this row could. A row that appears for the length of a prompt is a flicker.
        if (state === "off" || state === "opening") {
            if (drawn) {
                drawn = "";
                setVoiceSpin(null);
                box.textContent = "";
                box.hidden = true;
                box.removeAttribute("data-near");
                box.removeAttribute("data-slow");
            }
            renderComposer();
            return;
        }
        if (drawn !== state) {
            drawn = state;
            box.textContent = "";
            box.removeAttribute("data-near");
            box.removeAttribute("data-slow");
            if (state === "reading") {
                // The same turning mark the session list draws, on the same clock — this page
                // has one way of saying "something is happening over there" and this is it.
                var spin = document.createElement("canvas");
                spin.className = "spin";
                box.appendChild(spin);
                setVoiceSpin(spin);
                drawSpinner(spin, spinPhase);
            } else {
                setVoiceSpin(null);
                var dot = document.createElement("span");
                dot.className = "dot";
                box.appendChild(dot);
            }
            var what = document.createElement("span");
            what.className = "what";
            var said = document.createElement("span");
            said.className = "said";
            var note = document.createElement("span");
            note.className = "note";
            note.textContent = T.webVoiceSlow;
            what.appendChild(said);
            what.appendChild(note);
            box.appendChild(what);

            var out = document.createElement("button");
            out.type = "button";
            out.className = "drop";
            out.textContent = T.webCancel;
            // Same reason as Send and the attachment: pressing this must not close the keyboard
            // of somebody who was typing while they dictated.
            out.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
            out.addEventListener("click", cancel);
            box.appendChild(out);
            box.hidden = false;
        }
        say();
        renderComposer();
    }

    /// The count, and only the count.
    function say() {
        var box = els.voice;
        var said = box.querySelector(".said");
        if (!said) return;
        var n = Math.floor(elapsed());
        if (state === "reading") {
            said.textContent = fill(T.webVoiceReading, { n: n });
            // Twelve seconds of nothing has an explanation and it is worth giving. Anything
            // shorter does not, and a permanent line of it would be an apology for a wait that
            // usually is not one.
            if (n >= SLOW_SECONDS) box.setAttribute("data-slow", "1");
            return;
        }
        said.textContent = fill(T.webVoiceListening, { t: mmss(n) });
        // Running out of room, said before it happens rather than announced afterwards.
        if (MAX_SECONDS - n <= NEAR_SECONDS) box.setAttribute("data-near", "1");
    }

    function mmss(n) {
        var m = Math.floor(n / 60), s = n % 60;
        return m + ":" + (s < 10 ? "0" : "") + s;
    }

    return {
        /**
         * Whether the microphone is open.
         *
         * The one reason the button stays alive when the rest of the composer has gone dead —
         * a session closing underneath a recording must not leave it with nothing to stop it.
         */
        live: function () { return state === "recording"; },
        /// Part of a message that has not arrived yet, the way a picture still shrinking is.
        busy: function () { return state !== "off"; },
        press: press,
        cancel: cancel
    };
})();

els.mic.addEventListener("click", function () { Voice.press(); });
// Pressing it must not take the focus off the box somebody is typing in; the click still lands.
els.mic.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
