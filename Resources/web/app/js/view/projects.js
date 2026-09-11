import { T, fill } from "../core/i18n.js";
import { bindWorktreeLifecycle } from "./worktrees.js";

/** The local places route has an authenticated implicit machine. Attach it at the UI adapter
    boundary so historical Session lookup uses the same strict (machine, conversation) locator
    as Cloud, without teaching a Cloud response with a missing identity to masquerade as local. */
export function localProjectPlaces(answer, machine) {
    if (!answer || !Array.isArray(answer.places) || typeof machine !== "string" || !machine)
        return answer;
    return { ...answer, places: answer.places.map(place => place && typeof place === "object"
        ? { ...place, machine } : place) };
}

/* ==========================================================================
   The Projects page

   Three questions, one page. The first is "where could I start a session" and
   it is answered by `/v1/places` — directories an assistant has actually been
   run in and that are still on the disk. The second is asked in front of one
   of them: **which of this Project's worktrees finished a Feature, and did it
   reach the branch.** That is
   `GET /v1/orchestrator/usage/project-worktrees`, and the reason it is not
   `git worktree list` is that this Mac carries 58 managed checkouts and the
   ledger remembers 150, most of which produced nothing anybody kept.

   The third is an explicit secondary action on a Board Project: **what is the
   lifecycle of the repository's actual worktrees now?** That answer comes from
   the bounded ProjectWorktreeLifecycle read model. It keeps active, dirty,
   landed, temporary and unknown evidence as separate facts. Refresh is an
   explicit observation; the browser has no cleanup route or credential.

   **The screen has one subject and it is `delivered`.** Eighteen of this
   repository's hundred and eighteen Feature-carrying worktrees finished their
   work and are still sitting on a branch out of which git has merged no
   delivery — sixteen it has not merged at all, and two that never received a
   commit to merge — measured on 2026-09-06, against the production ledger and
   the repository together. So that outcome is not one of six equal rows in a table: it is
   the block at the top, open, with its worktrees listed and the branch each
   one is on. The other five rungs are `<details>` underneath, closed, because
   they are the answer to "and the rest?".

   **That number replaced a much bigger one, and the difference is the point.**
   The same payload read the old way said fifty-three, because the rung asked
   whether anybody had filled `landing_state` in rather than whether the work
   was in the tree: of those fifty-three, twenty-four were on branches already
   contained by the checkout's HEAD and thirteen were on branches that no
   longer exist. So a worktree now carries `landingEvidence` beside its
   outcome and this page draws it — a verified record a root wrote and a
   branch this side merely found merged are both good answers to "did it
   land", and they are not the same answer. `git could not be asked` is a
   third answer and is drawn as itself, because a screen that cannot ask
   looks exactly like the screen this change was made to stop.

   **Two of those twenty-four had never received a commit**, and a branch with
   no commits is contained by HEAD from the moment `git worktree add -b` makes
   it. That is `branch_empty`, and it is not a landing; on this Mac the same
   reading found twelve such branches among seventy-five, ten of them with an
   uncommitted checkout still on disk. And the branches that are *gone* are
   their own group here rather than folded into `landed`: the app deletes a
   delivery branch only when it is empty, but the app is not the only thing
   that deletes branches, so an absence is this side losing sight of the work
   rather than proof the work arrived.

   **An empty answer and an answer that never arrived are drawn differently.**
   The route already refuses rather than returning a blank 200 — `404
   project_not_found`, `409 ambiguous_project` — and every answer it does give
   carries a `read` receipt saying how much was scanned. A page that showed the
   same grey "nothing here" for both would put that work back where it was, so
   the receipt is on screen whenever there is one and never when there is not.

   This module imports only the words and the DOM-injected lifecycle renderer, so
   `Tests/web-projects.mjs` can drive the whole of it against a stand-in document. The transport arrives
   through `bindProjectsPage`'s second argument, guarded by its caller: neither
   of these two reads exists on the Cloud path, so over that transport the page
   says so rather than drawing controls that cannot answer.
   ========================================================================== */

