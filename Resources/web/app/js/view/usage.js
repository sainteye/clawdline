/* Project Portfolio rendering. Accounting decisions are made by UsageQueryService; this module
   only turns its typed availability and reason codes into visible, keyboard-reachable UI. */

export function formatUsageNumber(value) {
    if (value === null || value === undefined) return "Unknown";
    return new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value);
}

function significant(value) {
    if (value === null || value === undefined) return "Unknown";
    return new Intl.NumberFormat(undefined, { maximumSignificantDigits: 6 }).format(value);
}

function signed(value) {
    if (value === 0) return "0";
    return (value > 0 ? "+" : "") + formatUsageNumber(value);
}

export function describeUsageComparison(comparison) {
    comparison = comparison || {};
    if (comparison.status === "comparable") {
        var percent = comparison.percent === null || comparison.percent === undefined
            ? "percent unavailable" : signed(Math.round(comparison.percent)) + "%";
        return signed(comparison.absolute || 0) + " · " + percent;
    }
    var reasons = {
        closed_range_required: "choose a closed date range",
        range_truncated: "one range exceeds the scan limit",
        no_previous_data: "no work in the equal previous range",
        incomplete_output: "output coverage is incomplete"
    };
    return "Change unavailable · " + (reasons[comparison.reason] || "ranges are not comparable");
}

export function rankUsageProjects(projects) {
    return (projects || []).slice().sort(function (left, right) {
        var a = typeof left.output === "number" ? left.output : -1;
        var b = typeof right.output === "number" ? right.output : -1;
        if (a !== b) return b - a;
        return String(left.id || "").localeCompare(String(right.id || ""));
    });
}

function clear(node) {
    while (node.firstChild) node.removeChild(node.firstChild);
}

function appendText(doc, parent, tag, value, className) {
    var node = doc.createElement(tag);
    if (className) node.className = className;
    node.textContent = value;
    parent.appendChild(node);
    return node;
}

function tableCell(doc, row, label, value, className) {
    var cell = appendText(doc, row, "td", value, className);
    cell.dataset.label = label;
    cell.setAttribute("role", "cell");
    return cell;
}

function costText(cost) {
    if (!cost || cost.status !== "available") {
        var reason = cost && cost.reason ? cost.reason.replaceAll("_", " ") : "unavailable";
        return "Unavailable · " + reason;
    }
    return significant(cost.value) + " " + cost.unit + " · " + cost.basis;
}

function lineageText(lineage) {
    if (!lineage || lineage.status === "unavailable") return "Unavailable";
    var value = formatUsageNumber(lineage.rootRuns) + " / " + formatUsageNumber(lineage.childRuns);
    return lineage.status === "partial" ? value + " · partial" : value;
}

function coverageText(coverage) {
    if (!coverage) return "Unavailable";
    if (coverage.status === "complete") return "Complete";
    return (coverage.status === "partial" ? "Partial" : "Unavailable")
        + " · " + formatUsageNumber(coverage.unknownOutputRuns) + " unknown output";
}

function errorFrom(response) {
    return response.json().catch(function () { return {}; }).then(function (body) {
        var error = body.error || {}, code = error.code || "usage_error";
        if (code === "usage_analytics_busy") {
            throw new Error("Usage Analytics is busy; sessions remain available. Try again shortly.");
        }
        if (code === "export_too_large") {
            throw new Error("This range exceeds the matched-row export limit. Narrow the dates or filters and try again.");
        }
        throw new Error(error.message || "Usage could not be read (" + response.status + ").");
    });
}

function localDay(date, timezone) {
    var parts = new Intl.DateTimeFormat("en-CA", {
        timeZone: timezone, year: "numeric", month: "2-digit", day: "2-digit"
    }).formatToParts(date).reduce(function (out, part) {
        out[part.type] = part.value;
        return out;
    }, {});
    return parts.year + "-" + parts.month + "-" + parts.day;
}

function option(doc, value, label) {
    var node = doc.createElement("option");
    node.value = value;
    node.textContent = label;
    return node;
}

