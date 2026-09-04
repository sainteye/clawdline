/* ==========================================================================
   Which page

   Until now this document was one page with things laid over it: the session
   list underneath, and Usage and Settings arriving on top of it — one as a
   fixed panel that hid `#app` by hand, the other as a modal sheet. Neither of
   them could be linked to, neither of them was anywhere in particular, and
   every new destination had to invent its own way of appearing and its own way
   of putting the page back.

   So there is a place to *be* now, and this is it. A page is a section of the
   document that is on screen when it is the current one and `hidden` when it is
   not, which is exactly the mechanism both of those already used — `keys.js`
   still asks `els.settings.hidden` and gets the same answer it always did. What
   is new is that one thing decides, the answer is written down where anybody can
   read it (`data-page` on the root element), and it is in the address, so a page
   can be linked to and the back button steps between them.

   **Nothing here touches the document until `bind` is called.** Several of the
   Node suites import modules that reach this one, and a module that reads
   `document` while it is being evaluated is a module that cannot be imported
   without a browser. `go` before `bind` is a no-op rather than a throw, for the
   same reason.

   Adding a page — the Projects page is the next one — is three things and no
   new mechanism: a `<section class="page" data-page-view="…">` in the document,
   an entry in the array `main.js` hands to `bind`, and a `data-page-to="…"`
   control somewhere to reach it. See `docs/web-pages.md`.
   ========================================================================== */

/** The page a fragment names, or null when it names none. */
export function pageInHash(hash) {
    var found = /(?:^|[#&])page=([^&]*)/.exec(String(hash || ""));
    if (!found || !found[1]) return null;
    try { return decodeURIComponent(found[1]); } catch (e) { return found[1]; }
}

/** The fragment that names one. The whole fragment: a page is not a session. */
export function hashForPage(name) { return "#page=" + encodeURIComponent(name); }

export var Pages = (function () {
    var doc = null;
    var root = null;
    var order = [];
    var byName = {};
    var homeName = "";
    var current = "";
    var announce = null;
    var writeHash = null;
    var fallback = "";

    /** The one control that took us here, so leaving can hand focus back to it. */
    function focusTarget(page) {
        if (!page || !page.focus || !doc) return null;
        return doc.getElementById(page.focus);
    }

    /* Where the keyboard goes when the page being arrived at names nothing — which the session
       list does, because it is a list and not a form with a first field.

       Leaving is the half that was missing. `hidden` takes the control focus is on out of the
       document, and a browser answers that by putting focus on `<body>`: measured as `active=BODY`
       after "Back to sessions", after Escape on Usage, and after Close on Settings. The Usage
       panel used to hand focus back to the wordmark from its own `close()`, and nothing did once
       that close became `Pages`' business. The wordmark is the one control on screen whatever page
       this is, and it is what opens the way to the others.

       Only on a real move. The first paint arrives from nowhere, and taking the keyboard on load
       is a page announcing itself to somebody who has not asked it anything. */
    function fallbackTarget(from) {
        if (!from || !fallback || !doc) return null;
        return doc.getElementById(fallback);
    }

    function show(page, on) {
        if (page.element) page.element.hidden = !on;
    }

    /* Every control that names a page — the sidebar's rows, "Back to sessions"
       at the top of Usage, Settings' own Close — is answered here rather than by
       each of them binding a listener of its own. That is what makes the next
       page's link a line of markup instead of a line of JavaScript, and it is
       why the attribute is `data-page-to` rather than `data-page`: the root
       element carries `data-page` as the answer to *which page is this*, and a
       click that walked up to it would otherwise read as a request to navigate. */
    function delegate(ev) {
        var node = ev.target;
        while (node && node !== doc) {
            var wanted = node.getAttribute && node.getAttribute("data-page-to");
            if (wanted) {
                if (ev.preventDefault) ev.preventDefault();
                Pages.go(wanted);
                return;
            }
            node = node.parentNode;
        }
    }

    var Pages = {
        /**
         * @param {object} config
         *   `document`  — the document these pages live in.
         *   `root`      — the element that wears `data-page`; the document element.
         *   `pages`     — `[{name, element, enter, leave, focus}]`, home first.
         *   `onChange`  — told after every move, so the shell can close the menu.
         *   `writeHash` — how a deliberate navigation reaches the address bar.
         *   `focusFallback` — the id the keyboard lands on when a page names no control of
         *                     its own; only ever on a move away from another page.
         */
        bind: function (config) {
            doc = config.document;
            root = config.root || null;
            order = (config.pages || []).slice();
            byName = {};
            for (var i = 0; i < order.length; i++) byName[order[i].name] = order[i];
            homeName = order.length ? order[0].name : "";
            announce = typeof config.onChange === "function" ? config.onChange : null;
            writeHash = typeof config.writeHash === "function" ? config.writeHash : null;
            fallback = config.focusFallback || "";
            current = "";
            if (doc && doc.addEventListener) doc.addEventListener("click", delegate);
            // The document comes up with the home page's markup already visible and
            // the rest of it `hidden`, and this says so rather than trusting it: a
            // page left visible in the markup is a page over the top of the list.
            this.go(homeName, { hash: false });
            return this;
        },

        /** Which page is on screen. `""` before `bind`. */
        current: function () { return current; },

        /** The page a bare URL means, and the one every "close" goes back to. */
        home: function () { return homeName; },

        /** Is this a page at all? Asked of a fragment before it is believed. */
        knows: function (name) { return Object.prototype.hasOwnProperty.call(byName, name); },

        /**
         * Go there. Idempotent: asking for the page already on screen does
         * nothing at all, which is what keeps a second tap on the sidebar's
         * Usage row from re-fetching the portfolio underneath somebody.
         *
         * `options.hash === false` for the two moves that must not write to the
         * address: the first paint, and a move the address asked for.
         */
        go: function (name, options) {
            if (!doc || !this.knows(name) || name === current) return false;
            var to = byName[name];
            var from = current ? byName[current] : null;
            current = name;
            for (var i = 0; i < order.length; i++) show(order[i], order[i] === to);
            if (from && typeof from.leave === "function") from.leave();
            if (root && root.setAttribute) root.setAttribute("data-page", name);
            if (typeof to.enter === "function") to.enter();
            var landing = focusTarget(to) || fallbackTarget(from);
            // `preventScroll`, because the page has just been shown and the browser
            // would otherwise scroll it to wherever the focused control happens to
            // be — a page that arrives already scrolled past its own heading.
            if (landing && landing.focus) landing.focus({ preventScroll: true });
            if (writeHash && (!options || options.hash !== false)) writeHash(hashForPage(name));
            if (announce) announce(name, from ? from.name : "");
            return true;
        },

        /**
         * Back to the list. What every Close and every Escape on a page means — and what an
         * address that has stopped naming a page means, which is why it takes the same options
         * every other move does: that one must not write the address it was just read from.
         */
        goHome: function (options) { return this.go(homeName, options); }
    };

    return Pages;
})();
