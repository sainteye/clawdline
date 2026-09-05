import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";

/* ==========================================================================
   Snippets, as data

   The sheet's arithmetic, with no document in it. `input/snippets.js` is the
   DOM island — it builds the overlay, binds the presses and calls `appendMsg`;
   everything it decides *before* writing HTML is here, so it can be run in
   Node the way `view/user-messages-data.js` already is.

   **Nothing in this file reads `document`, `window` or `localStorage`, at
   import time or afterwards.** `core/pages.js` says why that matters: several
   of the Node suites import modules that reach this one, and a module that
   touches the document while it is being evaluated cannot be imported without
   a browser. `Tests/web-snippets.mjs` imports this file with no globals
   stubbed at all, which is what keeps the promise honest.
   ========================================================================== */

/**
 * Which of the five snippet routes this transport actually has.
 *
 * The direct path has all of them; the relay has the reading half only, because the list rides
 * the orchestrator snapshot and there is no envelope class for a write. So a control is drawn
 * from what the transport can do rather than from what the feature can do — the same shape
 * `/v1/places` and `/v1/push/key` are already asked about, and for the same reason: a button
 * that fails when pressed is worse than a button that is not there.
 */
export function snippetControls(client) {
    function has(name) {
        return !!client && typeof client[name] === "function";
    }
    return {
        read: has("snippets"),
        create: has("createSnippet"),
        update: has("updateSnippet"),
        remove: has("deleteSnippet"),
        order: has("orderSnippets")
    };
}

/**
 * Which controls this sheet may draw, once the transport's routes and this device's write
 * switch have both been asked.
 *
 * Two different refusals with one answer, because the sheet's question is not "may this device
 * write" or "does this Mac have the route" but "is there anything behind this button". A relay
 * reader and a read-only device both get a list they can look at and no control that would fail
 * when pressed, and they get it from one place rather than from a condition repeated at six
 * call sites.
 *
 * `menu` is the row's own `⋯`: it exists only if it would have something in it, so a transport
 * that somehow carried none of the three writing routes draws no menu rather than an empty one.
 */
export function snippetActions(controls, readOnly) {
    var can = controls && typeof controls === "object" ? controls : {};
    var may = readOnly !== true;
    var update = may && !!can.update;
    var remove = may && !!can.remove;
    var order = may && !!can.order;
    return {
        create: may && !!can.create,
        update: update,
        remove: remove,
        order: order,
        menu: update || remove || order
    };
}

/** The tail of a path, for a project whose answer carried no label of its own. */
function labelForKey(key) {
    var parts = String(key || "").replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || String(key || "");
}

function orderable(row, index) {
    var position = row && typeof row.position === "number" && isFinite(row.position)
        ? row.position : Number.MAX_SAFE_INTEGER;
    return { row: row, position: position, index: index };
}

function inOrder(rows) {
    return rows.map(orderable).sort(function (a, b) {
        // The order the person put them in, and — for two rows that claim the same place, or a
        // record written before `position` existed — the order they arrived in. Never the title:
        // a list that re-sorts itself when a snippet is renamed is a list nobody can point at.
        return a.position - b.position || a.index - b.index;
    }).map(function (entry) { return entry.row; });
}

/**
 * This session's snippets, in the two groups the sheet draws: this project first, then the ones
 * that belong to every project.
 *
 * **The scope key is resolved on the Mac and not here.** `GET /v1/snippets?session=<id>` answers
 * `project` beside the list, already filtered and already ordered, and when that field is present
 * this function does no more than split the answer in two — the registry match, the subdirectory
 * prefix and the worktree folding into its main checkout all happened there, where the git
 * directory and `~/.claude/project-icons.json` are.
 *
 * The relay is the one path with no such answer: `snippets()` there reads whole records out of
 * the published orchestrator snapshot, the way `schedules()` does, so `context.project` — the
 * open session's own `cwd` — is compared with `row.project` **by equality and nothing else**. A
 * phone reading a session that sits in a subdirectory of its project therefore sees the global
 * group and not the project one. That is a smaller answer, not a wrong one, and inventing the
 * prefix rule here is precisely what the design says the browser must not do.
 *
 * `context.machine` is the other half of that: snapshot rows are tagged with the machine that
 * published them, and a fleet viewer must not offer one Mac's project snippets inside another
 * Mac's session.
 */