/**
 * What Escape means on this page, reachable from the one module that owns the Escape chain.
 *
 * **It is not a listener of this module's own, and that is the whole point.** The first version
 * was one, standing down while the drawer was open — and in a browser it fired anyway and took
 * the page with it, because `input/keys.js` closes the drawer and *returns*, and returning is
 * only ever true of the listener doing it. By the time a second listener on the same document
 * ran, the drawer it was checking for was already shut. No stand-in document catches that: a
 * harness with one listener has nothing to be second to.
 *
 * So the ordering lives where the ordering is decided. `input/keys.js` calls this after the
 * drawer and the shortcuts card have had their turn, exactly as it calls `Settings.close`.
 * `bindProjectsPage` fills it in; before that it is a no-op, so importing this module still
 * touches no document.
 */
export var Projects = { escape: function () { } };

/** The outcomes this page draws below the fold, hardest evidence first. */
var SECONDARY = ["landed", "nothing_to_land", "branch_gone", "active", "abandoned", "unknown"];

function number(value) {
    if (value === null || value === undefined) return "—";
    return new Intl.NumberFormat().format(value);
}

function clear(node) {
    while (node && node.firstChild) node.removeChild(node.firstChild);
}

function appendText(doc, parent, tag, value, className) {
    var node = doc.createElement(tag);
    if (className) node.className = className;
    node.textContent = value;
    parent.appendChild(node);
    return node;
}

/** The words for one rung of the ladder: its name, and what it rests on. */
function outcomeWords(outcome) {
    if (outcome === "landed") return { name: T.webProjectLanded, say: T.webProjectLandedSay };
    if (outcome === "nothing_to_land") {
        return { name: T.webProjectNothingToLand, say: T.webProjectNothingToLandSay };
    }
    if (outcome === "delivered") return { name: T.webProjectDelivered, say: T.webProjectDeliveredSay };
    if (outcome === "branch_gone") return { name: T.webProjectBranchGone, say: T.webProjectBranchGoneSay };
    if (outcome === "active") return { name: T.webProjectActive, say: T.webProjectActiveSay };
    if (outcome === "abandoned") return { name: T.webProjectAbandoned, say: T.webProjectAbandonedSay };
    return { name: T.webProjectUnknownOutcome, say: T.webProjectUnknownSay };
}

/**
 * A day, in the reader's own locale.
 *
 * The payload's timestamps are ISO 8601 in UTC. A date that cannot be parsed is left as the
 * string it arrived as rather than printed as `Invalid Date`: a value this page cannot read is
 * still a value somebody may want to see.
 */
function day(value) {
    if (!value) return "";
    var at = new Date(value);
    if (isNaN(at.getTime())) return String(value);
    return at.toLocaleDateString();
}

function seenText(worktree) {
    var first = day(worktree.firstSeenAt), last = day(worktree.lastSeenAt);
    if (first && last && first !== last) return first + " – " + last;
    return first || last || "—";
}

/**
 * The branch a worktree's delivery is on, **by convention and said so**.
 *
 * The payload deliberately carries no `branch`: the ledger stores none, and the registry that
 * does is swept, so a field present only for recent tasks would read as an old one's absence.
 * `docs/api.md` writes the convention down instead — `clawdline/task/<worktree id>` — and that
 * is what this shows, under a label that says which of the two it is. It is the one thing on
 * this screen somebody can act on, and leaving it off would make the whole block a lament.
 */
function branchOf(worktree) {
    return "clawdline/task/" + worktree.id;
}

/**
 * What the outcome beside it rests on, in words rather than in the wire's spelling.
 *
 * **The two that mean "landed" are deliberately not one word.** `record` is a root's landing
 * receipt, written by the broker only after it verified with a machine credential that the commit
 * is contained by the named target branch; `branch_merged` is this Mac reading `for-each-ref
 * --merged HEAD` and recognising a landing nobody wrote down. A reader deciding whether to go and
 * merge something needs to know which of the two they are looking at.
 *
 * **And `record` itself is two things.** The broker's landing sweep can close a record on its
 * write-set arm, which proves that nothing of the task's declared write set was outstanding at two
 * named instants — not that a commit reached the target. That row arrives as `record_unverified`
 * and says so, because a timer can now produce them by the dozen.
 *
 * **And the two that look like `branch_merged` and are not.** `branch_empty` is a branch HEAD
 * contains because it still points at the commit it was cut from, which is every delivery branch
 * before its first commit; `branch_base_unknown` is that same containment with nothing left on
 * record to say what it was cut from, so the two cannot be told apart. Both are drawn as
 * themselves under a `delivered` verdict, because "in HEAD" and "landed" turn out not to be the
 * same sentence.
 *
 * An unrecognised value falls through to `unknown` rather than being printed raw: a payload from
 * a newer app must not put a wire token on screen as if it were a sentence.
 */
