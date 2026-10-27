import { loadingSkeletonHTML } from "../../../loading-skeleton.js"
import {
    documentShareURL, normalizeDocumentIdentity, normalizeDocumentLocator, shareDocument, isVideoPath
} from "../net/document-links.js";
import { renderDocumentBody } from "./document-render.js";
import { failureSentence } from "../core/failure-text.js";

function words(language) {
    var zh = /^zh(?:-|$)/i.test(language || "");
    return zh ? {
        title: "文件", menu: "文件", back: "‹ 工作階段", loading: "正在讀取文件…",
        connecting: "正在等待加密連線…", empty: "這個工作階段沒有可讀的文件或 MP4 影片。",
        project: "專案", task: "任務", share: "分享", copied: "連結已複製。",
        shared: "已開啟分享選單。", list: "回到文件", bytes: "位元組",
        fullscreen: "全螢幕", exitFullscreen: "離開全螢幕", reveal: "顯示控制列",
        videoTooLarge: "影片超過 32 MiB，請匯出較小的 MP4。", videoChanged: "影片已變更，請重新開啟。", videoNotFound: "找不到這支已發布的影片。", videoReadFailed: "影片讀取失敗，請再讀一次。",
        videoLoading: "正在讀取影片… {percent}%", videoError: "這個瀏覽器無法播放此影片。請使用 H.264／AAC 編碼的 MP4。",
        retry: "再讀一次", others: "看其他文件", connectingDetail: "連上之後會自動打開，不用重新整理。"
    } : {
        title: "Documents", menu: "Documents", back: "‹ Sessions", loading: "Loading documents…",
        connecting: "Waiting for the encrypted connection…",
        empty: "This session has no readable documents or MP4 videos.",
        project: "Project", task: "Task", share: "Share", copied: "Link copied.",
        shared: "Share sheet opened.", list: "Back to documents", bytes: "bytes",
        fullscreen: "Full screen", exitFullscreen: "Exit full screen", reveal: "Show controls",
        videoTooLarge: "The video exceeds 32 MiB. Export a smaller MP4.", videoChanged: "The video changed. Open it again.", videoNotFound: "This published video could not be found.", videoReadFailed: "The video could not be read. Try again.",
        videoLoading: "Loading video… {percent}%", videoError: "This browser cannot play this video. Use an MP4 with H.264/AAC encoding.",
        retry: "Read again", others: "Other documents",
        connectingDetail: "It opens by itself once connected; there is no need to reload."
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
    var listRead = false;
    var viewing = "idle";
    var noticeTimer = null;
    var readAbort = null;
    var disposeBody = null;
    function releaseBody() {
        if (readAbort) readAbort.abort();
        readAbort = null;
        if (disposeBody) disposeBody();
        disposeBody = null;
        elements.body.textContent = "";
    }

    function displayError(error) {
        var key = { video_too_large: "videoTooLarge", video_changed: "videoChanged",
            video_not_found: "videoNotFound", video_format_unsupported: "videoError",
            video_range_invalid: "videoReadFailed", video_answer_invalid: "videoReadFailed" }[error && error.code];
        return key ? error.code + ": " + w[key] : typedError(error);
    }

    function paintWords() {
        w = words(services.language ? services.language() : "en");
        elements.title.textContent = w.title;
        elements.back.textContent = w.back;
        name(elements.listBack, w.list, true);
        name(elements.share, w.share, false);
        if (elements.fullscreen) name(elements.fullscreen, w.fullscreen, true);
        if (elements.reveal) name(elements.reveal, w.reveal, true);
        if (elements.retry) elements.retry.textContent = w.retry;
        if (elements.others) elements.others.textContent = w.others;
        if (elements.stateDetail) elements.stateDetail.textContent = w.connectingDetail;
        if (elements.menu) elements.menu.textContent = w.menu;
    }

    /**
     * Icon buttons are named for assistive technology; `title` is the pointer's
     * tooltip, except on Share, where it carries why sharing is unavailable.
     */
    function name(element, label, tooltip) {
        if (!element) return;
        element.setAttribute("aria-label", label);
        if (tooltip) element.title = label;
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
        if (noticeTimer !== null) (services.cancel || clearTimeout)(noticeTimer);
        noticeTimer = null;
        elements.status.textContent = message || "";
        elements.status.dataset.state = error ? "error" : "status";
    }

    /** A passing notice (link copied, share sheet opened) leaves by itself. */
    function notice(message) {
        say(message, false);
        noticeTimer = (services.schedule || setTimeout)(function () {
            noticeTimer = null;
            if (elements.status.dataset.state !== "error") say("", false);
        }, 4000);
    }

    /**
     * An open document is `loading`, `connecting`, `error` or `ready`. Until it
     * is ready the viewer shows a state card in the document's place, and the
     * status line moves into that card, so the one sentence that says what is
     * happening sits where the document will appear rather than above it.
     */
    function view(state) {
        viewing = state;
        var card = !elements.viewer.hidden && (state === "loading" || state === "connecting" || state === "error");
        elements.viewer.dataset.state = state;
        if (!elements.state) return;
        elements.state.hidden = !card;
        elements.body.hidden = card;
        var home = card ? elements.stateText : elements.listView.parentNode;
        if (elements.status.parentNode !== home) {
            if (card) home.appendChild(elements.status);
            else home.insertBefore(elements.status, elements.listView);
        }
    }

    function show(viewer) {
        elements.listView.hidden = viewer;
        elements.viewer.hidden = !viewer;
        elements.listBack.hidden = !viewer;
        elements.back.hidden = viewer;
        if (elements.page) elements.page.classList.toggle("is-reading", viewer);
        view(viewer ? viewing : "idle");
    }

    function ready(on) {
        elements.share.disabled = !on || !shareURL;
        if (elements.fullscreen) elements.fullscreen.disabled = !on;
    }

    function clearDocument() {
        locator = null;
        answer = null;
        displayTitle = "";
        shareURL = null;
        elements.documentTitle.textContent = "";
        elements.meta.textContent = "";
        releaseBody();
        ready(false);
        elements.share.title = "";
        view("idle");
    }

    function reset(deactivate) {
        ticket += 1;
        cancelRetry();
        held = false;
        identity = null;
        listRead = false;
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
        listRead = true;
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
        disposeBody = renderDocumentBody(elements.body, value, { videoError: w.videoError });
        shareURL = null;
        var shareFailure = null;
        try {
            shareURL = documentShareURL(services.shareOrigin ? services.shareOrigin(value) : null,
                locatorOnly(value));
        } catch (error) { shareFailure = error; }
        ready(true);
        elements.share.title = shareFailure ? displayError(shareFailure) : "";
        view("ready");
        say("", false);
    }

    function retryable(error) {
        return error && ["cloud_starting", "offline", "relay_closed", "relay_reconnecting"]
            .indexOf(error.code) >= 0;
    }

    function openDocument(value) {
        var previous = locator;
        try { locator = normalizeDocumentLocator(locatorOnly(value)); }
        catch (error) { clearDocument(); say(displayError(error), true); return Promise.resolve(false); }
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
        releaseBody();
        ready(false);
        view("loading");
        say(isVideoPath(locator.path) ? w.videoLoading.replace("{percent}", "0") : w.loading, false);
        readAbort = new AbortController();
        var signal = readAbort.signal;
        var mine = ++ticket;
        var wanted = locator;
        return Promise.resolve().then(function () { return services.read(wanted, { signal: signal, progress: function (percent) {
            if (mine === ticket) say(w.videoLoading.replace("{percent}", String(percent)), false);
        } }); }).then(function (value) {
            if (mine !== ticket) return false;
            renderDocument(value);
            return true;
        }).catch(function (error) {
            if (mine !== ticket) return false;
            held = !isVideoPath(locator.path) && retryable(error);
            view(held ? "connecting" : "error");
            say(held ? w.connecting : displayError(error), !held);
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
        if (!listRead) {
            elements.rows.innerHTML = '<li role="presentation">' + loadingSkeletonHTML(w.loading, "SK-13") + "</li>";
            say("", false);
        } else say(w.loading, false);
        return Promise.resolve().then(function () { return services.list(identity); }).then(function (value) {
            if (mine !== ticket) return false;
            renderList(value.documents);
            return true;
        }).catch(function (error) {
            if (mine !== ticket) return false;
            if (!listRead) elements.rows.replaceChildren();
            held = retryable(error);
            say(held ? w.connecting : displayError(error), !held);
            if (held) scheduleRetry();
            return false;
        });
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
            notice(result === "copied" ? w.copied : w.shared);
        }).catch(function (error) { say(displayError(error), true); });
    });
    if (elements.retry) elements.retry.addEventListener("click", function () {
        if (locator) openDocument(locator);
    });
    // The same way back as the heading's, so the address steps back with it.
    if (elements.others) elements.others.addEventListener("click", function () { elements.listBack.click(); });

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
            say(displayError(error), true);
            return false;
        },
        openDirect: function (value, error) {
            paintWords();
            if (error || !value) {
                reset(false);
                services.navigate("documents", { hash: false });
                say(displayError({ code: "malformed_document_locator" }).replace(
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