export function snippetGroups(answer, context) {
    var reply = answer && typeof answer === "object" ? answer : {};
    var where = context && typeof context === "object" ? context : {};
    var stated = reply.project && typeof reply.project === "object" ? reply.project : null;
    var key = stated && typeof stated.key === "string" && stated.key
        ? stated.key
        : (typeof where.project === "string" && where.project ? where.project : "");
    var label = stated && typeof stated.label === "string" && stated.label
        ? stated.label : labelForKey(key);
    var rows = Array.isArray(reply.snippets) ? reply.snippets : [];

    var mine = rows.filter(function (row) {
        if (!row || typeof row !== "object") return false;
        if (typeof row.body !== "string" || !row.body) return false;
        if (where.machine && typeof row.machine === "string" && row.machine !== where.machine) {
            return false;
        }
        return true;
    });

    var project = inOrder(mine.filter(function (row) {
        return row.scope === "project" && !!key &&
            typeof row.project === "string" && row.project === key;
    }));
    var global = inOrder(mine.filter(function (row) { return row.scope === "global"; }));

    return {
        project: key ? { key: key, label: label } : null,
        groups: [
            { scope: "project", rows: project },
            { scope: "global", rows: global }
        ],
        count: project.length + global.length
    };
}

/** Every row the sheet drew, in the order it drew them — what a press index means. */
export function snippetOrder(model) {
    var groups = model && Array.isArray(model.groups) ? model.groups : [];
    return groups.reduce(function (all, group) {
        return all.concat(Array.isArray(group.rows) ? group.rows : []);
    }, []);
}

/** The line under the title: the first line of the body, with nothing else on it. A snippet is
 *  literal text and can be four thousand characters long; the row shows the beginning of it. */
export function snippetSummary(body) {
    var lines = String(body == null ? "" : body).split("\n");
    var first = "";
    for (var i = 0; i < lines.length && !first; i++) first = lines[i].trim();
    if (first.length > 140) first = first.slice(0, 139) + "…";
    return first;
}

/** The title a row shows. A record whose title never made it is still pressable, and says what
 *  it will insert rather than showing an empty line. */
export function snippetTitle(row) {
    var given = row && typeof row.title === "string" ? row.title.trim() : "";
    return given || snippetSummary(row && row.body);
}

function groupHeading(scope) {
    return scope === "project" ? T.webSnippetsThisProject : T.webSnippetsEveryProject;
}

/* ==========================================================================
   The editor, as arithmetic

   Everything below decides what to send and what a control would do; none of
   it sends anything and none of it touches a document. The bodies are built
   here rather than at the call sites because the store's key set is **exact**
   — `Sources/Snippets.swift` answers `400 malformed_snippet` for an unknown
   key and `400 snippet_scope_mismatch` for a `project` that is present when
   the scope is global, *including a `project: null`*. A body assembled by
   spreading a row and overwriting two fields would hit both of those, and it
   would hit them on a phone, once, in a way no test written against the
   happy path would catch.
   ========================================================================== */

/** Both limits are the store's, counted here so the sheet refuses before the Mac has to.
 *  JavaScript counts UTF-16 units where Swift counts characters, so this is the stricter of
 *  the two for anything outside the basic plane — refusing early is safe, allowing late is not. */
var TITLE_MAX = 60;
var BODY_MAX = 4000;

/** The text a field actually holds, with the whitespace a textarea collects at either end
 *  taken off. A snippet is literal text and every character between those ends is kept. */
function trimmed(value) {
    return String(value == null ? "" : value).replace(/^\s+|\s+$/g, "");
}

