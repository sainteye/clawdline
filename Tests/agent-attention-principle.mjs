#!/usr/bin/env node
import { readFileSync } from "node:fs";

const root = new URL("../", import.meta.url);
const read = (path) => readFileSync(new URL(path, root), "utf8");
const context = read("CONTEXT.md");
const agreements = read("AGENTS.md");
const dispatching = read("docs/dispatching.md");

const checks = [
  [context.includes("## Attention request"), "CONTEXT.md defines an attention request"],
  [agreements.includes("### Notify before waiting for the user"),
    "AGENTS.md tells agents to notify before waiting"],
  [agreements.includes("POST /v1/orchestrator/notify"),
    "the root notification route is present in the working agreement"],
  [agreements.includes("POST /v1/orchestrator/tasks/:id/notify"),
    "the child notification route is present in the working agreement"],
  [dispatching.includes("## Attention requests are part of the work"),
    "dispatching documents attention as part of delivery"],
  [dispatching.includes("agent_notify_disabled"),
    "the disabled-notification refusal remains explicit"],
  [dispatching.includes("not_subscribed"),
    "the no-subscription fallback remains explicit"],
];

let failed = false;
for (const [condition, message] of checks) {
  if (condition) continue;
  failed = true;
  console.error(`FAIL: ${message}`);
}

console.log(`${failed ? "not ok" : "ok"}: ${checks.length} agent-attention principle checks`);
if (failed) process.exit(1);