function evidenceText(worktree) {
    var evidence = worktree.landingEvidence;
    if (evidence === "record") return T.webProjectEvidenceRecord;
    if (evidence === "record_unverified") return T.webProjectEvidenceRecordUnverified;
    if (evidence === "branch_merged") return T.webProjectEvidenceBranchMerged;
    if (evidence === "branch_empty") return T.webProjectEvidenceBranchEmpty;
    if (evidence === "branch_base_unknown") return T.webProjectEvidenceBranchBaseUnknown;
    if (evidence === "branch_absent") return T.webProjectEvidenceBranchAbsent;
    if (evidence === "branch_unmerged") return T.webProjectEvidenceBranchUnmerged;
    return T.webProjectEvidenceUnknown;
}

/** Every Feature this worktree finished, as one line. Ids are the fallback for a missing label. */
function featureText(worktree) {
    var features = worktree.features || [];
    var names = features.map(function (feature) { return feature.label || feature.id; });
    return names.length ? names.join(" · ") : "";
}

/**
 * **What this worktree was doing**, which is not the same question as which work line it
 * belonged to.
 *
 * Nine cards on this Mac read `Clawdfather — handoff 18bde7c3`, all of them, because that label
 * is the *work line* a classifier grouped by and the page had nothing else to put in the
 * heading. 「光看標題真的看不出來分別」. The route now carries `work` — the task's own stored
 * title, newest where a Feature covers several — so the heading is that, and the label it used to
 * be moves down to a fact under a word that says which of the two it is.
 *
 * Where there is no title the heading stays what it was rather than going blank: a task old
 * enough for the registry to have swept it has no title anywhere, and the label is then the most
 * this page knows.
 */
function workText(worktree) {
    var work = worktree.work;
    return typeof work === "string" && work ? work : "";
}

/** Which of the two things one row in the delivered block needs, in the reader's own words. */
function needsText(worktree) {
    if (worktree.needs === "nothing_to_land") return T.webProjectNeedsNothingToLand;
    if (worktree.needs === "land_or_abandon") return T.webProjectNeedsLanding;
    if (worktree.needs === "no_record") return T.webProjectNeedsNoRecord;
    return "";
}

function fact(doc, parent, label, value, className) {
    var row = doc.createElement("div");
    row.className = "project-fact" + (className ? " " + className : "");
    appendText(doc, row, "span", label, "project-fact-key");
    appendText(doc, row, "strong", value, "project-fact-value");
    parent.appendChild(row);
    return row;
}

function worktreeItem(context, worktree) {
    var doc = context.document;
    var item = doc.createElement("li");
    item.className = "project-worktree";
    item.dataset.outcome = worktree.outcome || "unknown";
    item.dataset.worktreeId = worktree.id;
    var features = featureText(worktree);
    var work = workText(worktree);
    var heading = work || features;
    if (heading) appendText(doc, item, "h3", heading, "project-worktree-features");
    var id = appendText(doc, item, "code", String(worktree.id).slice(0, 8), "project-worktree-id");
    id.title = String(worktree.id);
    var facts = doc.createElement("div");
    facts.className = "project-facts";
    /* Only when the heading is the work itself: printing the same sentence twice under a label
       that says it is something else is worse than not labelling it at all. */
    if (work && features) fact(doc, facts, T.webProjectWorkLine, features, "project-fact-line");
    fact(doc, facts, T.webProjectBranch, branchOf(worktree), "project-fact-branch");
    var evidence = fact(doc, facts, T.webProjectEvidence, evidenceText(worktree),
                        "project-fact-evidence");
    evidence.dataset.evidence = worktree.landingEvidence || "unknown";
    fact(doc, facts, T.webProjectRuns, number(worktree.runs));
    fact(doc, facts, T.webProjectSeen, seenText(worktree));
    /* The row's own next step, and only where the route says there is one. It is the means and
       not the outcome: nothing on this page closes a landing, because a landing record is durable
       and terminal and one closed on a guess is worse than a wrong count. */
    var needs = needsText(worktree);
    if (needs) fact(doc, facts, T.webProjectNeeds, needs, "project-fact-needs");
    item.appendChild(facts);
    return item;
}