/**
 * What the editor starts with: an existing row, or a blank one.
 *
 * **Scope defaults to every project.** That was the ask, and it is the common case — a snippet
 * worth pressing twice is usually worth pressing in two projects. A row being changed keeps
 * whatever scope it already has.
 */
export function snippetDraft(row, context) {
    var seed = context && typeof context === "object" ? context : {};
    var base = row && typeof row === "object" ? row : null;
    var scope = base ? base.scope : seed.scope;
    return {
        id: base && typeof base.id === "string" ? base.id : "",
        title: base && typeof base.title === "string" ? base.title
            : String(seed.title == null ? "" : seed.title),
        body: base && typeof base.body === "string" ? base.body
            : String(seed.body == null ? "" : seed.body),
        scope: scope === "project" ? "project" : "global",
        project: base && typeof base.project === "string" ? base.project
            : String(seed.project == null ? "" : seed.project)
    };
}

/** Why this draft cannot be saved yet, or `""`. Three answers, because they need three
 *  different sentences: nothing typed, too much typed, and a project scope with no project —
 *  the last of which the sheet prevents rather than reports, so it is a guard and not a label. */
export function snippetDraftProblem(draft) {
    var made = draft && typeof draft === "object" ? draft : {};
    var title = trimmed(made.title);
    var body = trimmed(made.body);
    if (!title || !body) return "empty";
    if (title.length > TITLE_MAX || body.length > BODY_MAX) return "long";
    if (made.scope === "project" && !trimmed(made.project)) return "scope";
    return "";
}

/** `POST /v1/snippets`: title, body, scope, and `project` **if and only if** the scope is
 *  project. Null for a draft that is not ready, so a caller cannot send one by accident. */
export function snippetCreateBody(draft) {
    if (snippetDraftProblem(draft) !== "") return null;
    var body = { title: trimmed(draft.title), body: trimmed(draft.body), scope: draft.scope };
    if (draft.scope === "project") body.project = trimmed(draft.project);
    return body;
}

/**
 * `PATCH /v1/snippets/:id`: the fields that actually changed, and nothing else.
 *
 * Scope and project move together or not at all — sending `scope` without its `project`, or a
 * `project` alongside `scope: "global"`, are each one of the store's two refusals. An empty
 * object means nothing changed, which is a sheet to close rather than a request to make: the
 * store refuses a patch with no fields in it, and it is right to.
 */
export function snippetPatchBody(draft, row) {
    if (snippetDraftProblem(draft) !== "") return null;
    var was = row && typeof row === "object" ? row : {};
    var wasScope = was.scope === "project" ? "project" : "global";
    var wasProject = typeof was.project === "string" ? was.project : "";
    var title = trimmed(draft.title);
    var body = trimmed(draft.body);
    var patch = {};
    if (title !== was.title) patch.title = title;
    if (body !== was.body) patch.body = body;
    if (draft.scope !== wasScope ||
        (draft.scope === "project" && trimmed(draft.project) !== wasProject)) {
        patch.scope = draft.scope;
        if (draft.scope === "project") patch.project = trimmed(draft.project);
    }
    return patch;
}

/** The row menu's scope item: the patch that moves one snippet to the other scope. Null when
 *  there is nowhere to move it — a global row in a session whose project the Mac did not
 *  resolve has no project to be moved into, and the menu leaves the item out. */
export function snippetScopeSwap(row, projectKey) {
    if (!row || typeof row !== "object") return null;
    if (row.scope === "project") return { scope: "global" };
    if (row.scope !== "global") return null;
    var key = trimmed(projectKey);
    return key ? { scope: "project", project: key } : null;
}

/** One row swapped with its neighbour inside its own group, or null when the move runs off
 *  the end. Buttons rather than drag: dragging inside a scrolling sheet on a phone is a fight
 *  nobody wins. */
