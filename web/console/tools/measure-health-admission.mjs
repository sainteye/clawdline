#!/usr/bin/env node
// Synthetic overlap only: no network, authentication, database or production traffic.
// node --experimental-strip-types measure-health-admission.mjs BEFORE_ROOT AFTER_ROOT [RUNS]
import { pathToFileURL } from "node:url"
import { resolve } from "node:path"
import { performance } from "node:perf_hooks"

const [before, after, count = "20"] = process.argv.slice(2)
const runs = Number(count)
if (!before || !after || !Number.isSafeInteger(runs) || runs < 1) throw new Error("expected BEFORE_ROOT AFTER_ROOT [positive RUNS]")
const relative = "web/console/src/cloud/admin/health/"
const load = async root => ({
  cloud: await import(pathToFileURL(resolve(root, relative, "cloud.ts")).href),
  attention: await import(pathToFileURL(resolve(root, relative, "attention.ts")).href),
})
const arms = { before: await load(before), after: await load(after) }
const { fixtureAnswer } = await import(pathToFileURL(resolve(after, relative, "fixture.ts")).href)
const now = Date.parse("2026-10-20T12:00:00Z")
const params = { from: "2026-10-20T11:00:00.000Z", to: new Date(now).toISOString() }
const reads = ["overview", "outcomes", "incidents"].map(part => ({ part, path: part, params }))
const samples = { before: [], after: [] }
const trial = async arm => {
  let active = 0, peak = 0, refused = 0, requests = 0
  const get = async url => {
    requests++
    if (active === 2) { refused++; return Response.json({ error: { code: "health_busy" } }, { status: 429 }) }
    active++; peak = Math.max(peak, active)
    try {
      await new Promise(resolve => setTimeout(resolve, 20))
      return url.endsWith("/attention") ? Response.json({ schema: "clawdline.health_attention.v1", status: "ok", items: [], complete: true }) : fixtureAnswer("normal", url, now)
    } finally { active-- }
  }
  const start = performance.now()
  const [page, attention] = await Promise.all([
    arm.cloud.runReads(get, "https://measurement.example.invalid", reads, new AbortController().signal),
    arm.attention.readAttention("https://measurement.example.invalid", get),
  ])
  const successes = (page.kind === "parts" ? Object.values(page.parts).filter(p => p.ok).length : 0) + (attention.state === "ok" ? 1 : 0)
  return { settleMs: performance.now() - start, requests, successes, refused, peak, complete: successes === 4 }
}
// Three warmups per arm, then alternating order to reduce order bias.
for (let n = 0; n < 3; n++) for (const arm of Object.values(arms)) await trial(arm)
for (let n = 0; n < runs; n++) for (const name of n % 2 ? ["after", "before"] : ["before", "after"]) samples[name].push(await trial(arms[name]))
const percentile = (rows, p) => [...rows].sort((a, b) => a - b)[Math.ceil(rows.length * p) - 1]
const summary = Object.fromEntries(Object.entries(samples).map(([name, rows]) => [name, {
  runs: rows.length, requests: rows.reduce((n, row) => n + row.requests, 0),
  successes: rows.reduce((n, row) => n + row.successes, 0), refusals: rows.reduce((n, row) => n + row.refused, 0),
  complete: rows.filter(row => row.complete).length, peak: Math.max(...rows.map(row => row.peak)),
  settledMedianMs: percentile(rows.map(row => row.settleMs), .5), settledP95Ms: percentile(rows.map(row => row.settleMs), .95),
}]))
console.log(JSON.stringify({ schema: 1, node: process.version, platform: process.platform, arch: process.arch, workload: "three page reads plus attention, two server slots, fixed 20ms synthetic work per accepted read", warmups: 3, samples, summary }, null, 2))
