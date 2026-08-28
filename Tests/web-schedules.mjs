import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const elements = {
    schedules: { hidden: false },
    "schedules-count": { textContent: "" },
    "schedule-rows": { innerHTML: "" }
};

globalThis.localStorage = {
    getItem: function () { return null; },
    setItem: function () { }
};
globalThis.location = { search: "", protocol: "http:", hostname: "localhost" };
globalThis.window = {
    devicePixelRatio: 1,
    matchMedia: function () { return { matches: false }; }
};
globalThis.document = {
    documentElement: { lang: "en" },
    getElementById: function (id) {
        return elements[id] || { textContent: "", innerHTML: "" };
    }
};

const { loadScheduleProjects } = await import("../Resources/web/app/js/net/schedules.js");
const { renderSchedules, scheduleRunsHTML, scheduleRunPlace } =
    await import("../Resources/web/app/js/view/schedules.js");

const rows = [
    { id: "shown", title: "Morning brief", enabled: true, next_fire: 200 },
    { id: "unavailable", title: "Unavailable", enabled: true, next_fire: 300 },
    { file: "broken.json", state: "invalid", error: "bad input" }
];
const reads = [];
const projectIcon = { accent: "#d97757", cells: [["#d97757"]] };
const hydrated = await loadScheduleProjects(rows, function (id) {
    reads.push(id);
    if (id === "unavailable") return Promise.reject(new Error("gone"));
    return Promise.resolve({
        schedule: { task: { project_dir: "/Users/you/code/<clawdline>" } }
    });
}, function () {
    return Promise.resolve({ places: [{
        path: "/Users/you/code/<clawdline>", label: "clawdline", icon: projectIcon
    }] });
});

assert.deepEqual(reads, ["shown", "unavailable"], "only valid schedule ids need details");
assert.deepEqual(hydrated[0].project, {
    path: "/Users/you/code/<clawdline>", label: "clawdline", icon: projectIcon
});
assert.equal(hydrated[1], rows[1], "a failed detail read preserves the usable summary");
assert.equal(hydrated[2], rows[2], "invalid rows remain local error rows");

renderSchedules(hydrated, 100);
assert.match(elements["schedule-rows"].innerHTML,
    /<canvas class="schedule-project-mark" aria-hidden="true"><\/canvas><span class="schedule-project-name" title="\/Users\/you\/code\/&lt;clawdline&gt;">clawdline<\/span>/,
    "the schedule row shows the project icon slot and project name, with the path only as its title");
assert.match(elements["schedule-rows"].innerHTML,
    /<span class="schedule-project-name"[^>]*>clawdline<\/span><\/span><span class="schedule-meta-sep" aria-hidden="true"> · <\/span><time class="schedule-next"[^>]*>Next/,
    "the project name shares the metadata line with the next-run time");

const runMarkup = scheduleRunsHTML([
    { task_id: "run-live", state: "briefed", assistant: "codex", created: 300,
      terminal_id: "terminal-live", session_id: "session-live", summary: "still publishing" },
    { task_id: "run-done", state: "success", assistant: "codex", created: 200,
      finished_at: 220, session_id: "session-done", summary: "published <today>",
      project_dir: "/Users/you/code/<blog>" },
    { task_id: "run-lost", state: "failure", assistant: "claude", created: 100 }
], 400, function (terminal) { return terminal === "terminal-live"; });
assert.match(runMarkup, /data-task-id="run-live"[^>]*data-action="open"/,
    "a run whose terminal is still present opens it instead of resuming the transcript twice");
assert.match(runMarkup, /data-task-id="run-done"[^>]*data-action="resume"/,
    "a finished run with a proven conversation id is resumable");
assert.match(runMarkup, /published &lt;today&gt;/,
    "run summaries are escaped before they enter the schedule sheet");
assert.match(runMarkup,
    /<span class="schedule-run-summary" title="published &lt;today&gt;">published &lt;today&gt;<\/span>/,
    "the clamped summary keeps its whole text in the title, escaped the same way");
const scheduleCSS = await readFile(
    new URL("../Resources/web/app/css/schedules.css", import.meta.url), "utf8");
assert.match(scheduleCSS, /\.schedule-run-summary\s*\{[^}]*-webkit-line-clamp:\s*2;/,
    "the picker shows the first lines of a run summary, not the whole report");
assert.match(runMarkup,
    /class="schedule-run-meta" title="\/Users\/you\/code\/&lt;blog&gt;">codex · &lt;blog&gt;<\/span>/,
    "each occurrence names the project it actually used, with the full escaped path available");
assert.match(runMarkup, /data-task-id="run-lost"[^>]*disabled/,
    "a run without a proven conversation stays visible but cannot invent a resume action");
assert.ok(runMarkup.indexOf("run-live") < runMarkup.indexOf("run-done"),
    "the server's newest-first run order is kept");

assert.equal(scheduleRunPlace({ project_dir: "/old/project" }, [
    { id: "current", path: "/new/project" },
    { id: "original", path: "/old/project" }
])?.id, "original", "an old run resumes in the project it actually used, not today's template");

console.log("web schedule tests passed");
process.exit(0);
