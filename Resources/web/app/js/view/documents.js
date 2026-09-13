import {
    documentShareURL, normalizeDocumentIdentity, normalizeDocumentLocator, shareDocument
} from "../net/document-links.js";
import { documentBodyHTML } from "./document-render.js";
import { failureSentence } from "../core/failure-text.js";

function words(language) {
    var zh = /^zh(?:-|$)/i.test(language || "");
    return zh ? {
        title: "文件", menu: "文件", back: "‹ 工作階段", loading: "正在讀取文件…",
        connecting: "正在等待加密連線…", empty: "這個工作階段沒有可讀的 Markdown 或文字文件。",
        project: "專案", task: "任務", share: "分享", copy: "複製連結", copied: "連結已複製。",
        shared: "已開啟分享選單。", list: "‹ 文件", bytes: "位元組"
    } : {
        title: "Documents", menu: "Documents", back: "‹ Sessions", loading: "Loading documents…",
        connecting: "Waiting for the encrypted connection…",
        empty: "This session has no readable Markdown or text documents.",
        project: "Project", task: "Task", share: "Share", copy: "Copy link", copied: "Link copied.",
        shared: "Share sheet opened.", list: "‹ Documents", bytes: "bytes"
    };
}

/** A refusal as its code first, then the sentence its code chooses (`core/failure-text.js`). */
function typedError(error) {
    var code = error && typeof error.code === "string" ? error.code : "document_read_failed";
    return code + ": " + failureSentence(Object.assign({}, error, { code: code }),
        "The document could not be read.");
}

/**
 * The document page's complete state machine. Its transport functions are thunks so a direct
 * fragment can arrive while `api` is still the cold placeholder, then use the connected client
 * when `transportChanged()` retries the same explicit locator.
 */