function renderProjects(context, projects) {
    var doc = context.document, elements = context.elements, state = context.state;
    var body = elements["usage-project-list"];
    clear(body);
    projects = rankUsageProjects(projects);
    elements["usage-project-count"].textContent = projects.length + (projects.length === 1 ? " Project" : " Projects");
    if (!projects.length) {
        var empty = doc.createElement("tr"), cell = tableCell(doc, empty, "Projects", "No Project work in this range", "usage-empty");
        empty.setAttribute("role", "row");
        cell.colSpan = 9;
        body.appendChild(empty);
        elements["usage-project-detail"].hidden = true;
        state.selectedProject = null;
        return;
    }
    if (!projects.some(function (project) { return project.id === state.selectedProject; })) {
        state.selectedProject = projects[0].id;
    }
    projects.forEach(function (project, index) {
        var row = doc.createElement("tr");
        row.setAttribute("role", "row");
        row.dataset.projectId = project.id;
        row.dataset.selected = String(project.id === state.selectedProject);
        var nameCell = doc.createElement("td"), name = doc.createElement("span");
        nameCell.setAttribute("role", "cell");
        nameCell.dataset.label = "Project";
        name.className = "usage-project-name";
        appendText(doc, name, "span", "#" + (project.rank || index + 1), "usage-project-rank");
        appendText(doc, name, "strong", project.label || "Unknown Project");
        nameCell.appendChild(name);
        row.appendChild(nameCell);
        var output = tableCell(doc, row, "Generated output", formatUsageNumber(project.output), "usage-value usage-value-primary");
        if (project.unknownOutputRuns) appendText(doc, output, "small", project.unknownOutputRuns + " unknown", "usage-subvalue");
        tableCell(doc, row, "Agent work", formatUsageNumber(project.runs), "usage-value");
        tableCell(doc, row, "Scheduled", formatUsageNumber(project.scheduledRuns), "usage-value");
        tableCell(doc, row, "Root / child", lineageText(project.lineage));
        tableCell(doc, row, "Estimated spending (Claude Code)", costText(project.cost));
        tableCell(doc, row, "Coverage", coverageText(project.coverage));
        tableCell(doc, row, "Change", describeUsageComparison(project.comparison));
        var action = doc.createElement("td"), button = doc.createElement("button");
        action.setAttribute("role", "cell");
        action.dataset.label = "Open";
        button.type = "button";
        button.className = "usage-open-project";
        button.dataset.projectId = project.id;
        button.textContent = project.id === state.selectedProject ? "Viewing" : "Open";
        button.setAttribute("aria-label", "Open " + (project.label || "Unknown Project") + " details");
        button.addEventListener("click", function () {
            state.selectedProject = project.id;
            var controls = body.querySelectorAll(".usage-open-project");
            for (var i = 0; i < controls.length; i++) {
                var selected = controls[i].dataset.projectId === project.id;
                controls[i].textContent = selected ? "Viewing" : "Open";
                if (controls[i].parentNode && controls[i].parentNode.parentNode) {
                    controls[i].parentNode.parentNode.dataset.selected = String(selected);
                }
            }
            renderProjectDetail(context, project);
            // The activated button was not replaced, so browser focus remains on the equivalent
            // Project action instead of falling back to the document body.
            button.focus();
        });
        action.appendChild(button);
        row.appendChild(action);
        body.appendChild(row);
    });
    renderProjectDetail(context, projects.find(function (project) {
        return project.id === state.selectedProject;
    }) || projects[0]);
}

function renderTrend(context, trend) {
    var doc = context.document, node = context.elements["usage-project-trend"];
    clear(node);
    if (!trend || !trend.length) {
        appendText(doc, node, "p", "No output buckets in this range.", "usage-empty");
        return;
    }
    var maximum = Math.max.apply(null, trend.map(function (item) {
        return item.tokens && typeof item.tokens.output === "number" ? item.tokens.output : 0;
    }).concat([1]));
    trend.forEach(function (item) {
        var column = doc.createElement("div"), bar = doc.createElement("i");
        var value = item.tokens && typeof item.tokens.output === "number" ? item.tokens.output : null;
        column.className = "usage-mini-column";
        bar.className = "usage-mini-bar";
        bar.style.height = value === null ? "2px" : Math.max(3, (value / maximum) * 62) + "px";
        bar.title = item.bucket + ": " + formatUsageNumber(value) + " generated output";
        column.appendChild(bar);
        appendText(doc, column, "span", item.bucket.slice(5));
        node.appendChild(column);
    });
}