export function snippetReorder(rows, id, delta) {
    var list = Array.isArray(rows) ? rows.slice() : [];
    var step = delta < 0 ? -1 : 1;
    var from = -1;
    for (var i = 0; i < list.length; i++) {
        if (list[i] && list[i].id === id) { from = i; break; }
    }
    if (from < 0) return null;
    var to = from + step;
    if (to < 0 || to >= list.length) return null;
    var moved = list[from];
    list[from] = list[to];
    list[to] = moved;
    return list;
}

/**
 * `POST /v1/snippets/order`: one scope and its complete order.
 *
 * The same promise `POST /v1/orchestrator/landing-queue/order` makes — it may reorder the
 * members of that scope and may never add or remove one — so the ids come from the group the
 * sheet is showing, which is exactly the set the Mac answered for that scope. A row with no
 * id at all makes the whole body null rather than an order the store would refuse halfway.
 */
export function snippetOrderBody(scope, projectKey, rows) {
    var list = Array.isArray(rows) ? rows : [];
    var order = [];
    for (var i = 0; i < list.length; i++) {
        var id = list[i] && typeof list[i].id === "string" ? list[i].id : "";
        if (!id) return null;
        order.push(id);
    }
    if (!order.length) return null;
    if (scope === "project") {
        var key = trimmed(projectKey);
        return key ? { scope: "project", project: key, order: order } : null;
    }
    if (scope !== "global") return null;
    return { scope: "global", order: order };
}

/**
 * The two the design was written from, offered on an empty list as one press each.
 *
 * It is the cheapest possible answer to "what is this for", and the text is what the person
 * already types. Global, because that is the default and because a starter that belonged to
 * one project would teach the wrong thing about the feature on its first use.
 */
export function snippetStarters() {
    return [
        { key: "commit", title: T.webSnippetStarterCommitTitle,
          body: T.webSnippetStarterCommitBody, scope: "global" },
        { key: "report", title: T.webSnippetStarterReportTitle,
          body: T.webSnippetStarterReportBody, scope: "global" }
    ];
}

/**
 * A draft made out of something the person already sent.
 *
 * The whole message is the body; its first line, cut to the store's sixty characters, is the
 * title. `view/user-messages-data.js` is what finds the message — this only shapes it, so the
 * 我傳出的訊息 sheet and this one cannot disagree about which turn "my last message" means.
 */
export function snippetDraftFromText(text) {
    var body = trimmed(text);
    var first = snippetSummary(body);
    var title = first.length > TITLE_MAX ? first.slice(0, TITLE_MAX - 1) + "…" : first;
    return snippetDraft(null, { title: title, body: body, scope: "global" });
}

/** One button in a row's own `⋯`. */
function menuItem(attribute, index, label, extra) {
    return '<button class="snippet-act' + (extra || "") + '" type="button" ' +
        attribute + '="' + index + '">' + esc(label) + "</button>";
}

/**
 * The whole list, as markup.
 *
 * Pure on purpose: this is what the sheet shows, so a test can read it rather than a person
 * having to look at a screenshot. It draws rows, headings and the three things that are not a
 * list — the empty state, the read-only reason, and a transport's own refusal.
 *
 * **Every writing control is drawn from `snippetActions`, or not drawn.** On the Cloud path
 * reading works and writing does not, and a device may be paired for reading alone; in both
 * cases the row's `⋯`, its items and the empty state's starters are absent rather than
 * disabled, because a button that fails when pressed is worse than one that is not there.
 * The `＋` is the same question asked in `input/snippets.js`, where the sheet's head lives.
 *
 * The row menu opens **inside the list**, under the row it belongs to, rather than floating
 * over it. A menu positioned against a row inside a scroller has to be moved every time the
 * scroller moves; a menu that is part of the list moves because the list moved.
 */
