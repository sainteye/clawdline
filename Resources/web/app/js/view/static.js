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
    text(els["tx-focus-label"], T.webShowOnMac);
    text(els["image-lightbox-title"], T.webImagePreview);
    text(els["image-lightbox-close"], T.webImageClose);
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
    text(els["shell-title"], T.webShellTitle);
    text(els["shell-stop"], T.webShellStop);
    text(els["shell-close"], T.webShellClose);
    text(els["session-screen"], T.webScreenTitle);
    text(els["screen-title"], T.webScreenTitle);
    text(els["screen-close"], T.webClose);
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

    // The pages, and the menu that names them. `Sessions` is the word the transcript's back
    // button already uses for the same destination, so it is the same string.
    text(els["nav-sessions"], T.webBack);
    text(els["nav-settings"], T.webSettings);
    // The wordmark itself is not painted here. It opens the menu now rather than the settings
    // sheet, and `Menu` would be a new member on the `Copy` protocol whose Chinese half is not
    // this change's to write — so it is in the markup, in English, and says so there.
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

    // Saying what to start, instead of picking where. The header microphone names what pressing
    // it leads to, the way `start-go` names its own sheet a few lines up; the sheet's "say it
    // again" button is doing nothing but dictation once the sheet is already open, so it keeps
    // the composer's own words for that rather than a second phrase for the same act. Either
    // button's words while it is actually recording belong to `Voice.show()` — same reasoning as
    // `els.mic` above — and this only paints the state each one comes up in.
    attr(els["voice-go"], "title", T.webCommand);
    attr(els["voice-go"], "aria-label", T.webCommandLabel);
    attr(els["command-sheet"], "aria-label", T.webCommandLabel);
    text(els["command-title"], T.webCommand);
    attr(els["command-mic"], "aria-label", T.webVoiceStart);
    attr(els["command-mic"], "title", T.webVoiceStart);
    // The words this box is asking for — the same question the fallback placeholder already
    // asked, translated. `command.js` owns everything the sheet says once a person has acted on
    // it; this is the one line still true before that.
    els["command-text"].placeholder = T.webCommandHeard;
    attr(els["command-list"], "aria-label", T.webCommandWhere);
    attr(els["command-instructions"], "aria-label", T.webCommandFirst);
    text(els["command-cancel"], T.webCancel);
    text(els["command-go"], T.webCommandGo);

    // Making a schedule. `input/schedule.js` owns everything that changes while the sheet is
    // open; what is here never does — it is the same split as the command sheet two blocks up.
    attr(els["schedule-new"], "title", T.webScheduleNew);
    attr(els["schedule-new"], "aria-label", T.webScheduleNew);
    text(els["schedule-form-title"], T.webScheduleNew);
    text(els["schedule-form-say"], T.webScheduleNewSay);
    text(els["schedule-title-label"], T.webScheduleTitle);
    attr(els["schedule-title"], "aria-label", T.webScheduleTitle);
    text(els["schedule-at-label"], T.webScheduleAt);
    attr(els["schedule-at"], "aria-label", T.webScheduleAt);
    text(els["schedule-days-label"], T.webScheduleOn);
    attr(els["schedule-days"], "aria-label", T.webScheduleOn);
    text(els["schedule-where-label"], T.webScheduleWhere);
    text(els["schedule-with-label"], T.webScheduleWith);
    attr(els["schedule-places"], "aria-label", T.webScheduleWhere);
    text(els["schedule-first-label"], T.webScheduleFirst);
    attr(els["schedule-instructions"], "aria-label", T.webScheduleFirst);
    text(els["schedule-more-label"], T.webScheduleMore);
    text(els["schedule-close-label"], T.webScheduleWhenDone);
    attr(els["schedule-close"], "aria-label", T.webScheduleWhenDone);
    // The two number fields' words are on a `<span>` with no id of its own — the label element
    // dom.js registered wraps both that span and the input — so it is found from there rather
    // than added to the list in `dom.js`, which is one of the four files nobody here re-edits.
    text(els["schedule-catch-label"] && els["schedule-catch-label"].querySelector(".field-label"),
        T.webScheduleCatchUp);
    text(els["schedule-timeout-label"] && els["schedule-timeout-label"].querySelector(".field-label"),
        T.webScheduleTimeout);
    text(els["schedule-cancel"], T.webCancel);
    text(els["schedule-go"], T.webScheduleCreate);

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
