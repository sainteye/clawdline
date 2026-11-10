import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node runs source tests with type stripping.
import { compactValue, dateRange, comparisonComplete, dailyValue, lineSegments, percentageChange, sampleLabel, selectedDay, shortDate, UsageRequestScope } from "./model.ts"
// @ts-expect-error -- Node runs source tests with type stripping.
import { readUsageOverview, UsageReadError, type UsageOverview, type UsageOverviewDay } from "./api.ts"

const day: UsageOverviewDay = { date: "2026-10-12", tokens: 1200, dispatches: 2, duration_median_seconds: 90, duration_samples: 3, coverage: "complete" }

test("daily elapsed values display the returned successful-task median in minutes", () => {
  assert.equal(dailyValue(day, "duration"), 1.5)
  assert.equal(dailyValue({ ...day, duration_samples: 0 }, "duration"), null)
  assert.equal(dailyValue({ ...day, duration_median_seconds: null }, "duration"), null)
  assert.equal(dailyValue({ ...day, tokens: null }, "tokens"), null)
  assert.equal(dailyValue({ ...day, tokens: null }, "dispatches"), 2)
})

test("missing history splits SVG paths while actual zero remains on the baseline", () => {
  const paths = lineSegments([4, 0, null, 3], 6, 378)
  assert.equal(paths.length, 2)
  assert.equal(paths[0], "M54.00,74.00 L154.00,174.00")
  assert.equal(paths[1], "M354.00,99.00")
  assert.deepEqual(lineSegments([null, null], 1, 378), [])
})

test("comparisons require both complete periods and reject a zero denominator", () => {
  const data = { days: 7, daily: Array(7).fill(day), previous_daily: Array(7).fill(day), coverage: { state: "complete", truncated: false } } as UsageOverview
  assert.equal(comparisonComplete(data), true)
  assert.equal(percentageChange(12, 10, comparisonComplete(data)), "+20%")
  for (const previous of [[], Array(7).fill({ ...day, coverage: "partial" }), Array(7).fill({ ...day, coverage: "missing" })]) {
    assert.equal(comparisonComplete({ ...data, previous_daily: previous }), false)
  }
  assert.equal(percentageChange(12, 10, false), null)
  assert.equal(percentageChange(12, 0, true), null)
  assert.equal(percentageChange(null, 10, true), null)
  assert.equal(comparisonComplete({ ...data, coverage: { ...data.coverage, truncated: true } }), false)
})

test("chart keyboard selection reaches and clamps both ends", () => {
  assert.equal(selectedDay(0, "ArrowLeft", 7), 0)
  assert.equal(selectedDay(0, "ArrowRight", 7), 1)
  assert.equal(selectedDay(2, "End", 28), 27)
  assert.equal(selectedDay(27, "ArrowRight", 28), 27)
  assert.equal(selectedDay(27, "Home", 28), 0)
  assert.equal(shortDate("2026-10-12", "en"), "10/12")
  assert.equal(compactValue(null), "—")
  assert.equal(compactValue(1_200_000), "1.2M")
})

test("period, project and machine invalidations reject late responses even without transport cancellation", async () => {
  const scope = new UsageRequestScope()
  const first = scope.begin()
  const published: string[] = []
  let deliverOld: (value: string) => void = () => {}
  const oldReply = new Promise<string>(resolve => { deliverOld = resolve }).then(value => {
    if (scope.current(first)) published.push(value)
  })
  const second = scope.begin()
  assert.equal(first.aborted, true)
  assert.equal(scope.current(first), false)
  assert.equal(scope.current(second), true)
  scope.cancel()
  assert.equal(scope.current(second), false)
  const third = scope.begin()
  assert.equal(scope.current(third), true)
  if (scope.current(third)) published.push("new machine and filter")
  deliverOld("old machine and filter")
  await oldReply
  assert.deepEqual(published, ["new machine and filter"])
})

