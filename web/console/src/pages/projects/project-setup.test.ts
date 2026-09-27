import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
import { projectSetupCapabilities, projectSetupInstructions, projectSetupProgress } from "./project-setup.ts"

test("a Project setup Session receives the portable guide, all visible layers and the operational boundary", () => {
  const words = projectSetupInstructions({ label: "Sample", path: "/work/sample" })
  assert.match(words, /clawdline guide zh-TW project/)
  assert.match(words, /Project 名稱："Sample"/)
  assert.match(words, /Project root："\/work\/sample"/)
  assert.match(words, /名稱與像素圖示/)
  assert.match(words, /deploy／CI/)
  assert.match(words, /\.devstack\.json/)
  assert.match(words, /不要啟動、停止、重啟或部署/)
  assert.match(words, /仍無法驗證/)
})

test("labels and paths are quoted as data rather than interpolated as new instructions", () => {
  const words = projectSetupInstructions({ label: "line\nbreak", path: "/work/`odd`" })
  assert.match(words, /Project 名稱："line\\nbreak"/)
  assert.match(words, /Project root："\/work\/`odd`"/)
})

test("the setup card gives both unreadable and empty Project lists an actionable next step", () => {
  const source = readFileSync(new URL("./ProjectSetup.tsx", import.meta.url), "utf8")
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
  assert.match(deploy?.detail ?? "", /最近失敗/)
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
