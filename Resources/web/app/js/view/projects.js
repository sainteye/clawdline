import { T, fill } from "../core/i18n.js";

/* ==========================================================================
   The Projects page

   Two questions, one page. The first is "where could I start a session" and
   it is answered by `/v1/places` — directories an assistant has actually been
   run in and that are still on the disk. The second is asked in front of one
   of them: **which of this Project's worktrees finished a Feature, and did it
   reach the branch.** That is
   `GET /v1/orchestrator/usage/project-worktrees`, and the reason it is not
   `git worktree list` is that this Mac carries 58 managed checkouts and the
   ledger remembers 150, most of which produced nothing anybody kept.

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

   This module imports nothing but the words, so `Tests/web-projects.mjs` can
   drive the whole of it against a stand-in document. The transport arrives
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
var SECONDARY = ["landed", "branch_gone", "active", "abandoned", "unknown"];

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
    if (features) appendText(doc, item, "h3", features, "project-worktree-features");
    var id = appendText(doc, item, "code", String(worktree.id).slice(0, 8), "project-worktree-id");
    id.title = String(worktree.id);
    var facts = doc.createElement("div");
    facts.className = "project-facts";
    fact(doc, facts, T.webProjectBranch, branchOf(worktree), "project-fact-branch");
    var evidence = fact(doc, facts, T.webProjectEvidence, evidenceText(worktree),
                        "project-fact-evidence");
    evidence.dataset.evidence = worktree.landingEvidence || "unknown";
    fact(doc, facts, T.webProjectRuns, number(worktree.runs));
    fact(doc, facts, T.webProjectSeen, seenText(worktree));
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
        var name = appendText(doc, text, "strong", place.label || place.path, "project-row-name");
        name.style.color = place.icon ? context.tint(place.icon.accent) : "";
        // The path is here for the one job `/v1/places` says it is for: telling two projects with
        // the same name apart. Nothing on this page is built out of it except the query below.
        appendText(doc, text, "span", place.path || "", "project-row-path");
        button.appendChild(text);
        button.setAttribute("aria-label", fill(T.webProjectOpenLabel, { name: place.label || place.path }));
        button.addEventListener("click", function () { context.open(place); });
        item.appendChild(button);
        rows.appendChild(item);
    });
}

export function bindProjectsPage(elements, environment) {
    environment = environment || {};
    var doc = environment.document || document;
    var state = { view: "list", places: null, place: null, status: "", loading: 0 };
    var context = {
        document: doc, elements: elements, state: state,
        drawIcon: environment.drawIcon || function () { return false; },
        tint: environment.tint || function () { return ""; },
        open: function (place) { openProject(place); }
    };
    /* Both reads are absent on the Cloud path — see the note at the top of this file and the
       Cloud section of `docs/api.md`. They arrive as functions or as nothing at all, and nothing
       at all is a sentence rather than a button that fails when pressed. */
    var readPlaces = typeof environment.places === "function" ? environment.places : null;
    var readWorktrees = typeof environment.projectWorktrees === "function"
        ? environment.projectWorktrees : null;
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
    function loadPlaces() {
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
        }).catch(function (error) {
            if (ticket !== state.loading) return;
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
        return readWorktrees(place.path).then(function (data) {
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

    /** Arriving. The list, every time — the detail is a view rather than an address. */
    function enter() {
        showView("list");
        state.place = null;
        return loadPlaces();
    }

    /** Nothing to put back: the page that follows draws itself. Kept as the seam a page has. */
    function leave() { }

    function backToList() {
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
             escape: Projects.escape, state: state };
}
