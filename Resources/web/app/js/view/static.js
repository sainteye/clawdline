import { esc } from "../core/esc.js";
import { T, fill, words } from "../core/i18n.js";
import { els } from "../core/dom.js";
import { WHO } from "./transcript.js";

/* ---- the words that live in the markup ----------------------------------- */

/**
 * Everything written into the HTML above, written again in the language that came back.
 *
 * The English is left in the document rather than blanked out, because a page whose strings
 * never arrived has to still be a page somebody can use — so the markup is the fallback and this
 * is the correction, and it runs once, before the first render, rather than on every draw.
 * Nothing below here is redrawn later: none of it changes while the page is open.
 */
export function paintStatic() {
    function text(el, s) { if (el) el.textContent = s; }
    function attr(el, name, s) { if (el) el.setAttribute(name, s); }

    els.filter.placeholder = T.webFilterPlaceholder;
    attr(els.filter, "aria-label", T.webFilterLabel);
    attr(els.rows, "aria-label", T.webListLabel);
    text(els["ptr-label"], T.webPull);

    // The chevron is drawn, not typed: it points the way back whichever language is beside it.
    text(els.back, "‹ " + T.webBack);
    attr(els.back, "aria-label", T.webBackLabel);
    text(els["tx-refresh-label"], T.webGitRefresh);
    attr(els["tx-refresh"], "title", T.webGitRefresh);
    text(els["tx-focus-label"], T.webShowOnMac);
    attr(els["session-actions"], "aria-label", T.webSessionActions);
    text(els["session-focus"], T.webShowOnMac);
    text(els["session-info"], T.webSessionInfo);
    text(els["session-actions-back"], "‹ " + T.webSessionActions);
    text(els["info-title"], T.webInfoTitle);
    attr(els["info-sheet"], "aria-label", T.webInfoTitle);
    text(els["info-refresh"], T.webInfoRefresh);
    text(els["info-close"], T.webClose);
    text(els["session-git"], T.webSessionGit);
    text(els["session-end"], T.webEndSession);
    text(els["git-title"], T.webGitTitle);
    text(els["git-refresh"], T.webGitRefresh);
    text(els["git-close"], T.webGitClose);
    text(els["action-confirm-cancel"], T.webCancel);
    text(els["action-confirm-go"], T.webConfirm);

    text(els["stale-say"], T.webStale);
    text(els["stale-go"], T.webStaleGo);
    if (els["stale-shut"]) els["stale-shut"].setAttribute("aria-label", T.webClose);

    attr(els.attach, "aria-label", T.webAttach);
    attr(els.attach, "title", T.webAttach);
    // The microphone at rest. What it says while it is recording belongs to `Voice.show()`,
    // for the same reason the send button's word belongs to `renderComposer`: one owner for a
    // label that changes, and this one only ever paints the state the page comes up in.
    attr(els.mic, "aria-label", T.webVoiceStart);
    attr(els.mic, "title", T.webVoiceStart);
    els.msg.setAttribute("data-placeholder", T.placeholder);
    attr(els.msg, "aria-label", T.placeholder);
    // The send button's words and its hover text belong to `renderComposer`: both of them change
    // while a message is in flight, and one owner for a thing that moves.

    // The wordmark, and the sheet behind it.
    attr(els.brand, "aria-label", T.webSettings);
    attr(els.brand, "title", T.webSettings);
    attr(els["settings-sheet"], "aria-label", T.webSettings);
    text(els["settings-title"], T.webSettings);
    text(els["settings-notify-title"], T.webSettingsNotify);
    text(els["settings-assistant-icons-title"], T.webSettingsAssistantIcons);
    els["settings-assistant-icons-say"].innerHTML = words(T.webSettingsAssistantIconsSay);
    text(els["settings-assistant-icons-label"], T.webSettingsAssistantIconsShow);
    text(els["settings-order-title"], T.webSettingsOrder);
    els["settings-order-say"].innerHTML = words(T.webSettingsOrderSay);
    attr(els["settings-order"], "title", T.webOrderTip);
    text(els["settings-close"], T.webClose);

    // Starting one. The button carries the short words as its hover text and the long ones as
    // its name, because what a screen reader reads out is the only label a drawn `+` has.
    attr(els["start-go"], "title", T.webStart);
    attr(els["start-go"], "aria-label", T.webStartLabel);
    attr(els["start-sheet"], "aria-label", T.webStartLabel);
    text(els["start-title"], T.webStart);
    els["start-filter"].placeholder = T.webStartFilter;
    attr(els["start-filter"], "aria-label", T.webStartFilter);
    text(els["start-close"], T.webClose);
    // The × is a shape, not a word; the word is what it is called.
    attr(els["starting-close"], "aria-label", T.webClose);

    // The transcript's left margin. Claude's own name is not in the strings and is not
    // translated — it is a name, and it is the same name in fourteen languages.
    WHO.user = T.webWhoYou;
    WHO.tool = T.webWhoTool;

    // The shortcuts card. The keys themselves stay as they are — a symbol printed on a keyboard
    // is not copy — and only the sentences beside them come from the server. `esc` is on the key
    // as well, so it is left with the rest of them.
    var sheet = els.keys.querySelector(".sheet");
    attr(sheet, "aria-label", T.webKeysLabel);
    text(sheet.querySelector("h2"), T.webKeysTitle);
    var rows = [
        [["↑", "↓"], T.webKeysMove], [["⏎"], T.webKeysOpen], [["/"], T.webKeysFilter],
        [["esc"], T.webKeysEscape], [["⌘K"], T.webKeysList], [["⌘J"], T.webKeysPane],
        [["⌘I"], T.webSessionInfo],
        [["g", "G"], T.webKeysEnds], [["r"], T.webKeysReverse], [["?"], T.webKeysThis]
    ];
    sheet.querySelector("dl").innerHTML = rows.map(function (row) {
        return "<dt>" + row[0].map(function (key) { return "<kbd>" + esc(key) + "</kbd>"; }).join(" ") +
            "</dt><dd>" + words(row[1]) + "</dd>";
    }).join("");
    text(sheet.querySelector(".foot"), T.webKeysFoot);

    // The door, all three steps of it — including the two nobody sees until they need them.
    var card = els.door.querySelector(".door-card");
    attr(card, "aria-label", T.webDoorLabel);

    var ask = card.querySelector('section[data-step="ask"]');
    text(ask.querySelector(".lede"), T.webDoorAskLede);
    ask.querySelector(".fine").innerHTML = words(T.webDoorAskFine);
    text(ask.querySelector("label"), T.webDoorName);
    text(els["door-ask"], T.webDoorAsk);
    text(els["door-to-password"], T.webDoorToPassword);

    var code = card.querySelector('section[data-step="code"]');
    text(code.querySelector(".lede"), T.webDoorCodeLede);
    // A clock in the middle of a sentence, rewritten every second. So the sentence is rebuilt
    // *around* the span rather than over it: two text nodes and the element the clock owns,
    // which keeps working wherever a translation puts the hole.
    var fine = code.querySelector(".fine");
    var halves = String(T.webDoorCodeFine).split("{left}");
    fine.textContent = "";
    fine.appendChild(document.createTextNode(halves[0]));
    fine.appendChild(els["door-left"]);
    fine.appendChild(document.createTextNode(halves.length > 1 ? halves[1] : ""));
    text(els["door-left"], T.webDoorTwoMinutes);
    var boxes = els["door-digits"].children;
    for (var i = 0; i < boxes.length; i++) {
        attr(boxes[i], "aria-label", fill(T.webDoorDigit, { n: i + 1 }));
    }
    text(els["door-confirm"], T.webDoorConfirm);
    text(els["door-restart"], T.webDoorRestart);

    var pw = card.querySelector('section[data-step="password"]');
    text(pw.querySelector(".lede"), T.webDoorPasswordLede);
    text(pw.querySelector(".fine"), T.webDoorPasswordFine);
    var labels = pw.querySelectorAll("label");
    text(labels[0], T.webDoorPassword);
    text(labels[1], T.webDoorName);
    text(els["door-pw-go"], T.webDoorPasswordGo);
    text(els["door-to-pair"], T.webDoorToPair);
}