function renderMix(context, project) {
    var doc = context.document, node = context.elements["usage-project-mix"];
    clear(node);
    var groups = (project.assistantMix || []).concat(project.workMix || []);
    if (!groups.length) {
        appendText(doc, node, "p", "Assistant and work mix unavailable.", "usage-empty");
        return;
    }
    var maximum = Math.max.apply(null, groups.map(function (item) {
        return typeof item.output === "number" ? item.output : 0;
    }).concat([1]));
    var list = doc.createElement("div");
    list.className = "usage-mix-list";
    groups.forEach(function (item) {
        var row = doc.createElement("div"), track = doc.createElement("span"), fill = doc.createElement("i");
        row.className = "usage-mix-row";
        appendText(doc, row, "span", item.label);
        track.className = "usage-mix-track";
        fill.style.width = (typeof item.output === "number" ? (item.output / maximum) * 100 : 0) + "%";
        track.appendChild(fill);
        row.appendChild(track);
        appendText(doc, row, "strong", formatUsageNumber(item.output));
        list.appendChild(row);
    });
    node.appendChild(list);
}

function renderLineage(context, lineage) {
    var doc = context.document, node = context.elements["usage-project-lineage"];
    clear(node);
    lineage = lineage || { status: "unavailable", rootRuns: 0, childRuns: 0,
                           scheduledRuns: 0, unknownRuns: 0 };
    var grid = doc.createElement("div");
    grid.className = "usage-lineage-grid";
    [["Root", lineage.rootRuns], ["Child", lineage.childRuns],
     ["Scheduled", lineage.scheduledRuns], ["Unknown", lineage.unknownRuns]].forEach(function (item) {
        var card = doc.createElement("div");
        appendText(doc, card, "span", item[0]);
        appendText(doc, card, "strong", formatUsageNumber(item[1]));
        grid.appendChild(card);
    });
    node.appendChild(grid);
    if (lineage.status !== "available") {
        appendText(doc, node, "p", "Lineage " + lineage.status
            + " · missing evidence stays Unknown; scheduled work is never guessed as root or child.",
            "usage-summary");
    }
}

function renderProjectRecent(context, rows) {
    var doc = context.document, node = context.elements["usage-project-recent"];
    clear(node);
    (rows || []).forEach(function (row) {
        var item = doc.createElement("li"), label = (row.assistant || "Unknown") + " · " + (row.model || "Unknown model");
        appendText(doc, item, "span", label);
        appendText(doc, item, "strong", formatUsageNumber((row.tokens || {}).output));
        node.appendChild(item);
    });
    if (!node.firstChild) appendText(doc, node, "li", "No recent work.", "usage-empty");
}

function renderProjectDetail(context, project) {
    var elements = context.elements;
    if (!project) {
        elements["usage-project-detail"].hidden = true;
        return;
    }
    elements["usage-project-detail"].hidden = false;
    elements["usage-project-detail-title"].textContent = project.label || "Unknown Project";
    elements["usage-project-rank"].textContent = "#" + (project.rank || "—");
    elements["usage-project-summary"].textContent = formatUsageNumber(project.output)
        + " generated output across " + formatUsageNumber(project.runs) + " agent-work runs · "
        + describeUsageComparison(project.comparison) + " · " + costText(project.cost) + ".";
    renderTrend(context, project.trend || []);
    renderMix(context, project);
    renderLineage(context, project.lineage);
    renderProjectRecent(context, project.recentWork || []);
}

