import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { projectSetupCapabilities, projectSetupInstructions, projectSetupProgress } from "./project-setup.ts"

test("a Project setup Session receives the portable guide, all visible layers and the operational boundary", () => {
  const words = projectSetupInstructions({ label: "Sample", path: "/work/sample" })
  assert.match(words, /clawdline guide zh-TW project/)
  assert.match(words, /Project name: "Sample"/)
  assert.match(words, /Project root: "\/work\/sample"/)
  assert.match(words, /name and pixel icon/)
  assert.match(words, /deploy\/CI/)
  assert.match(words, /\.devstack\.json/)
  assert.match(words, /do not start, stop, restart, or deploy/)
  assert.match(words, /could not be verified/)
})

test("labels and paths are quoted as data rather than interpolated as new instructions", () => {
  const words = projectSetupInstructions({ label: "line\nbreak", path: "/work/`odd`" })
  assert.match(words, /Project name: "line\\nbreak"/)
  assert.match(words, /Project root: "\/work\/`odd`"/)
})

test("the setup dialog gives both unreadable and empty Project lists an actionable next step", () => {
  const source = readFileSync(new URL("./ProjectSetup.tsx", import.meta.url), "utf8")
  assert.match(source, /showModal\(\)/)
  assert.match(source, /aria-haspopup="dialog"/)
  assert.match(source, /focus\(\{ preventScroll: true \}\)/)
  assert.match(source, /role="alert"/)
  assert.match(source, /重新讀取/)
  assert.match(source, /clawdline project add/)
})

test("the readiness model separates configured deploy progress from a failed current run", () => {
  const place = {
    id: "shop", label: "Shop", path: "/work/shop", repo: "github.com/team/shop",
    setup: {
      icon: "override" as const,
      deploy: "ready" as const,
      deploy_activity: "failed" as const,
      servers: "missing" as const,
      server_count: 0,
      sync: "ready" as const,
    },
  }
  const rows = projectSetupCapabilities(place)
  const deploy = rows.find(row => row.key === "deploy")
  assert.equal(deploy?.complete, true, "the producer is configured")
  assert.equal(deploy?.tone, "attention", "the current failure still asks for attention")
  assert.match(deploy?.detail ?? "", /Recently failed/)
  assert.deepEqual(projectSetupProgress(place), { complete: 3, total: 4 })
})

test("non-GitHub deploy is explained and excluded from the completion denominator", () => {
  const place = {
    id: "shop", label: "Shop", path: "/work/shop", repo: "git.example.com/team/shop",
    setup: {
      icon: "registry" as const,
      deploy: "not_applicable" as const,
      deploy_activity: "unknown" as const,
      servers: "ready" as const,
      server_count: 2,
      sync: "ready" as const,
    },
  }
  assert.deepEqual(projectSetupProgress(place), { complete: 3, total: 3 })
  assert.equal(projectSetupCapabilities(place).find(row => row.key === "deploy")?.tone, "not-applicable")
})

test("an older daemon is shown as unknown instead of making up missing configuration", () => {
  const place = { id: "shop", label: "Shop", path: "/work/shop" }
  assert.equal(projectSetupProgress(place), null)
  assert.equal(projectSetupCapabilities(place)[0]?.tone, "unknown")
})

const base = {
  icon: "override" as const, deploy: "not_applicable" as const, deploy_activity: "unknown" as const,
  servers: "ready" as const, server_count: 1, sync: "ready" as const,
}

test("the Claude and Codex sharing row distinguishes shared, drifting, and unknown", () => {
  const row = (setup: object) => projectSetupCapabilities({ id: "shop", label: "Shop", path: "/work/shop", setup: setup as any })
    .find(capability => capability.key === "unify")
  const unified = row({ ...base, unify: "unified" })
  assert.equal(unified?.label, "Claude and Codex share rules and skills")
  assert.equal(unified?.detail, "Shared")
  assert.equal(unified?.tone, "ready")
  assert.equal(unified?.action, "unify")
  const drifting = row({ ...base, unify: "drifting", unify_count: 3 })
  assert.equal(drifting?.detail, "Differences found (3)")
  assert.equal(drifting?.tone, "attention")
  assert.equal(drifting?.complete, false)
  assert.equal(drifting?.action, "unify")
  const unknown = row({ ...base, unify: "unknown" })
  assert.match(unknown?.detail ?? "", /^Unknown/)
  assert.equal(unknown?.tone, "unknown")
  assert.equal(unknown?.action, "unify", "the preview says what could not be read")
  assert.deepEqual(projectSetupProgress({ id: "shop", label: "Shop", path: "/work/shop", setup: { ...base, unify: "drifting", unify_count: 3 } as any }),
    { complete: 3, total: 4 }, "drifting counts against the score")
})

test("a machine that sends no unify status shows unknown without a zero count", () => {
  const place = { id: "shop", label: "Shop", path: "/work/shop", setup: base }
  const row = projectSetupCapabilities(place).find(capability => capability.key === "unify")
  assert.match(row?.detail ?? "", /^Unknown/)
  assert.doesNotMatch(row?.detail ?? "", /0/)
  assert.equal(row?.tone, "unknown")
  assert.equal(row?.applicable, false)
  assert.equal(row?.action, undefined, "an older machine has no unify route to open")
  assert.deepEqual(projectSetupProgress(place), { complete: 3, total: 3 })
})

test("the readiness card's unify row opens the scoped Project view and its unify preview", () => {
  const source = readFileSync(new URL("./ProjectSetup.tsx", import.meta.url), "utf8")
  assert.match(source, /row\.action === "unify"/)
  assert.match(source, /openUnify\(place\)/)
  assert.match(source, /<ProjectUnify [^>]*openRequest=/)
})