export function bindDocumentsPage(elements, services) {
    var doc = services.document;
    var w = words(services.language ? services.language() : "en");
    var identity = null;
    var locator = null;
    var answer = null;
    var displayTitle = "";
    var held = false;
    var active = false;
    var ticket = 0;
    var retryTimer = null;
    var shareURL = null;

    function paintWords() {
        w = words(services.language ? services.language() : "en");
        elements.title.textContent = w.title;
        elements.back.textContent = w.back;
        elements.listBack.textContent = w.list;
        elements.share.textContent = w.share;
        elements.copy.textContent = w.copy;
        if (elements.menu) elements.menu.textContent = w.menu;
    }

    function cancelRetry() {
        if (retryTimer !== null) (services.cancel || clearTimeout)(retryTimer);
        retryTimer = null;
    }

    function scheduleRetry() {
        cancelRetry();
        retryTimer = (services.schedule || setTimeout)(function () {
            retryTimer = null;
            if (!active || !held) return;
            if (locator) openDocument(locator); else loadList();
        }, 750);
    }

    function say(message, error) {
        elements.status.textContent = message || "";
        elements.status.dataset.state = error ? "error" : "status";
    }

    function show(viewer) {
        elements.listView.hidden = viewer;
        elements.viewer.hidden = !viewer;
        elements.listBack.hidden = !viewer;
        elements.back.hidden = viewer;
    }

    function clearDocument() {
        locator = null;
        answer = null;
        displayTitle = "";
        shareURL = null;
        elements.documentTitle.textContent = "";
        elements.meta.textContent = "";
        elements.body.textContent = "";
        elements.share.disabled = true;
        elements.copy.disabled = true;
        elements.share.title = "";
        elements.copy.title = "";
    }

    function reset(deactivate) {
        ticket += 1;
        cancelRetry();
        held = false;
        identity = null;
        clearDocument();
        elements.rows.replaceChildren();
        show(false);
        say("", false);
        if (deactivate) active = false;
    }

    function titleFor(value) {
        return value.scope === "task" && value.title ? value.title : value.path;
    }

    function locatorOnly(value) {
        var out = { machine: value.machine, session: value.session,
            scope: value.scope, path: value.path };
        if (value.scope === "task") out.task = value.task;
        return out;
    }

    function renderList(rows) {
        show(false);
        elements.rows.replaceChildren();
        rows.forEach(function (row) {
            var item = doc.createElement("li");
            var button = doc.createElement("button");
            var name = doc.createElement("strong");
            var meta = doc.createElement("span");
            button.type = "button";
            button.className = "document-row";
            name.textContent = row.path;
            meta.textContent = (row.scope === "task" ? w.task + " · " + (row.title || row.task)
                : w.project) + " · " + row.bytes.toLocaleString() + " " + w.bytes;
            button.append(name, meta);
            button.addEventListener("click", function () { openDocument(row); });
            item.appendChild(button);
            elements.rows.appendChild(item);
        });
        say(rows.length ? "" : w.empty, false);
    }

    function renderDocument(value) {
        answer = value;
        show(true);
        elements.documentTitle.textContent = displayTitle || titleFor(value);
        elements.meta.textContent = (value.scope === "task" ? w.task : w.project) + " · " +
            value.path + " · " + value.byte_count.toLocaleString() + " " + w.bytes;
        elements.body.innerHTML = documentBodyHTML(value);
        shareURL = null;
        var shareFailure = null;
        try {
            shareURL = documentShareURL(services.shareOrigin ? services.shareOrigin(value) : null,
                locatorOnly(value));
        } catch (error) { shareFailure = error; }
        elements.share.disabled = !shareURL;
        elements.copy.disabled = !shareURL;
        elements.share.title = shareFailure ? typedError(shareFailure) : "";
        elements.copy.title = shareFailure ? typedError(shareFailure) : "";
        say("", false);
    }

    function retryable(error) {
        return error && ["cloud_starting", "offline", "relay_closed", "relay_reconnecting"]
            .indexOf(error.code) >= 0;
    }

    function openDocument(value) {
        var previous = locator;
        try { locator = normalizeDocumentLocator(locatorOnly(value)); }
        catch (error) { clearDocument(); say(typedError(error), true); return Promise.resolve(false); }
        if (!previous || previous.machine !== locator.machine ||
            previous.session !== locator.session || previous.scope !== locator.scope ||
            previous.task !== locator.task || previous.path !== locator.path) {
            displayTitle = titleFor(value);
        }
        identity = { machine: locator.machine, session: locator.session };
        held = false;
        answer = null;
        show(true);
        elements.documentTitle.textContent = locator.path;
        elements.meta.textContent = "";
        elements.body.textContent = "";
        elements.share.disabled = true;
        elements.copy.disabled = true;
        say(w.loading, false);
        var mine = ++ticket;
        return Promise.resolve().then(function () { return services.read(locator); }).then(function (value) {
            if (mine !== ticket) return false;
            renderDocument(value);
            return true;
        }).catch(function (error) {
            if (mine !== ticket) return false;
            held = retryable(error);
            say(held ? w.connecting : typedError(error), !held);
            if (held) scheduleRetry();
            return false;
        });
    }

    function loadList() {
        if (!identity) return Promise.resolve(false);
        var mine = ++ticket;
        held = false;
        clearDocument();
        show(false);
        say(w.loading, false);
        return Promise.resolve().then(function () { return services.list(identity); }).then(function (value) {
            if (mine !== ticket) return false;
            renderList(value.documents);
            return true;
        }).catch(function (error) {
            if (mine !== ticket) return false;
            held = retryable(error);
            say(held ? w.connecting : typedError(error), !held);
            if (held) scheduleRetry();
            return false;
        });
    }

    function copy() {
        if (!answer) return Promise.resolve(false);
        return Promise.resolve().then(function () {
            var url = documentShareURL(services.shareOrigin ? services.shareOrigin(answer) : null,
                locatorOnly(answer));
            var nav = services.navigator();
            if (!nav || !nav.clipboard || typeof nav.clipboard.writeText !== "function") {
                var unavailable = new Error("This browser cannot copy the document link.");
                unavailable.code = "document_copy_unavailable";
                throw unavailable;
            }
            return nav.clipboard.writeText(url);
        }).then(function () {
            say(w.copied, false); return true;
        }).catch(function (error) { say(typedError(error), true); return false; });
    }

    paintWords();
    elements.back.addEventListener("click", function () { services.navigate("sessions"); });
    elements.listBack.addEventListener("click", function () {
        ticket += 1;
        cancelRetry();
        held = false;
        clearDocument();
        loadList();
    });
    elements.share.addEventListener("click", function () {
        if (!answer) return;
        Promise.resolve().then(function () {
            return shareDocument(locatorOnly(answer), titleFor(answer), {
                origin: services.shareOrigin ? services.shareOrigin(answer) : null,
                navigator: services.navigator()
            });
        }).then(function (result) {
            say(result === "copied" ? w.copied : w.shared, false);
        }).catch(function (error) { say(typedError(error), true); });
    });
    elements.copy.addEventListener("click", copy);

    return {
        enter: function () {
            active = true;
            paintWords();
            if (locator) return openDocument(locator);
            return loadList();
        },
        leave: function () { reset(true); },
        hide: function () { reset(true); },
        openSession: function (value) {
            paintWords();
            var nextIdentity;
            try { nextIdentity = normalizeDocumentIdentity(value); }
            catch (error) { return this.openSessionError(error); }
            reset(false);
            identity = nextIdentity;
            var already = active;
            services.navigate("documents");
            if (already) loadList();
            return true;
        },
        openSessionError: function (error) {
            paintWords();
            reset(false);
            services.navigate("documents", { hash: false });
            say(typedError(error), true);
            return false;
        },
        openDirect: function (value, error) {
            paintWords();
            if (error || !value) {
                reset(false);
                services.navigate("documents", { hash: false });
                say(typedError({ code: "malformed_document_locator" }).replace(
                    "The document could not be read.", "Invalid document link."), true);
                return false;
            }
            var nextLocator;
            try { nextLocator = normalizeDocumentLocator(value); }
            catch (caught) { return this.openDirect(null, caught); }
            reset(false);
            locator = nextLocator;
            displayTitle = locator.path;
            identity = { machine: locator.machine, session: locator.session };
            var already = active;
            services.navigate("documents", { hash: false });
            if (already) openDocument(locator);
            return true;
        },
        transportChanged: function () {
            if (!active || !held) return false;
            cancelRetry();
            if (locator) openDocument(locator); else loadList();
            return true;
        },
        paint: paintWords,
        state: function () { return { active: active, held: held, identity: identity, locator: locator,
            hasAnswer: !!answer, displayTitle: displayTitle }; }
    };
}
