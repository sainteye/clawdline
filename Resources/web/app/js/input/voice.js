import { T, fill } from "../core/i18n.js";
import { els } from "../core/dom.js";
import { reduced } from "../core/env.js";
import { toast } from "../core/util.js";
import { drawSpinner, drawWave, setVoiceSpin, spinPhase, WAVE } from "../core/pixels.js";
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
    /// Where the meter's floor and ceiling sit, in dBFS. A phone held at arm's length hears
    /// ordinary speech somewhere around -30, a room that has gone quiet around -55, and the
    /// numbers are picked so the first fills most of the mark and the second none of it.
    var QUIET_DB = -50;
    var LOUD_DB = -20;

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

    /* The meter. `heard` is an audio context of its own, held open only while the microphone is
       — it is not the one `decode` builds afterwards, and the note on `listen` says why they
       cannot be the same one. `levels` is the last second and a bit of loudness, one whole
       number of cells per column, and `peak` is the loudest frame since the last of them went in. */
    var heard = null;
    var ears = null;            // the AnalyserNode reading the stream that is being recorded
    var samples = null;         // its scratch buffer, allocated once rather than sixty times a second
    var bars = null;            // the canvas in the row, or null when nothing is drawing
    var frame = null;           // the outstanding `requestAnimationFrame`
    var levels = [];
    var peak = 0;
    var pushed = 0;

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
        // Before the row is drawn, and still in the turn the recording started in. See `listen`:
        // the whole reason this is here and not where the first frame wants it is the gesture.
        listen(got);
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
        deaf();
        silence(stream);
        stream = null;
    }

    function silence(got) {
        if (!got || !got.getTracks) return;
        got.getTracks().forEach(function (track) {
            try { track.stop(); } catch (e) { /* already gone */ }
        });
    }

    /* ---- what the microphone is hearing ------------------------------------- */

    /**
     * Listen to the stream that is being recorded, so the row can draw it.
     *
     * A mark that pulses on a clock says the page is doing something. It does not say the
     * microphone is hearing anything, and those are two different facts to somebody holding a
     * phone at arm's length in a room with other people talking — the first question of a
     * dictation that came back empty is always "was it even picking me up". So the level is read
     * off the stream itself, and a flat line means a quiet room rather than meaning broken.
     *
     * **The context is opened here, in the same turn the recording starts, rather than at the
     * first frame that wants it.** iOS gives a page that asks inside a gesture a context that is
     * already running and a page that asks afterwards one that is suspended — and a suspended
     * analyser reads a flat line, which is exactly the picture a microphone hearing nothing
     * would draw. The failure would look like an answer, which is the worst way for this to be
     * wrong. It is closed again the moment the recording ends either way: an audio session left
     * open is one iOS may route or duck something else for.
     *
     * This is not the context `decode` builds. That one is opened long after any gesture, has no
     * reason to be running, and closes itself when the bytes are decoded.
     *
     * Nothing is connected to the destination: an analyser is pulled by the graph on its own, and
     * a path to the speakers would be this page playing the room back into the room.
     */
    function listen(got) {
        // Reduced motion is honoured by never building any of this. There is then nothing to
        // animate, and no audio session held open for the sake of something nobody asked to see.
        if (reduced) return;
        var Ctx = window.AudioContext || window.webkitAudioContext;
        if (!Ctx) return;
        try {
            heard = new Ctx();
            // Belt and braces for the browser that hands one back suspended anyway. The promise
            // is not waited on: the meter starts a frame late and nothing else notices.
            if (heard.state === "suspended" && heard.resume) heard.resume();
            ears = heard.createAnalyser();
            ears.fftSize = 1024;
            heard.createMediaStreamSource(got).connect(ears);
            samples = new Uint8Array(ears.fftSize);
        } catch (e) {
            // All of this is decoration over a recording that is already running. A browser that
            // will not build the graph gets the still dot instead and loses not one word.
            deaf();
        }
    }

    /**
     * One frame.
     *
     * **Read every frame and drawn ten times a second, which is two rates on purpose.** The
     * analyser only ever holds the last twenty milliseconds or so, so a loop that looked once
     * per column would miss four fifths of what was said and flatten the syllables it did catch;
     * the loudest reading between columns is the one that gets kept. And a column per frame
     * would be a blur travelling sixty pixels a second, which is something to watch rather than
     * something to glance at.
     *
     * `requestAnimationFrame` rather than a timer because a page in the background stops being
     * given frames, and a meter still drawing to a canvas nobody can see is a phone warming in
     * a pocket for nothing.
     */
    function paint() {
        frame = requestAnimationFrame(paint);
        if (!ears || !bars) return;
        ears.getByteTimeDomainData(samples);
        var sum = 0;
        for (var i = 0; i < samples.length; i++) {
            // 128 is silence in the byte form; what is wanted is the distance from it.
            var d = (samples[i] - 128) / 128;
            sum += d * d;
        }
        var lit = cells(Math.sqrt(sum / samples.length));
        if (lit > peak) peak = lit;
        var now = Date.now();
        if (now - pushed < WAVE.step) return;
        pushed = now;
        levels.push(peak);
        levels.shift();
        peak = 0;
        drawWave(bars, levels);
    }

    /// A root-mean-square in [0, 1] as whole cells above the floor. **In decibels**, because the
    /// linear number spends its entire life in the bottom tenth — ordinary speech a phone's
    /// length away sits around 0.03 — and a mark drawn straight from it is a flat line with an
    /// occasional twitch in it, which is the picture this exists to avoid.
    function cells(rms) {
        var db = 20 * Math.log10(rms || 1e-6);
        var t = (db - QUIET_DB) / (LOUD_DB - QUIET_DB);
        if (t < 0) t = 0; else if (t > 1) t = 1;
        return Math.round(t * (WAVE.rows - 1));
    }

    /// Stop drawing, because the canvas is being thrown away by whatever is rebuilding the row.
    /// The microphone may still be open — this says nothing about that.
    function still() {
        if (frame !== null) cancelAnimationFrame(frame);
        frame = null;
        bars = null;
    }

    /// Stop drawing and give the audio session back. Called wherever the microphone is handed
    /// back, which is the moment this has any further reason to be open.
    function deaf() {
        still();
        ears = null;
        samples = null;
        levels = [];
        peak = 0;
        pushed = 0;
        var ctx = heard;
        heard = null;
        shut(ctx);
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
     * Rebuilding it on every tick would take the buttons out from under a thumb four times a
     * second, which is the same lesson `renderWaiting` learned about its own. So only the count
     * is written after this, into a text node that is already on screen — and the meter is a
     * canvas that is also already on screen, whose *contents* change while the element it is
     * drawn on does not move.
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
                still();
                box.textContent = "";
                box.hidden = true;
                box.removeAttribute("data-near");
                box.removeAttribute("data-slow");
            }
            renderComposer();
            return;
        }
        if (drawn !== state) {
            // Whatever the last state was drawing into, it is about to stop existing.
            still();
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
            } else if (ears) {
                // Decoration, and only decoration: the seconds beside it are the substance of
                // this row and are what a screen reader is given. `aria-hidden` rather than a
                // label, because a live region that announced a picture of loudness ten times a
                // second would talk over the one sentence in here worth hearing.
                setVoiceSpin(null);
                var wave = document.createElement("canvas");
                wave.className = "wave";
                wave.setAttribute("aria-hidden", "true");
                box.appendChild(wave);
                bars = wave;
                levels = [];
                while (levels.length < WAVE.cols) levels.push(0);
                peak = 0;
                pushed = 0;
                // Drawn once before the first frame, so the row opens on the flat line rather
                // than on an empty space that fills in a sixtieth of a second later.
                drawWave(wave, levels);
                frame = requestAnimationFrame(paint);
            } else {
                // No analyser: reduced motion, or a browser that would not build the graph. The
                // dot this row has always drawn, which under reduced motion the global rule
                // stops on its brightest frame.
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

            /**
             * The ways out, in the order this page puts every pair — the one that undoes on the
             * left, the one that goes on with it on the right, and the weight on the second.
             *
             * **Two of them while it is listening.** Ending a recording used to live only on the
             * microphone beside the box, which meant the row that says a recording is happening
             * was not the row you could end one from — and the two halves of the same decision,
             * "keep this" and "throw this away", sat at opposite ends of the composer. The
             * microphone still stops it; nothing was taken away. It simply stopped being the
             * only way, and this is the one place somebody is already looking.
             *
             * Transcribing keeps the single Cancel: there is no "finish" to offer for something
             * that is being done at the other end.
             */
            var acts = document.createElement("div");
            acts.className = "acts";
            acts.appendChild(way(T.webCancel, "drop", cancel));
            if (state === "recording") acts.appendChild(way(T.webVoiceStop, "go", stop));
            box.appendChild(acts);
            box.hidden = false;
        }
        say();
        renderComposer();
    }

    /// One of them. The `mousedown` is the one Send and the attachment carry for the same
    /// reason: pressing this must not close the keyboard of somebody who was typing while they
    /// dictated. No new words — "Stop and transcribe" is what the microphone already calls
    /// itself while it is recording, and one action said twice should be said the same way.
    function way(words, kind, go) {
        var b = document.createElement("button");
        b.type = "button";
        b.className = kind;
        b.textContent = words;
        b.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
        b.addEventListener("click", go);
        return b;
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
