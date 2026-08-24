import { MOCK, phone } from "../core/env.js";
import { T } from "../core/i18n.js";
import { S } from "../core/state.js";
import { els } from "../core/dom.js";
import { toast } from "../core/util.js";
import { api } from "../net/api.js";
import { render } from "../view/list.js";

/* ==========================================================================
   5. The door
   Everything below this line assumes the server is answering. This is what is
   on screen when it is not answering *this browser* — a pairing code that is
   shown on the Mac and typed in here, in that direction and never the other.
   ========================================================================== */

export var Door = {

    pairing: null,      // { id, expires } while a code is live on the Mac's screen
    wrong: 0,           // counted here as well as there: the server kills a pairing at five
    ticker: null,

    show: function () {
        if (!els.door.hidden) return;
        els.door.hidden = false;
        S.locked = true;
        if (!els["door-name"].value) els["door-name"].value = suggestName();
        if (!els["door-pw-name"].value) els["door-pw-name"].value = suggestName();
        this.step("ask");
        // Not on a phone: a keyboard springing up over the explanation of what is about to
        // happen means the explanation is never read.
        if (!phone()) els["door-name"].focus();
    },

    hide: function () {
        if (els.door.hidden) return;
        els.door.hidden = true;
        S.locked = false;
        this.stopClock();
        this.say("");
        render();
    },

    step: function (name) {
        els.door.dataset.step = name;
        this.say("");
        if (name !== "code") this.stopClock();
    },

    /** What went wrong, or what is about to happen. The server writes real sentences; this
     *  shows them rather than translating them into a code the reader has to look up. */
    say: function (words, calm) {
        els["door-say"].textContent = words || "";
        els["door-say"].className = "say" + (calm ? " calm" : "");
    },

    ask: function () {
        var self = this;
        var name = els["door-name"].value.trim() || suggestName();
        els["door-ask"].disabled = true;
        this.say(T.webDoorAsking, true);
        api.pair(name).then(function (d) {
            self.pairing = { id: d.pairing_id, expires: d.expires };
            self.wrong = 0;
            self.clearDigits();
            self.step("code");
            self.startClock();
            // Only ever from the fixture: a real server does not put the code in the reply, which
            // is the one thing this whole screen exists to be true about.
            if (MOCK && d.mock_code) self.say("Fixture: the code is " + d.mock_code, true);
            if (!phone()) els["door-digits"].firstElementChild.focus();
        }).catch(function (e) {
            // The one refusal that needs a sentence of our own: three requests in ten minutes is
            // the limit because each one puts an alert on somebody's screen, and "rate_limited"
            // on its own reads like a fault rather than like a door working correctly.
            self.say(e.code === "rate_limited"
                ? e.message + " " + T.webDoorRateLimited
                : e.message || T.webDoorAskFailed);
        }).then(function () { els["door-ask"].disabled = false; });
    },

    confirm: function () {
        var self = this;
        if (!this.pairing) { this.step("ask"); return; }
        var code = this.code();
        if (code.length !== 6) { this.say(T.webDoorSixDigits); return; }
        els["door-confirm"].disabled = true;
        this.say(T.webDoorChecking, true);
        api.confirmPair(this.pairing.id, code).then(function () {
            self.signedIn();
        }).catch(function (e) {
            // Only a refusal counts. A code that never reached the Mac was not a wrong guess, and
            // five dropped connections should not tell somebody their pairing is finished when the
            // far end never touched its counter.
            if (e.code === "forbidden") self.wrong += 1;
            self.clearDigits();
            // Five wrong guesses and the pending pairing is gone at the far end. Counting here
            // too is what keeps the screen honest about it: the server's refusal for a dead
            // pairing and its refusal for a wrong digit are the same 403 with the same code, and
            // only the sentence differs — which is a fine thing to *show* and a poor thing to
            // branch on.
            if (self.wrong >= 5) {
                self.pairing = null;
                self.step("ask");
                self.say(T.webDoorFinished);
                return;
            }
            self.say(e.message || T.webDoorWrongCode);
            if (!phone()) els["door-digits"].firstElementChild.focus();
        }).then(function () { els["door-confirm"].disabled = false; });
    },

    password: function () {
        var self = this;
        var secret = els["door-password"].value;
        if (!secret) { this.say(T.webDoorNeedPassword); return; }
        // Return in either box asks for this, and so does the button; the box is in a form as
        // well now. One attempt at a time whichever of them it came from.
        if (els["door-pw-go"].disabled) return;
        els["door-pw-go"].disabled = true;
        this.say(T.webDoorChecking, true);
        api.password(secret, els["door-pw-name"].value.trim() || suggestName()).then(function () {
            els["door-password"].value = "";
            self.signedIn();
        }).catch(function (e) {
            self.say(e.message || T.webDoorWrongPassword);
        }).then(function () { els["door-pw-go"].disabled = false; });
    },

    /** The cookie is set; ask the server again from the top rather than assuming. */
    signedIn: function () {
        this.pairing = null;
        this.wrong = 0;
        this.hide();
        toast(T.webDoorPaired);
        api.start();
    },

    code: function () {
        var out = "";
        var boxes = els["door-digits"].children;
        for (var i = 0; i < boxes.length; i++) out += boxes[i].value;
        return out;
    },

    clearDigits: function () {
        var boxes = els["door-digits"].children;
        for (var i = 0; i < boxes.length; i++) {
            boxes[i].value = "";
            boxes[i].classList.remove("filled");
        }
    },

    startClock: function () {
        var self = this;
        this.stopClock();
        function tick() {
            if (!self.pairing) return;
            var left = Math.max(0, self.pairing.expires - Math.floor(Date.now() / 1000));
            if (left <= 0) {
                self.pairing = null;
                self.step("ask");
                self.say(T.webDoorExpired);
                return;
            }
            var m = Math.floor(left / 60), sec = left % 60;
            els["door-left"].textContent = m + ":" + (sec < 10 ? "0" : "") + sec;
        }
        tick();
        this.ticker = setInterval(tick, 1000);
    },

    stopClock: function () { clearInterval(this.ticker); this.ticker = null; }
};