/**
 * The hero: what finished and never landed.
 *
 * `total` is every worktree in the answer, and the all-clear sentence is drawn only when there is
 * something for it to be an all-clear *about*. Read in a browser on a Project with nothing in it,
 * "nothing is waiting" sat directly above "no worktree here has finished a Feature", which is two
 * sentences agreeing that there is nothing and one of them implying somebody had checked.
 */
function renderDelivered(context, worktrees, total) {
    var doc = context.document, elements = context.elements;
    var list = elements["project-delivered-list"];
    clear(list);
    var any = worktrees.length > 0;
    elements["project-delivered"].hidden = !any;
    elements["project-delivered-none"].hidden = any || !total;
    elements["project-delivered-none"].textContent = T.webProjectDeliveredNone;
    if (!any) return;
    elements["project-delivered-count"].textContent = number(worktrees.length);
    elements["project-delivered-title"].textContent = T.webProjectDelivered;
    elements["project-delivered-say"].textContent = T.webProjectDeliveredSay;
    worktrees.forEach(function (worktree) { list.appendChild(worktreeItem(context, worktree)); });
}

/**
 * The other four rungs, each closed.
 *
 * `<details>` rather than a fold this module opens and shuts itself: the browser already has a
 * disclosure that a keyboard reaches, a screen reader announces and a find-in-page opens, and
 * every line of state this page keeps about which section is open is a line that can disagree
 * with the document. A group with nothing in it is not drawn at all — an empty "Landed (0)"
 * reads as a reading, and the reading that matters is above.
 */
function renderGroups(context, byOutcome) {
    var doc = context.document, node = context.elements["project-groups"];
    clear(node);
    SECONDARY.forEach(function (outcome) {
        var worktrees = byOutcome[outcome] || [];
        if (!worktrees.length) return;
        var words = outcomeWords(outcome);
        var group = doc.createElement("details");
        group.className = "project-group";
        group.dataset.outcome = outcome;
        var summary = doc.createElement("summary");
        appendText(doc, summary, "span", words.name, "project-group-name");
        appendText(doc, summary, "span", number(worktrees.length), "project-group-count");
        group.appendChild(summary);
        appendText(doc, group, "p", words.say, "project-group-say");
        var list = doc.createElement("ol");
        list.className = "project-worktrees";
        worktrees.forEach(function (worktree) { list.appendChild(worktreeItem(context, worktree)); });
        group.appendChild(list);
        node.appendChild(group);
    });
}

/**
 * The receipt.
 *
 * This is the line that keeps an empty answer from looking like a failed one, so it is written
 * whenever the route answered and cleared whenever it did not — never left over from the last
 * Project somebody opened.
 */
function renderRead(context, read) {
    var elements = context.elements;
    read = read || {};
    elements["project-read"].textContent = fill(T.webProjectRead, {
        rows: number(read.rows), project: number(read.projectRows),
        worktree: number(read.worktreeRows), feature: number(read.featureRows)
    });
    var partial = read.truncated === true || context.state.status === "partial";
    elements["project-truncated"].hidden = !partial;
    elements["project-truncated"].textContent = T.webProjectTruncated;
}

function renderAnswer(context, answer) {
    var elements = context.elements, state = context.state;
    answer = answer || {};
    state.status = answer.status || "";
    var worktrees = answer.worktrees || [];
    var byOutcome = {};
    worktrees.forEach(function (worktree) {
        var outcome = worktree.outcome || "unknown";
        (byOutcome[outcome] || (byOutcome[outcome] = [])).push(worktree);
    });
    renderDelivered(context, byOutcome.delivered || [], worktrees.length);
    renderGroups(context, byOutcome);
    /* The one sentence that says the query ran and found nothing. It is separate from the
       receipt beside it on purpose: the receipt is the evidence, this is the reading. */
    elements["project-none"].hidden = worktrees.length > 0;
    elements["project-none"].textContent = T.webProjectNoWorktrees;
    var excluded = (answer.excluded || {}).worktreesWithoutFeature || 0;
    elements["project-excluded"].hidden = !excluded;
    elements["project-excluded"].textContent = fill(T.webProjectExcluded, { n: number(excluded) });
    /* Not this Project's, and not dropped either: rows written before canonical Project keys
       existed carry a checkout path as their own key and so belong under no Project at all. */
    var unattributed = (answer.unattributed || {}).worktrees || 0;
    elements["project-unattributed"].hidden = !unattributed;
    elements["project-unattributed-title"].textContent = T.webProjectUnattributed;
    elements["project-unattributed-say"].textContent =
        fill(T.webProjectUnattributedSay, { n: number(unattributed) });
    renderRead(context, answer.read);
    elements["project-status"].textContent = "";
}

