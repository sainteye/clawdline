// What a row says about a machine this browser cannot read, against a fake
// account: `node --test web/console/src/cloud/unpaired-rows.test.ts`.
//
// The fake answers what the control plane's `GET /v1/machines` answers
// (`MachineRecord`, `internal/adapters/cloud/pairing.go`): ids, names,
// platforms, key fingerprints and a revocation stamp.
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
// @ts-expect-error -- a `.ts` path, for node; see cloud/forget.test.ts.
import { accountMachineNames, accountMachineRoster, machineIdentityFacts, sessionsFact, withAccountNames } from "./unpaired-rows.ts"

const API = "https://api.example.test"

function account(answer: { status: number; body?: unknown; throws?: boolean }) {
  const asked: Array<{ url: string; credentials?: string }> = []
  const get = (url: string, init?: RequestInit) => {
    asked.push({ url, credentials: init?.credentials })
    if (answer.throws) return Promise.reject(new TypeError("Failed to fetch"))
    return Promise.resolve({ status: answer.status, json: () => Promise.resolve(answer.body) })
  }
  return { asked, get }
}

const LISTED = {
  machines: [
    { id: "mac_1", name: "Studio", platform: "darwin", key_fingerprint: "AAAA", key_epoch: 1, identity_epoch: 1, revoked_at: null },
    { id: "mac_51463f04", name: "build-box", platform: "linux", key_fingerprint: "BBBB", key_epoch: 1, identity_epoch: 1, revoked_at: null },
    { id: "mac_old", name: "gone", platform: "linux", key_fingerprint: "CCCC", key_epoch: 1, identity_epoch: 1, revoked_at: "2026-09-20T00:00:00Z" },
    { id: "mac_blank", name: "  ", platform: "linux", key_fingerprint: "DDDD", key_epoch: 1, identity_epoch: 1, revoked_at: null },
  ],
}

test("the account's names are read with this browser's own cookie, revoked and blank ones left out", async () => {
  const { asked, get } = account({ status: 200, body: LISTED })
  const names = await accountMachineNames(API, get)
  assert.deepEqual(asked, [{ url: API + "/v1/machines", credentials: "include" }])
  assert.deepEqual([...names.entries()], [
    ["mac_1", { name: "Studio", platform: "darwin" }],
    ["mac_51463f04", { name: "build-box", platform: "linux" }],
  ])
})

test("the account roster keeps revoked ids so a retained relay snapshot cannot draw them again", async () => {
  const { get } = account({ status: 200, body: LISTED })
  const roster = await accountMachineRoster(API, get)
  assert.ok(roster)
  assert.deepEqual([...roster.revoked], ["mac_old"])
  assert.equal(roster.names.has("mac_old"), false)
})

test("an account that will not say leaves no names, and no error for the list to trip on", async () => {
  for (const answer of [{ status: 401, body: {} }, { status: 200, body: { nope: 1 } }, { status: 200, throws: true }]) {
    const { get } = account(answer)
    assert.equal((await accountMachineNames(API, get)).size, 0)
  }
})

const present = (id: string, name: string, platform: string) => ({
  name,
  label: (platform === "linux" ? "Linux" : "Desk") + " · " + name,
  kind: platform === "linux" ? "linux" : "desk",
})

test("a machine this browser cannot name is called what the account calls it; one it can name keeps its own", () => {
  const rows = [
    { id: "mac_1", name: "Studio (published)", label: "Desk · Studio (published)", kind: "desk" },
    { id: "mac_51463f04", name: "Machine · 51463f04", label: "Machine · 51463f04", kind: "unknown" },
    { id: "mac_unlisted", name: "Machine · unlisted", label: "Machine · unlisted", kind: "unknown" },
  ]
  const names = new Map([
    ["mac_1", { name: "Studio", platform: "darwin" }],
    ["mac_51463f04", { name: "build-box", platform: "linux" }],
  ])
  const shown = withAccountNames(rows, names, (id: string) => id === "mac_1", present)
  assert.deepEqual(
    shown.map((row: { label: string }) => row.label),
    ["Desk · Studio (published)", "Linux · build-box", "Machine · unlisted"],
  )
  assert.equal(shown[1].kind, "linux")
})

test("a count is only said where the sessions could be read", () => {
  assert.deepEqual(sessionsFact({ pairing: "paired", sessions: 0 }), { count: 0 })
  assert.deepEqual(sessionsFact({ pairing: "paired", sessions: 11 }), { count: 11 })
  assert.equal(sessionsFact({ pairing: "not_paired", sessions: 0 }), "unread")
  assert.equal(sessionsFact({ pairing: "unknown", sessions: 0 }), "unknown")
  // Sessions this browser did open are a count whatever the pairing lookup said.
  assert.deepEqual(sessionsFact({ pairing: "unknown", sessions: 3 }), { count: 3 })
})

test("a row always has a stable id fragment and says only the identity facts it knows", () => {
  assert.deepEqual(
    machineIdentityFacts({ id: "mac_51463f04", kind: "linux", observedAt: Date.UTC(2026, 8, 19) }),
    { shortID: "51463f04", platform: "linux", seenAt: Date.UTC(2026, 8, 19) },
  )
  assert.deepEqual(machineIdentityFacts({ id: "short", kind: "unknown", observedAt: null }), {
    shortID: "short",
    platform: "unknown",
    seenAt: null,
  })
  assert.deepEqual(machineIdentityFacts({ id: "mac_bad", observedAt: Number.NaN }), {
    shortID: "mac_bad",
    platform: "unknown",
    seenAt: null,
  })
})

test("pairing is one action in the row's action group, and opens the two-path guide", () => {
  const here = dirname(fileURLToPath(import.meta.url))
  const gate = readFileSync(resolve(here, "CloudGate.tsx"), "utf8")
  const panel = readFileSync(resolve(here, "PairPanel.tsx"), "utf8")
  const css = readFileSync(resolve(here, "cloud.css"), "utf8")
  assert.match(gate, /className="cloud-machine-actions"/)
  assert.match(gate, /aria-label=\{nextWord\("cloudMachineActions"/)
  assert.match(panel, /className="cloud-pair-ways"/)
  assert.match(panel, /cloudPairFromMachineTitle/)
  assert.match(panel, /cloudPairFromBrowserTitle/)
  assert.match(panel, /nextWord\("cloudPairAgentPrompt", \{ machine: target, command: line \}\)/)
  assert.match(panel, /copy\(agentPrompt\)/)
  assert.doesNotMatch(css, /\.cloud-machines \.cloud-pair \{\s*display: block; width: 100%/)
})

test("the header switches machines in place and leaves account actions on the full machine screen", () => {
  const here = dirname(fileURLToPath(import.meta.url))
  const gate = readFileSync(resolve(here, "CloudGate.tsx"), "utf8")
  const css = readFileSync(resolve(here, "cloud.css"), "utf8")

  assert.match(gate, /aria-controls="cloud-quick-machines"/)
  assert.match(gate, /className="cloud-switch-menu"/)
  assert.match(gate, /quickMachines\.map/)
  assert.match(gate, /onClick=\{\(\) => switchMachine\(machine\)\}/)
  assert.match(gate, /className="cloud-switch-manage"/)
  assert.match(css, /\.cloud-switch-menu \{/)
  assert.match(css, /\.cloud-switch-option\[data-current="true"\]/)
})