function renderScheduledWork(context, scheduled) {
    var doc = context.document, elements = context.elements, body = elements["usage-schedule-body"];
    clear(body);
    (scheduled.schedules || []).forEach(function (item) {
        var row = doc.createElement("tr");
        row.setAttribute("role", "row");
        tableCell(doc, row, "Schedule", item.label || item.id);
        tableCell(doc, row, "Runs", formatUsageNumber(item.runs), "usage-value");
        tableCell(doc, row, "Active days", formatUsageNumber(item.activeDays), "usage-value");
        tableCell(doc, row, "Generated output", formatUsageNumber(item.output), "usage-value usage-value-primary");
        tableCell(doc, row, "Coverage", coverageText(item.coverage));
        body.appendChild(row);
    });
    if (!body.firstChild) {
        var empty = doc.createElement("tr"), cell = tableCell(doc, empty, "Scheduled Work", "No explicit schedule identity in this range", "usage-empty");
        empty.setAttribute("role", "row");
        cell.colSpan = 5;
        body.appendChild(empty);
    }
    var unknown = scheduled.unknownSchedule || {};
    elements["usage-unknown-schedule"].textContent = unknown.runs
        ? formatUsageNumber(unknown.runs) + " scheduled runs remain Unknown Schedule because identity is missing."
        : "Every scheduled run in this range has explicit schedule identity.";
}

function renderFeatures(context, features) {
    var doc = context.document, elements = context.elements, body = elements["usage-feature-body"];
    clear(body);
    elements["usage-feature-summary"].textContent = features.automaticAttribution === false
        ? "Automatic Feature attribution is not configured. Accepted manual or external assignments appear here."
        : "A Feature appears only with one unambiguous accepted attribution head.";
    (features.groups || []).forEach(function (item) {
        var row = doc.createElement("tr");
        row.setAttribute("role", "row");
        tableCell(doc, row, "Feature", item.label || item.id);
        tableCell(doc, row, "Agent work", formatUsageNumber(item.runs), "usage-value");
        tableCell(doc, row, "Generated output", formatUsageNumber(item.output), "usage-value usage-value-primary");
        tableCell(doc, row, "Coverage", coverageText(item.coverage));
        body.appendChild(row);
    });
    if (!body.firstChild) {
        var empty = doc.createElement("tr"), cell = tableCell(doc, empty, "Features", "No accepted Feature attribution in this range", "usage-empty");
        empty.setAttribute("role", "row");
        cell.colSpan = 4;
        body.appendChild(empty);
    }
    var unknown = features.unknown || {};
    elements["usage-unknown-feature"].textContent = formatUsageNumber(unknown.runs || 0)
        + " runs remain Unknown Feature. Proposals, rejections, and conflicting accepted heads never enter a named total.";
}

function renderInsights(context, insights) {
    var doc = context.document, node = context.elements["usage-insights"];
    clear(node);
    (insights || []).forEach(function (insight) {
        var card = doc.createElement("article");
        card.className = "usage-insight";
        appendText(doc, card, "span", (insight.kind || "clue").replaceAll("_", " "));
        appendText(doc, card, "strong", insight.title || "Operational clue");
        appendText(doc, card, "p", insight.detail || "Inspect the underlying work before acting.");
        node.appendChild(card);
    });
    if (!node.firstChild) {
        appendText(doc, node, "p", "No sound cross-range clue is available yet. This is not a zero; the necessary comparison may be unavailable.", "usage-empty");
    }
}

function coverageLine(context, label, value, warning) {
    var doc = context.document, item = doc.createElement("li");
    if (warning) item.className = "usage-warning";
    appendText(doc, item, "span", label);
    appendText(doc, item, "strong", formatUsageNumber(value));
    context.elements["usage-coverage-list"].appendChild(item);
}

function renderCoverage(context, data) {
    var totals = data.totals || {}, coverage = totals.coverage || {};
    var states = coverage.states || {}, reasons = coverage.reasons || {};
    clear(context.elements["usage-coverage-list"]);
    Object.keys(states).sort().forEach(function (key) {
        coverageLine(context, "Coverage · " + key, states[key], key !== "complete");
    });
    Object.keys(reasons).sort().forEach(function (key) {
        coverageLine(context, key.replaceAll("_", " "), reasons[key], true);
    });
    coverageLine(context, "Unknown token rows", coverage.tokenRowsUnknown,
                 typeof coverage.tokenRowsUnknown === "number" && coverage.tokenRowsUnknown > 0);
    coverageLine(context, "Corrections", data.corrections,
                 typeof data.corrections === "number" && data.corrections > 0);
}