/** Every drawn part of one Project's answer, taken off the screen in one place. */
function clearAnswer(context) {
    var elements = context.elements;
    clear(elements["project-delivered-list"]);
    clear(elements["project-groups"]);
    elements["project-delivered"].hidden = true;
    elements["project-delivered-none"].hidden = true;
    elements["project-none"].hidden = true;
    elements["project-excluded"].hidden = true;
    elements["project-unattributed"].hidden = true;
    elements["project-truncated"].hidden = true;
    // The receipt is emptied rather than hidden, because "nothing was read" is what its absence
    // means and an old Project's numbers under a new Project's name would be a lie with a source.
    elements["project-read"].textContent = "";
}

/**
 * What one refusal says.
 *
 * Every code here is one the route defines, and each gets its own sentence: the useful next move
 * after `project_not_found` (widen the range, check the name) is not the one after
 * `ambiguous_project` (there are two of these and you have to say which), and neither is the one
 * after `usage_analytics_busy` (nothing is wrong; ask again). Anything else falls back to the
 * message the Mac sent, which is already in the reader's language.
 */
function refusalText(error) {
    var code = error && error.code;
    if (code === "project_not_found") return T.webProjectNotFound;
    if (code === "ambiguous_project") return T.webProjectAmbiguous;
    if (code === "usage_analytics_busy") return T.webProjectBusy;
    return (error && error.message) || T.webProjectFailed;
}

function projectActivity(place, chinese) {
    var count = place.activeItemCount;
    if (!Number.isSafeInteger(count) || count < 0) return {
        text: chinese ? "進度尚未確認" : "Activity unknown", tone: "unknown"
    };
    var coverage = place.summaryCoverage;
    var complete = !place.activitySourcePartial
        && (coverage === "complete" || (coverage && coverage.status === "complete"));
    var current = place.activityReadStatus === "ready";
    var text = number(count) + (chinese ? " 項進行中" : " in progress");
    if (!current) return { text: (chinese ? "上次紀錄：" : "Last known: ") + text, tone: "unknown" };
    if (!complete) return { text: (chinese ? "部分紀錄：" : "Partial: ") + text, tone: "unknown" };
    return { text: count ? text : (chinese ? "目前無進行中項目" : "None in progress"),
        tone: count ? "active" : "idle" };
}

function renderPlaces(context, places) {
    var doc = context.document, elements = context.elements;
    var rows = elements["projects-rows"];
    clear(rows);
    elements["projects-count"].textContent = places.length ? number(places.length) : "";
    if (!places.length) {
        elements["projects-status"].textContent = T.webProjectsEmpty;
        return;
    }
    elements["projects-status"].textContent = "";
    places.forEach(function (place) {
        var item = doc.createElement("li");
        item.className = "project-row-wrap";
        var button = doc.createElement("button");
        button.type = "button";
        button.className = "project-row";
        button.dataset.placeId = place.id;
        var mark = doc.createElement("canvas");
        mark.setAttribute("aria-hidden", "true");
        mark.className = "project-row-mark" + (context.drawIcon(mark, place.icon, 4) ? "" : " none");
        button.appendChild(mark);
        var text = doc.createElement("span");
        text.className = "project-row-text";
        var heading = appendText(doc, text, "span", "", "project-row-heading");
        var name = appendText(doc, heading, "strong", place.label || place.path, "project-row-name");
        name.style.color = place.icon ? context.tint(place.icon.accent) : "";
        var activity = place.boardProjectId ? projectActivity(place,
            /^zh/i.test(doc.documentElement && doc.documentElement.lang || "")) : null;
        // Zero is visual silence, not removal of the Project or proof of complete coverage.
        // Keep its qualified accessible description below; unknown is never coerced to zero.
        if (activity && place.activeItemCount !== 0) appendText(doc, heading, "span", activity.text,
            "project-row-activity is-" + activity.tone);
        // The path is here for the one job `/v1/places` says it is for: telling two projects with
        // the same name apart. Nothing on this page is built out of it except the query below.
        appendText(doc, text, "span", place.path || "", "project-row-path");
        if (place.boardProjectId && Number.isInteger(place.itemCount) && place.itemCount >= 0) appendText(doc, text, "span", place.itemCount +
            (/^zh/i.test(doc.documentElement && doc.documentElement.lang || "") ? " 個工作項目 · 查看進度 →" : " work items · View progress →"), "project-row-path");
        button.appendChild(text);
        button.setAttribute("aria-label", fill(T.webProjectOpenLabel, { name: place.label || place.path })
            + (activity ? ", " + activity.text : ""));
        button.addEventListener("click", function () { context.open(place); });
        item.appendChild(button);
        if (place.boardProjectId && context.lifecycleAvailable()) {
            var worktrees = doc.createElement("button");
            worktrees.type = "button";
            worktrees.className = "project-row-worktrees";
            worktrees.textContent = /^zh/i.test(doc.documentElement && doc.documentElement.lang || "")
                ? "工作樹" : "Worktrees";
            worktrees.setAttribute("aria-label", (worktrees.textContent + ": "
                + (place.label || place.path)));
            worktrees.addEventListener("click", function () { context.openLifecycle(place); });
            item.appendChild(worktrees);
        }
        rows.appendChild(item);
    });
}

