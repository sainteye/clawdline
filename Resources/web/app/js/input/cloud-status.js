import { T } from "../core/i18n.js";
import { setFailureOpener } from "../core/failure-text.js";
import { api } from "../net/api.js";
import { cloudStatusView } from "../view/cloud-status.js";

/* ==========================================================================
   The Cloud status sheet (`docs/cloud-error-transparency.md` §4.3)

   Reached two ways, and **one press is the most either asks for**: a failure line anywhere on the
   page — which opens it at that failure's `ref` — or the "Cloud status" row in Settings, which
   replaces five presses on the version number. It is built here rather than in `index.html`
   because only the hosted console has anything to put in it: a page served by the Mac has no
   trail, no relay and no `cloud.status`, and draws neither the sheet nor the row.

   The Mac's snapshot is read when the sheet opens, never on a timer. A read that fails says so
   in the sheet with its own code, and the browser's half is drawn before it and stays drawn.
   ========================================================================== */

function cloudTransport() {
    return api && api.trail && typeof api.trail.snapshot === "function" ? api : null;
}

function element(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
}

export var CloudStatus = (function () {
    var overlay = null;
    var body = null;
    var focus = null;
    var macs = null;
    var reading = false;
    var readError = null;
    var generation = 0;
    var unwatch = null;
    var repair = null;

    function build() {
        if (overlay) return;
        overlay = element("div", "overlay cloud-status");
        overlay.id = "cloud-status";
        overlay.hidden = true;
        var sheet = element("div", "sheet cloud-status-sheet");
        sheet.setAttribute("role", "dialog");
        sheet.setAttribute("aria-modal", "true");
        sheet.setAttribute("aria-label", T.webCloudStatus);
        sheet.appendChild(element("h2", "", T.webCloudStatus));
        body = element("div", "cloud-status-body");
        sheet.appendChild(body);
        var close = element("button", "chip wide", T.webClose);
        close.type = "button";
        close.addEventListener("click", function () { CloudStatus.close(); });
        sheet.appendChild(close);
        overlay.appendChild(sheet);
        overlay.addEventListener("click", function (event) {
            if (event.target === overlay) CloudStatus.close();
        });
        document.addEventListener("keydown", function (event) {
            if (!overlay.hidden && event.key === "Escape") CloudStatus.close();
        });
        document.body.appendChild(overlay);
    }

    function line(parent, text, className) {
        if (!text) return;
        parent.appendChild(element("p", className || "say", text));
    }

    function draw() {
        var client = cloudTransport();
        if (!overlay || overlay.hidden || !client) return;
        var view = cloudStatusView({ trail: client.trail.snapshot(), macs: macs, reading: reading,
            readError: readError, focus: focus });
        body.textContent = "";

        var browser = element("div", "block");
        browser.appendChild(element("b", "", T.webCloudStatusBrowser));
        view.browser.forEach(function (text) { line(browser, text, "say mono"); });
        view.hints.forEach(function (text) { line(browser, text, "said"); });
        if (view.repair && repair) {
            var fix = element("button", "chip", T.webCloudStatusRepair);
            fix.type = "button";
            fix.addEventListener("click", function () { CloudStatus.close(); repair(); });
            browser.appendChild(fix);
        }
        body.appendChild(browser);

        var commands = element("div", "block");
        commands.appendChild(element("b", "", T.webCloudStatusCommands));
        line(commands, view.noCommands);
        var focused = null;
        view.commands.forEach(function (row) {
            var item = element("div", "cloud-status-command" + (row.focus ? " focus" : ""));
            item.setAttribute("data-ref", row.refText);
            item.appendChild(element("div", "mono", row.refText + (row.title ? " · " + row.title : "")));
            line(item, row.browserSteps, "say mono");
            line(item, row.macSteps, "say mono");
            line(item, row.refusal, "said mono");
            commands.appendChild(item);
            if (row.focus) focused = item;
        });
        body.appendChild(commands);

        view.macs.forEach(function (mac) {
            var block = element("div", "block");
            block.appendChild(element("b", "", mac.machine));
            mac.lines.forEach(function (text) { line(block, text, "say mono"); });
            line(block, mac.error, "said");
            body.appendChild(block);
        });
        line(body, view.reading, "say");
        line(body, view.readError, "said");
        if (focused && typeof focused.scrollIntoView === "function") {
            focused.scrollIntoView({ block: "center" });
        }
    }

    function read() {
        var client = cloudTransport();
        if (!client || typeof client.cloudStatus !== "function") return;
        var ticket = ++generation;
        reading = true;
        readError = null;
        draw();
        var settled;
        try { settled = Promise.resolve(client.cloudStatus()); }
        catch (error) { settled = Promise.reject(error); }
        settled.then(function (answer) {
            if (ticket !== generation) return;
            macs = answer && Array.isArray(answer.machines) ? answer.machines : [];
        }, function (error) {
            if (ticket !== generation) return;
            readError = error;
        }).then(function () {
            if (ticket !== generation) return;
            reading = false;
            draw();
        });
    }

    return {
        /** Whether this page has a Cloud status to show at all. */
        available: function () { return !!cloudTransport(); },

        open: function (at) {
            var client = cloudTransport();
            if (!client) return;
            build();
            focus = at && at.ref ? at.ref : null;
            macs = null;
            overlay.hidden = false;
            if (unwatch) unwatch();
            unwatch = typeof client.trail.onChange === "function"
                ? client.trail.onChange(function () { draw(); }) : null;
            read();
            draw();
        },

        close: function () {
            if (!overlay) return;
            overlay.hidden = true;
            generation += 1;
            if (unwatch) { unwatch(); unwatch = null; }
        },

        /** The existing pairing repair entry, handed in by the page that owns the Cloud door. */
        onRepair: function (fn) { repair = typeof fn === "function" ? fn : null; },

        /** Failure lines everywhere on the page open this sheet from now on. */
        bindFailureLines: function () {
            setFailureOpener(function (at) { CloudStatus.open(at); });
        },

        /**
         * The Settings row, drawn on every arrival at Settings and only in Cloud mode. Added to the
         * sheet here rather than written into `index.html`, for the reason at the top of this file.
         */
        drawSettingsRow: function (sheet) {
            if (!sheet) return;
            var row = sheet.querySelector ? sheet.querySelector("#settings-cloud-status") : null;
            if (!cloudTransport()) {
                if (row) row.hidden = true;
                return;
            }
            if (!row) {
                row = element("div", "block");
                row.id = "settings-cloud-status";
                row.appendChild(element("b", "", T.webCloudStatus));
                row.appendChild(element("p", "say", T.webCloudStatusSay));
                var go = element("button", "chip", T.webCloudStatusOpen);
                go.type = "button";
                go.addEventListener("click", function () { CloudStatus.open(null); });
                row.appendChild(go);
                var foot = sheet.querySelector ? sheet.querySelector(".foot") : null;
                if (foot && foot.parentNode === sheet) sheet.insertBefore(row, foot);
                else sheet.appendChild(row);
            }
            row.hidden = false;
        }
    };
})();