function openDetail(context, row) {
    var doc = context.document, elements = context.elements, list = elements["usage-detail-list"];
    clear(list);
    var tokens = row.tokens || {}, cost = row.cost;
    var facts = {
        Started: row.startedAt, Assistant: row.assistant, Model: row.model,
        Project: row.project, "Generated output": formatUsageNumber(tokens.output),
        "New input": formatUsageNumber(tokens.inputNew), "Cache read": formatUsageNumber(tokens.cacheRead),
        "Cache write": formatUsageNumber(tokens.cacheWrite), "Measured floor": formatUsageNumber(row.measuredFloor),
        "Strict total": formatUsageNumber(row.strictTotal), Coverage: row.coverage,
        "Coverage reasons": (row.coverageReasons || []).join(", ") || "None",
        "Unknown token parts": (row.unknownTokenParts || []).join(", ") || "None",
        Cost: cost ? significant(cost.value) + " " + cost.unit + " · " + cost.basis : "Unknown",
        "Missing cost": row.missingCostReason || "None", Ended: row.endedAt || "Unknown",
        "Source total": formatUsageNumber(row.sourceTotal), Reconciliation: row.reconciliation || "None",
        "Input basis": row.inputBasis || "Unknown"
    };
    Object.keys(facts).forEach(function (key) {
        appendText(doc, list, "dt", key);
        appendText(doc, list, "dd", facts[key] === null ? "Unknown" : facts[key]);
    });
    var dialog = elements["usage-detail"];
    if (dialog.showModal) dialog.showModal();
    else dialog.setAttribute("open", "");
}

function renderRows(context, data, append) {
    var doc = context.document, elements = context.elements, state = context.state;
    if (!append) {
        state.rows = [];
        clear(elements["usage-agent-list"]);
    }
    state.rows = state.rows.concat(data.rows || []);
    state.cursor = data.pagination && data.pagination.nextCursor;
    (data.rows || []).forEach(function (row) {
        var item = doc.createElement("li"), body = doc.createElement("div"), button = doc.createElement("button");
        var tokens = row.tokens || {};
        item.className = "usage-agent-card";
        appendText(doc, body, "h3", (row.project || "Unknown Project") + " · " + (row.assistant || "Unknown"));
        appendText(doc, body, "p", new Date(row.startedAt).toLocaleString() + " · " + (row.model || "Unknown model") + " · " + row.coverage);
        var metrics = doc.createElement("div");
        metrics.className = "usage-agent-metrics";
        appendText(doc, metrics, "span", "Output " + formatUsageNumber(tokens.output));
        appendText(doc, metrics, "span", "New input " + formatUsageNumber(tokens.inputNew));
        appendText(doc, metrics, "span", "Cache read " + formatUsageNumber(tokens.cacheRead));
        body.appendChild(metrics);
        button.type = "button";
        button.className = "usage-row-open";
        button.textContent = "Open";
        button.addEventListener("click", function () { openDetail(context, row); });
        item.appendChild(body);
        item.appendChild(button);
        elements["usage-agent-list"].appendChild(item);
    });
    elements["usage-more"].hidden = !(data.pagination && data.pagination.hasMore);
}

function renderMeta(context, data) {
    var doc = context.document, node = context.elements["usage-meta"];
    clear(node);
    [
        { value: (data.range || {}).timezone },
        { value: "Schema " + data.schemaVersion },
        { value: "Ledger freshness: " + ((data.freshness || {}).status || "unknown"), state: (data.freshness || {}).status },
        { value: "Range data through: " + ((data.rangeFreshness || {}).dataThrough || "none") },
        { value: "Observed prices: " + (((data.priceSnapshot || {}).observedIds || []).join(", ") || "none") }
    ].forEach(function (item) {
        var span = appendText(doc, node, "span", item.value);
        if (item.state) span.dataset.state = item.state;
    });
}