// Mode is a successful store fact, not an inference from a failed read. A broken Board must not
// remove a transport's independently authorized baseline Projects reader.
export async function readProjectPlaces(transport, onMode) {
    var baseline = typeof transport.places === "function"
        && typeof transport.projectWorktrees === "function";
    if (typeof transport.board === "function") {
        try {
            var answer = await transport.board();
            var board = answer && answer.board;
            if (!board || board.available === false || typeof board.enabled !== "boolean"
                || !Array.isArray(board.projects)) {
                var invalid = new Error(board && board.error && board.error.message || "Board unavailable");
                invalid.code = board && board.error && board.error.code || "board_unavailable";
                throw invalid;
            }
            if (onMode) onMode(board);
            if (board.enabled) return { places: board.projects.filter(function (project) {
                // Membership comes from the Mac's durable Start Point catalog, not historical
                // task existence. displayPath is the older server's same trusted exact join.
                return project.isStartPoint === true || (project.isStartPoint !== false
                    && typeof project.displayPath === "string" && project.displayPath.length > 0);
            }).map(function (project) {
                return { id: project.id, boardProjectId: project.id, label: project.label || project.name,
                    icon: project.icon, path: project.displayPath || "", itemCount: project.itemCount,
                    activeItemCount: project.activeItemCount, summaryCoverage: project.summaryCoverage,
                    activitySourcePartial: !!(board.source && (board.source.truncated
                        || (board.source.ingestion && board.source.ingestion.status !== "complete"))),
                    activityReadStatus: board.readState && board.readState.status };
            }), boardMode: true, readState: board.readState };
        } catch (error) {
            if (!baseline || !(/^(board_|http_503$)/.test(error.code || "") || error.status === 503)) throw error;
            var fallback = await transport.places();
            return Object.assign({}, fallback, { boardUnavailable: {
                code: error.code || "board_unavailable", message: error.message || "Board unavailable"
            } });
        }
    }
    if (!baseline) {
        var unavailable = new Error("Projects are unavailable on this connection in standard mode.");
        unavailable.code = "projects_unavailable";
        throw unavailable;
    }
    return transport.places();
}