/**
 * What to call this device, guessed from the browser so that nobody has to think of a name for
 * a thing they are holding. Editable, because the guess is often "Chrome on Mac" when what
 * matters is "the work laptop".
 *
 * **Not translated, deliberately.** What comes out of here is stored on the Mac and read back in
 * the paired-devices list in Settings — a list drawn in whatever language the *Mac* is set to.
 * A name suggested in the phone's language would arrive there as a word out of place in a list
 * of names, and it is a name: the reader can type whatever they like over it.
 */
function suggestName() {
    var ua = navigator.userAgent || "";
    if (/iPhone/.test(ua)) return "iPhone";
    if (/iPad/.test(ua)) return "iPad";
    if (/Android/.test(ua)) return /Mobile/.test(ua) ? "Android phone" : "Android tablet";
    var browser = /Edg\//.test(ua) ? "Edge"
        : /OPR\//.test(ua) ? "Opera"
        : /Chrome\//.test(ua) ? "Chrome"
        : /Firefox\//.test(ua) ? "Firefox"
        : /Safari\//.test(ua) ? "Safari" : "";
    var machine = /Macintosh|Mac OS X/.test(ua) ? "Mac"
        : /Windows/.test(ua) ? "Windows"
        : /Linux/.test(ua) ? "Linux" : "";
    if (browser && machine) return browser + " on " + machine;
    return browser || machine || "A browser";
}

/* ---- the six boxes -------------------------------------------------------
   A code read off another screen and typed with a thumb. Every ordinary thing
   somebody does to a field like this works: paste the whole code anywhere in
   it, type straight through without tabbing, backspace out of an empty box.
   -------------------------------------------------------------------------- */

(function digits() {
    var boxes = Array.prototype.slice.call(els["door-digits"].children);

    function fill(from, text) {
        var digits = text.replace(/\D/g, "").split("");
        for (var i = from; i < boxes.length && digits.length; i++) {
            boxes[i].value = digits.shift();
            boxes[i].classList.add("filled");
        }
        var next = Math.min(boxes.length - 1, from + text.replace(/\D/g, "").length);
        boxes[next].focus();
        // Six digits in and there is nothing else this screen is for.
        if (Door.code().length === 6) Door.confirm();
    }

    boxes.forEach(function (box, index) {
        box.addEventListener("input", function () {
            var typed = box.value;
            box.value = "";
            fill(index, typed);
        });
        box.addEventListener("keydown", function (ev) {
            if (ev.key === "Backspace" && !box.value && index > 0) {
                ev.preventDefault();
                boxes[index - 1].value = "";
                boxes[index - 1].classList.remove("filled");
                boxes[index - 1].focus();
            } else if (ev.key === "ArrowLeft" && index > 0) {
                ev.preventDefault(); boxes[index - 1].focus();
            } else if (ev.key === "ArrowRight" && index < boxes.length - 1) {
                ev.preventDefault(); boxes[index + 1].focus();
            } else if (ev.key === "Enter") {
                ev.preventDefault(); Door.confirm();
            }
        });
        box.addEventListener("paste", function (ev) {
            var text = (ev.clipboardData || window.clipboardData).getData("text") || "";
            if (!/\d/.test(text)) return;
            ev.preventDefault();
            fill(index, text);
        });
        box.addEventListener("focus", function () { box.select(); });
    });
})();

els["door-ask"].addEventListener("click", function () { Door.ask(); });
els["door-confirm"].addEventListener("click", function () { Door.confirm(); });
els["door-restart"].addEventListener("click", function () { Door.pairing = null; Door.step("ask"); });
els["door-to-password"].addEventListener("click", function () { Door.step("password"); });
els["door-to-pair"].addEventListener("click", function () { Door.step("ask"); });
els["door-pw-go"].addEventListener("click", function () { Door.password(); });
els["door-name"].addEventListener("keydown", function (ev) { if (ev.key === "Enter") Door.ask(); });
// The password box is in a form now — see the note beside it — so Return would otherwise reload
// the page and lose whatever was typed. It means the same thing it always did.
document.getElementById("door-pw-form").addEventListener("submit", function (ev) {
    ev.preventDefault();
    Door.password();
});
els["door-password"].addEventListener("keydown", function (ev) { if (ev.key === "Enter") Door.password(); });
els["door-pw-name"].addEventListener("keydown", function (ev) { if (ev.key === "Enter") Door.password(); });