function renderPortfolio(context, data) {
    var elements = context.elements, portfolio = data.portfolio || {}, totals = data.totals || {};
    var projects = rankUsageProjects(portfolio.projects || []);
    elements["usage-measured"].textContent = formatUsageNumber((totals.tokens || {}).output);
    elements["usage-output-change"].textContent = describeUsageComparison(portfolio.comparison);
    elements["usage-run-count"].textContent = formatUsageNumber(portfolio.runs);
    var scheduled = portfolio.scheduledWork || {};
    var scheduledUnknown = scheduled.unknownOutputRuns || 0;
    elements["usage-scheduled-output"].textContent = typeof scheduled.output === "number"
        ? formatUsageNumber(scheduled.output) + (scheduledUnknown ? " measured" : "")
        : "Unknown";
    elements["usage-scheduled-runs"].textContent = formatUsageNumber(scheduled.runs || 0)
        + " scheduled runs · " + formatUsageNumber(scheduledUnknown) + " Unknown output";
    var rowCount = totals.rows || 0, unknown = totals.tokenPartsUnknown && totals.tokenPartsUnknown.output || 0;
    elements["usage-coverage-kpi"].textContent = rowCount
        ? Math.round(((rowCount - unknown) / rowCount) * 100) + "%" : "Unknown";
    elements["usage-unknown-count"].textContent = formatUsageNumber(unknown) + " rows have unknown output";
    renderProjects(context, projects);
    renderScheduledWork(context, portfolio.scheduledWork || {});
    renderFeatures(context, portfolio.features || {});
    renderInsights(context, portfolio.insights || []);
    renderCoverage(context, data);
}