export function snippetsListHTML(model, options) {
    var opts = options && typeof options === "object" ? options : {};
    var groups = model && Array.isArray(model.groups) ? model.groups : [];
    var may = snippetActions(opts.controls, opts.readOnly);
    var openMenu = typeof opts.menuFor === "number" ? opts.menuFor : -1;
    var projectKey = model && model.project ? model.project.key : "";
    var index = 0;
    var out = [];

    // The sheet is on screen before the answer is. Nothing, rather than the empty state — "there
    // are none" is a fact about a list that has arrived, and saying it for the half-second before
    // one does is the page asserting something nobody has told it yet.
    if (opts.loading) return "";
    if (opts.error) {
        return '<p class="snippets-note">' + esc(opts.error) + "</p>";
    }
    if (!snippetOrder(model).length) {
        // Two empty states. A device that can write is shown the door and the two snippets this
        // design was written from; a device that cannot is told where snippets come from, which
        // is the only useful thing to say to somebody who cannot make one here.
        if (!may.create) {
            return '<p class="snippets-note">' + esc(T.webSnippetsEmpty) + "</p>";
        }
        out.push('<p class="snippets-note">' + esc(T.webSnippetsEmptyNew) + "</p>");
        out.push('<div class="snippet-starters">');
        snippetStarters().forEach(function (starter, at) {
            out.push('<button class="snippet-starter" type="button" data-snippet-starter="' +
                at + '">');
            out.push('<span class="snippet-row-title">' + esc(starter.title) + "</span>");
            out.push('<span class="snippet-row-line">' + esc(snippetSummary(starter.body)) +
                "</span>");
            out.push("</button>");
        });
        out.push("</div>");
        return out.join("");
    }
    if (opts.readOnly) {
        out.push('<p class="snippets-note snippets-read-only">' + esc(T.webSnippetsReadOnly) + "</p>");
    }

    groups.forEach(function (group) {
        var rows = Array.isArray(group.rows) ? group.rows : [];
        if (!rows.length) return;
        var where = group.scope === "project" && model.project ? model.project.label : "";
        out.push('<section class="snippet-group">');
        out.push('<h3 class="snippet-group-title">' + esc(groupHeading(group.scope)) +
            (where ? '<span class="snippet-group-where">' + esc(where) + "</span>" : "") +
            "</h3>");
        rows.forEach(function (row, at) {
            var summary = snippetSummary(row.body);
            // A row cannot be "the one with its menu open" on a transport that draws no menus.
            // Left as a bare index comparison, a relay reader's list still carried the open
            // class — a state about a control that is not there.
            var open = may.menu && index === openMenu;
            out.push('<div class="snippet-line' + (open ? " open" : "") + '">');
            out.push('<button class="snippet-row" type="button" data-snippet="' + index + '"' +
                (opts.readOnly ? " disabled" : "") + ">");
            out.push('<span class="snippet-row-title">' + esc(snippetTitle(row)) + "</span>");
            if (summary) out.push('<span class="snippet-row-line">' + esc(summary) + "</span>");
            out.push("</button>");
            if (may.menu) {
                out.push('<button class="snippet-more" type="button" data-snippet-more="' +
                    index + '" aria-haspopup="menu" aria-expanded="' + (open ? "true" : "false") +
                    '" aria-label="' + esc(T.webSnippetMore) + '">⋯</button>');
            }
            out.push("</div>");
            if (may.menu && open) {
                var swap = snippetScopeSwap(row, projectKey);
                out.push('<div class="snippet-menu" role="menu">');
                if (may.update) {
                    out.push(menuItem("data-snippet-edit", index, T.webSnippetEdit));
                }
                if (may.order && at > 0) {
                    out.push(menuItem("data-snippet-up", index, T.webSnippetUp));
                }
                if (may.order && at < rows.length - 1) {
                    out.push(menuItem("data-snippet-down", index, T.webSnippetDown));
                }
                if (may.update && swap) {
                    out.push(menuItem("data-snippet-scope", index,
                        swap.scope === "global" ? T.webSnippetToGlobal : T.webSnippetToProject));
                }
                if (may.remove) {
                    out.push(menuItem("data-snippet-delete", index, T.webSnippetDelete,
                        " danger"));
                }
                out.push("</div>");
            }
            index += 1;
        });
        out.push("</section>");
    });
    return out.join("");
}