test("overview fetch carries real filters, caller cancellation and flat offline refusals", async () => {
  const original = globalThis.fetch
  const controller = new AbortController()
  let carriedSignal: AbortSignal | null | undefined
  try {
    globalThis.fetch = async (input, init) => {
      assert.equal(String(input), "/v1/usage/overview?days=28&project=opaque%2Fproject%26one")
      carriedSignal = init?.signal
      return new Response(JSON.stringify({ error: "machine_offline" }), { status: 503 })
    }
    await assert.rejects(readUsageOverview(28, "opaque/project&one", controller.signal), (error: unknown) => error instanceof UsageReadError && error.code === "machine_offline")
    controller.abort()
    assert.equal(carriedSignal?.aborted, true)
    globalThis.fetch = async () => new Response(JSON.stringify({ daily: null, previous_daily: null, project_options: null, projects: null }))
    const result = await readUsageOverview(7, "", new AbortController().signal)
    assert.deepEqual(result.project_options, [])
    assert.deepEqual(result.daily, [])
  } finally { globalThis.fetch = original }
})

test("large totals use billions and calendar ranges preserve year boundaries", () => {
 assert.equal(compactValue(2_889_100_000), "2.9B")
 assert.equal(compactValue(1_000_000_000), "1B")
 assert.equal(compactValue(950_000_000), "950M")
 assert.equal(dateRange("2026-10-12", "2026-10-18", "en"), "Oct 12, 2026 – Oct 18, 2026")
 assert.equal(dateRange("2026-12-25", "2027-01-01", "en"), "Dec 25, 2026 – Jan 1, 2027")
})

test("calendar labels use the selected interface locale and stay fixed across viewer time zones", () => {
  const savedZone = process.env.TZ
  try {
    for (const tag of ["en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"]) {
      let labels: [string, string, string] | null = null
      for (const zone of ["Pacific/Honolulu", "Pacific/Auckland"]) {
        process.env.TZ = zone
        const current: [string, string, string] = [shortDate("2026-01-09", tag),
          dateRange("2026-01-09", "2026-01-15", tag), dateRange("2026-12-25", "2027-01-01", tag)]
        assert.deepEqual(current, labels ?? current, `${tag} changed in ${zone}`)
        labels = current
      }
    }
    assert.equal(shortDate("2026-01-09", "de"), "9.1.")
    assert.match(dateRange("2026-12-25", "2027-01-01", "de"), /2026.*2027/u)
    assert.equal(shortDate("2026-01-09", "en"), "1/9")
  } finally {
    if (savedZone === undefined) delete process.env.TZ
    else process.env.TZ = savedZone
  }
})

test("sample labels select the locale's singular form without changing CJK wording", () => {
  assert.equal(sampleLabel(0, "de", "{count} Stichprobe", "{count} Stichproben"), "0 Stichproben")
  assert.equal(sampleLabel(1, "de", "{count} Stichprobe", "{count} Stichproben"), "1 Stichprobe")
  assert.equal(sampleLabel(2, "de", "{count} Stichprobe", "{count} Stichproben"), "2 Stichproben")
  assert.equal(sampleLabel(1, "en", "{count} sample", "{count} samples"), "1 sample")
  assert.equal(sampleLabel(1, "fr", "{count} échantillon", "{count} échantillons"), "1 échantillon")
  assert.equal(sampleLabel(1, "ja", "{count} サンプル", "{count} サンプル"), "1 サンプル")
})

// @ts-expect-error -- Node runs source tests with type stripping.
import { usageProjectOptions } from "./project-options.ts"
// @ts-expect-error -- Node runs source tests with type stripping.
import { sha256Hex } from "../../session/fingerprint.ts"

test("usage project choices come from configured projects rather than Session history", () => {
  const id = sha256Hex(new TextEncoder().encode("/projects/configured"))
  const options = [{ id: "session-only", name: "temporary-session" }, { id, name: "old-directory-name" }, { id: "unattributed", name: "unattributed" }]
  const places = [{ path: "/projects/configured", label: "Configured project", icon: { svg: "project-mark" } }]
  assert.deepEqual(usageProjectOptions(options, places), [{ id, label: "Configured project", icon: places[0].icon }])
  assert.deepEqual(usageProjectOptions(options, []), [])
  assert.deepEqual(usageProjectOptions([], places), [])
})