export function bindUsagePortfolio(elements, environment) {
    environment = environment || {};
    var doc = environment.document || document;
    var request = environment.fetch || fetch;
    var state = { data: null, rows: [], cursor: null, loading: false, pending: null,
                  view: "overview", exporting: false, selectedProject: null };
    var context = { document: doc, elements: elements, state: state };

    function params(cursor) {
        var query = new URLSearchParams();
        query.set("from", elements["usage-from"].value);
        query.set("to", elements["usage-to"].value);
        query.set("timezone", elements["usage-timezone"].value);
        query.set("group", "project");
        query.set("bucket", "day");
        query.set("view", state.view);
        query.set("limit", "50");
        if (cursor) query.set("cursor", cursor);
        return query;
    }

    function initializeControls() {
        var zone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
        [zone, "UTC", "America/New_York", "Europe/London", "Asia/Taipei"]
            .filter(function (value, index, all) { return all.indexOf(value) === index; })
            .forEach(function (value) { elements["usage-timezone"].appendChild(option(doc, value, value)); });
        var now = new Date(), before = new Date(now.getTime() - 29 * 86400000);
        elements["usage-to"].value = localDay(now, zone);
        elements["usage-from"].value = localDay(before, zone);
    }

    function downloadExport(format) {
        if (state.exporting) {
            elements["usage-status"].textContent = "An export is already being prepared.";
            return;
        }
        state.exporting = true;
        elements["usage-status"].textContent = "Preparing " + format.toUpperCase() + " export…";
        var endpoint = (format === "csv" ? "/v1/orchestrator/usage/analytics.csv?"
                                          : "/v1/orchestrator/usage/analytics.json?")
            + params(null).toString();
        request(endpoint, { credentials: "same-origin", cache: "no-store" }).then(function (response) {
            if (!response.ok) return errorFrom(response);
            return response.blob();
        }).then(function (blob) {
            if (!blob) return;
            var url = URL.createObjectURL(blob), anchor = doc.createElement("a");
            anchor.href = url;
            anchor.download = "clawdline-usage." + format;
            doc.body.appendChild(anchor);
            anchor.click();
            anchor.remove();
            URL.revokeObjectURL(url);
            elements["usage-status"].textContent = "Export downloaded.";
        }).catch(function (error) {
            elements["usage-status"].textContent = error.message;
        }).finally(function () { state.exporting = false; });
    }

    function render(data, append) {
        state.data = data;
        elements["usage-analytics"].removeAttribute("data-stale");
        var availability = data.availability || {}, partial = availability.status === "partial";
        elements["usage-availability"].hidden = !partial;
        if (partial) {
            elements["usage-availability"].textContent = availability.reason === "scan_limit_reached"
                ? "Partial result: more matching rows exist than this bounded query can read. Narrow the dates before relying on totals or export."
                : "Partial result: " + (availability.reason || "unknown reason") + ".";
        }
        renderMeta(context, data);
        if (!append) renderPortfolio(context, data);
        renderRows(context, data, append);
    }

    function load(cursor, append) {
        if (state.loading) {
            state.pending = { cursor: cursor, append: append };
            elements["usage-status"].textContent = append ? "Load more queued…" : "Refresh queued…";
            return;
        }
        state.loading = true;
        elements["usage-status"].textContent = "Reading the local ledger…";
        request("/v1/orchestrator/usage/analytics?" + params(cursor).toString(), {
            credentials: "same-origin", cache: "no-store"
        }).then(function (response) {
            if (!response.ok) return errorFrom(response);
            return response.json();
        }).then(function (body) {
            render(body.usage, append);
            elements["usage-status"].textContent = "";
        }).catch(function (error) {
            elements["usage-status"].textContent = error.message;
            if (!append && state.data) {
                var oldRange = state.data.range || {};
                var oldLabel = (oldRange.from || "unbounded") + "…" + (oldRange.to || "unbounded");
                var requested = (elements["usage-from"].value || "unbounded") + "…"
                    + (elements["usage-to"].value || "unbounded");
                elements["usage-analytics"].setAttribute("data-stale", "true");
                elements["usage-availability"].hidden = false;
                elements["usage-availability"].textContent = "Refresh failed. Showing stale data for "
                    + oldLabel + "; controls currently request " + requested + ".";
            }
        }).finally(function () {
            state.loading = false;
            if (state.pending) {
                var pending = state.pending;
                state.pending = null;
                load(pending.cursor, pending.append);
            }
        });
    }

    function selectView(view) {
        state.view = view;
        var overview = view === "overview";
        elements["usage-overview"].setAttribute("aria-selected", String(overview));
        elements["usage-agent-work"].setAttribute("aria-selected", String(!overview));
        elements["usage-overview"].tabIndex = overview ? 0 : -1;
        elements["usage-agent-work"].tabIndex = overview ? -1 : 0;
        elements["usage-overview-panel"].hidden = !overview;
        elements["usage-agent-work-panel"].hidden = overview;
        load(null, false);
    }

    function open() {
        elements.settings.hidden = true;
        elements["usage-analytics"].hidden = false;
        elements.app.hidden = true;
        elements["usage-open"].setAttribute("aria-expanded", "true");
        doc.body.style.overflow = "hidden";
        elements["usage-close"].focus();
        load(null, false);
    }

    function close() {
        elements["usage-analytics"].hidden = true;
        elements.app.hidden = false;
        elements["usage-open"].setAttribute("aria-expanded", "false");
        doc.body.style.overflow = "";
        elements.brand.focus();
    }

    initializeControls();
    elements["usage-open"].addEventListener("click", open);
    elements["usage-close"].addEventListener("click", close);
    elements["usage-overview"].addEventListener("click", function () { selectView("overview"); });
    elements["usage-agent-work"].addEventListener("click", function () { selectView("agent_work"); });
    [elements["usage-overview"], elements["usage-agent-work"]].forEach(function (tab) {
        tab.addEventListener("keydown", function (event) {
            if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
            event.preventDefault();
            var next = tab === elements["usage-overview"]
                ? elements["usage-agent-work"] : elements["usage-overview"];
            next.focus();
            selectView(next === elements["usage-overview"] ? "overview" : "agent_work");
        });
    });
    elements["usage-controls"].addEventListener("submit", function (event) {
        event.preventDefault();
        load(null, false);
    });
    elements["usage-more"].addEventListener("click", function () {
        if (state.cursor) load(state.cursor, true);
    });
    elements["usage-export-csv"].addEventListener("click", function () { downloadExport("csv"); });
    elements["usage-export-json"].addEventListener("click", function () { downloadExport("json"); });
    elements["usage-detail-close"].addEventListener("click", function () {
        elements["usage-detail"].close();
    });
    doc.addEventListener("keydown", function (event) {
        if (event.key === "Escape" && !elements["usage-analytics"].hidden
            && !elements["usage-detail"].open) close();
    });
    return { load: load, render: render, selectView: selectView };
}
