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

/**
 * The whole list, as markup.
 *
 * Pure on purpose: this is what the sheet shows, so a test can read it rather than a person
 * having to look at a screenshot. It draws rows, headings and the three things that are not a
 * list — the empty state, the read-only reason, and a transport's own refusal — and no control
 * that this wave has nothing to open: the row's `⋯` and the `＋` arrive with the editor, behind
 * `snippetControls`, and a menu drawn before the thing it opens is exactly the dead button that
 * guard exists to prevent.
 */
export function snippetsListHTML(model, options) {
    var opts = options && typeof options === "object" ? options : {};
    var groups = model && Array.isArray(model.groups) ? model.groups : [];
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
        return '<p class="snippets-note">' + esc(T.webSnippetsEmpty) + "</p>";
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
        rows.forEach(function (row) {
            var summary = snippetSummary(row.body);
            out.push('<button class="snippet-row" type="button" data-snippet="' + index + '"' +
                (opts.readOnly ? " disabled" : "") + ">");
            out.push('<span class="snippet-row-title">' + esc(snippetTitle(row)) + "</span>");
            if (summary) out.push('<span class="snippet-row-line">' + esc(summary) + "</span>");
            out.push("</button>");
            index += 1;
        });
        out.push("</section>");
    });
    return out.join("");
}