export function bindProjectsPage(elements, environment) {
    environment = environment || {};
    var doc = environment.document || document;
    var state = { view: "list", places: null, place: null, status: "", loading: 0 };
    var active = false, catalogTimer = null;
    var schedule = environment.setTimeout || globalThis.setTimeout;
    var unschedule = environment.clearTimeout || globalThis.clearTimeout;
    function stopCatalogTimer() {
        if (catalogTimer !== null) unschedule(catalogTimer);
        catalogTimer = null;
    }
    function laterCatalog(delay) {
        stopCatalogTimer();
        if (!active || state.view !== "list") return;
        catalogTimer = schedule(function () {
            catalogTimer = null;
            if (!active || state.view !== "list") return;
            if (doc.hidden) laterCatalog(delay);
            else loadPlaces(true);
        }, delay);
        if (catalogTimer && catalogTimer.unref) catalogTimer.unref();
    }
    var context = {
        document: doc, elements: elements, state: state,
        drawIcon: environment.drawIcon || function () { return false; },
        tint: environment.tint || function () { return ""; },
        open: function (place) { openProject(place); },
        openLifecycle: function (place) { openLifecycle(place); },
        lifecycleAvailable: function () {
            return typeof environment.lifecycleAvailable === "function"
                ? environment.lifecycleAvailable() : false;
        }
    };
    /* The legacy places/delivery join and the lifecycle model are separate reads. The lifecycle
       read is available locally and through Cloud's closed machine vocabulary; neither exposes
       cleanup. Availability is asked when the row is drawn because Cloud boots in two phases. */
    var readPlaces = typeof environment.places === "function" ? environment.places : null;
    var readWorktrees = typeof environment.projectWorktrees === "function"
        ? environment.projectWorktrees : null;
    var lifecycle = bindWorktreeLifecycle({
        "project-worktree-lifecycle": elements["project-worktree-lifecycle"],
        "project-worktree-status": elements["project-worktree-status"],
        "project-worktree-summary": elements["project-worktree-summary"],
        "project-worktree-rows": elements["project-worktree-rows"],
        "project-worktree-refresh": elements["project-worktree-refresh"]
    }, {
        document: doc,
        read: environment.projectWorktreeLifecycle,
        refresh: environment.projectWorktreeLifecycleRefresh,
        openOwner: environment.openWorktreeOwner
    });
    /* **Whether this transport carries them is asked when the page is used, not when it is
       bound.** `net/api.js` holds a live binding that the entry point fills in, and on the Cloud
       path it fills it in twice — once with an idle client and again when the relay handshake
       finishes. A guard read at bind time would be reading the placeholder and drawing the page
       for a transport that had not been chosen yet. */
    var carries = typeof environment.carries === "function"
        ? environment.carries
        : function () { return !!(readPlaces && readWorktrees); };

    function showView(view) {
        state.view = view;
        elements["projects-list-view"].hidden = view !== "list";
        elements["projects-detail-view"].hidden = view !== "detail";
    }

    /**
     * The list.
     *
     * Asked afresh on every arrival, the way the new-session sheet asks: the Mac drops
     * directories that are no longer on the disk while it builds this answer, so the list is only
     * as true as the moment it was given.
     */
    function loadPlaces(background = false) {
        stopCatalogTimer();
        if (!background) {
            clear(elements["projects-rows"]);
            elements["projects-count"].textContent = "";
            state.places = null;
        }
        if (!readPlaces || !carries()) {
            clear(elements["projects-rows"]);
            elements["projects-count"].textContent = "";
            elements["projects-status"].textContent = T.webProjectsUnavailable;
            return Promise.resolve();
        }
        var ticket = ++state.loading;
        elements["projects-status"].textContent = T.webProjectsLoading;
        return readPlaces().then(function (data) {
            if (ticket !== state.loading) return;
            state.places = (data && data.places) || [];
            renderPlaces(context, state.places);
            var freshness = data && data.readState && data.readState.status;
            // Poll only the lightweight Board catalog while visible, never every item's detail.
            if (data && data.boardMode && freshness === "ready") laterCatalog(15000);
            if (freshness === "loading" || freshness === "stale" || freshness === "error") {
                var chinese = /^zh/i.test(doc.documentElement && doc.documentElement.lang || "");
                elements["projects-status"].textContent = freshness === "loading"
                    ? (chinese ? "正在背景準備專案目錄…" : "Preparing the project directory in the background…")
                    : freshness === "error" && !state.places.length
                    ? (chinese ? "專案目錄暫時無法取得，稍後自動重試。" : "The project directory is temporarily unavailable; retrying shortly.")
                    : (chinese ? "顯示上次可用的專案目錄，背景正在更新。" : "Showing the last available directory while it updates.");
                laterCatalog(freshness === "loading" ? 2000 : 15000);
            }
            if (data && data.boardUnavailable) {
                var zh = /^zh/i.test(doc.documentElement && doc.documentElement.lang || "");
                elements["projects-status"].textContent = (zh
                    ? "看板暫時無法讀取，目前顯示一般專案。設定未變更。"
                    : "Board unavailable; showing standard Projects. Your setting has not changed.")
                    + " (" + data.boardUnavailable.code + ")";
            }
        }).catch(function (error) {
            if (ticket !== state.loading) return;
            if (background && state.places && state.places.length
                && ![401, 403].includes(error.status)
                && !/unauthorized|forbidden|permission/.test(error.code || "")) {
                // The retained rows must lose their current activity claim together with the
                // directory warning; a green zero after a failed refresh is a false reassurance.
                state.places = state.places.map(function (place) {
                    return Object.assign({}, place, { activityReadStatus: "stale" });
                });
                renderPlaces(context, state.places);
                var chinese = /^zh/i.test(doc.documentElement && doc.documentElement.lang || "");
                elements["projects-status"].textContent = (chinese
                    ? "更新失敗，目前保留上次的專案目錄。"
                    : "Update failed; showing the last available project directory. ") + refusalText(error);
                laterCatalog(15000);
                return;
            }
            clear(elements["projects-rows"]);
            elements["projects-count"].textContent = "";
            elements["projects-status"].textContent = refusalText(error);
        });
    }

    /**
     * One Project.
     *
     * The Project is named by its path rather than by the opaque Portfolio id, because a place is
     * the only identity this page has and the route accepts all three spellings. Everything drawn
     * for the last Project comes off the screen before the request goes out: a receipt left over
     * from another Project is worse than no receipt at all.
     */
    function openProject(place) {
        lifecycle.leave();
        if (place.boardProjectId && environment.openBoard) {
            stopCatalogTimer();
            environment.openBoard(place);
            return Promise.resolve();
        }
        state.place = place;
        showView("detail");
        elements["project-name"].textContent = place.label || place.path;
        elements["project-path"].textContent = place.path || "";
        var mark = elements["project-mark"];
        mark.className = "project-mark" + (context.drawIcon(mark, place.icon, 5) ? "" : " none");
        clearAnswer(context);
        if (!readWorktrees || !carries()) {
            elements["project-status"].textContent = T.webProjectsUnavailable;
            return Promise.resolve();
        }
        var ticket = ++state.loading;
        elements["project-status"].textContent = T.webProjectReading;
        return readWorktrees(place).then(function (data) {
            if (ticket !== state.loading) return;
            renderAnswer(context, data && data.projectWorktrees);
        }).catch(function (error) {
            if (ticket !== state.loading) return;
            // Nothing is taken off the screen here, because `clearAnswer` above already did it
            // before the request went out — and that is the whole of the difference between
            // "read 726 rows and found none" and "this was never answered". A second clear on
            // this path would be a line no test could ever turn red.
            elements["project-status"].textContent = refusalText(error);
        });
    }

    /** The lifecycle view is an explicit secondary action; the row's primary Board action stays. */
    function openLifecycle(place) {
        stopCatalogTimer();
        state.place = place;
        showView("detail");
        elements["project-name"].textContent = place.label || place.path;
        elements["project-path"].textContent = place.path || "";
        var mark = elements["project-mark"];
        mark.className = "project-mark" + (context.drawIcon(mark, place.icon, 5) ? "" : " none");
        clearAnswer(context);
        elements["project-status"].textContent = "";
        return lifecycle.enter(place);
    }

    /** Arriving. The list, every time — the detail is a view rather than an address. */
    function enter() {
        active = true;
        lifecycle.leave();
        showView("list");
        state.place = null;
        return loadPlaces();
    }

    /** Nothing to put back: the page that follows draws itself. Kept as the seam a page has. */
    function leave() { active = false; ++state.loading; stopCatalogTimer(); lifecycle.leave(); }

    function backToList() {
        lifecycle.leave();
        showView("list");
        state.place = null;
        // The keyboard goes back to the heading of the list it has returned to, not to a control
        // that is now hidden — `hidden` takes the focused node out of the document and the
        // browser drops focus on the body, from where nothing can be given back.
        var title = elements["projects-title"];
        if (title && title.focus) title.focus({ preventScroll: true });
    }

    elements["projects-back"].addEventListener("click", backToList);

    /* A Project is open *inside* this page, so the first Escape gives the Project back and only
       the second leaves — anything else closes two things for one press. `navigate` is the page
       router; a harness that has none leaves this inert rather than reaching for a document it
       was never given. `input/keys.js` decides when this is called; see the note on `Projects`. */
    var navigate = environment.navigate || function () { };
    Projects.escape = function () {
        if (state.view === "detail") { backToList(); return; }
        navigate("sessions");
    };

    showView("list");
    return { enter: enter, leave: leave, loadPlaces: loadPlaces, openProject: openProject,
             openLifecycle: openLifecycle,
             escape: Projects.escape, state: state };
}
