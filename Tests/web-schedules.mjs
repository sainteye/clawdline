import assert from "node:assert/strict";

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
const { renderSchedules } = await import("../Resources/web/app/js/view/schedules.js");

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

console.log("web schedule tests passed");
process.exit(0);
