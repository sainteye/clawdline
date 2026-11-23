// Run directly with PLAYWRIGHT_MODULE pointing to an installed Playwright.
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { once } from "node:events"
import { mkdtempSync, rmSync, writeFileSync } from "node:fs"
import { basename, join, resolve } from "node:path"
import { test } from "node:test"
import { fileURLToPath } from "node:url"

const web = fileURLToPath(new URL("../../../", import.meta.url))
const consoleRoot = join(web, "console")

test("owners can wake an eligible offline or incompletely reported machine from the fleet on mobile", { timeout: 90_000 }, async () => {
  const { chromium, webkit } = await import(process.env.PLAYWRIGHT_MODULE || "playwright")
  const fixture = mkdtempSync(join(consoleRoot, "fleet-wake-fixture-"))
  let vite
  const browsers = []
  try {
    writeFileSync(join(fixture, "index.html"), '<meta name="viewport" content="width=device-width,initial-scale=1"><div id="root"></div><script type="module" src="./entry.tsx"></script>')
    writeFileSync(join(fixture, "entry.tsx"), `import React, {useCallback, useState} from "react";
import {createRoot} from "react-dom/client";
import {FleetSessionList} from "../src/cloud/FleetSessionList";
import {CloudReadProblem} from "../src/cloud/CloudReadProblem";
import {bootCatalog} from "../src/catalog";
import {loadStrings} from "../src/legacy/bridge";
import "../src/legacy/tokens.css";
import "../src/legacy/base.css";
import "../src/cloud/cloud.css";
localStorage.setItem("ui_language", new URLSearchParams(location.search).get("lang"));
await bootCatalog(tag => "/catalogs/" + tag + ".json");
await loadStrings(async () => ({}));
const root = createRoot(document.getElementById("root"));
const noop = () => {};
const owner = id => ({ id, name: id, platform: "Linux", freshness: "stale", instaBoxWakeOrigin: location.origin });
let reconnected = false;
const source = {
  subscribe: () => noop,
  readMachine: async id => {
    if (id === "relay-offline") return { kind: "unavailable", reason: "offline" };
    if (id === "incomplete") return { kind: "ready", complete: reconnected, observedAt: Date.now(), rows: [] };
    return new Promise(() => {});
  },
};
function FleetFixture({online, problem}) {
  const [wakeMachines, setWakeMachines] = useState(new Set());
  const onWakeAvailability = useCallback((id, ready) => setWakeMachines(before => {
    const next = new Set(before);
    if (ready) next.add(id);
    else next.delete(id);
    return next;
  }), []);
  return <>
    <CloudReadProblem message={problem.machine + " read failed"} code={problem.code}
      retryLabel="Retry" dismissLabel="Dismiss" onRetry={noop}
      fleetWakeAvailable={wakeMachines.has(problem.machine)} />
    <FleetSessionList onWakeAvailability={onWakeAvailability} machines={[
      { ...owner("eligible"), freshness: online ? "current" : "unknown" },
      { ...owner("relay-offline"), freshness: "current" },
      { ...owner("incomplete"), freshness: "current" },
      owner("unbound"), owner("no-key"), owner("denied"),
      { ...owner("online"), freshness: "current" },
      { ...owner("guest"), instaBoxWakeOrigin: undefined },
    ]} source={source} target={null} filter="" onFilter={noop} onOpen={noop}
      onConnection={noop} onCloseRequest={noop} onArchiveRequest={noop} />
  </>;
}
let problem = {machine: "eligible", code: "cloud_read_timeout"};
function render(online = reconnected) {
  reconnected = online;
  root.render(<FleetFixture online={online} problem={problem} />);
}
window.failRead = (machine, code) => { problem = { machine, code }; render(); };
window.reconnect = () => render(true);
render();`)
    vite = spawn(process.execPath, [resolve(web, "node_modules/vite/bin/vite.js"), "--host", "127.0.0.1", "--port", "0"],
      { cwd: consoleRoot, stdio: ["ignore", "pipe", "pipe"] })
    const origin = await new Promise((resolveOrigin, reject) => {
      let output = ""
      const timeout = setTimeout(() => reject(new Error("Vite startup timed out")), 20_000)
      vite.stdout.on("data", data => {
        output += data
        const match = output.match(/http:\/\/127\.0\.0\.1:\d+/)
        if (match) { clearTimeout(timeout); resolveOrigin(match[0]) }
      })
      vite.stderr.on("data", data => process.stderr.write(data))
      vite.once("exit", code => { clearTimeout(timeout); reject(new Error("Vite exited: " + code)) })
    })
    for (const [engine, lang, wakeLabel, wakingLabel, accepted, cooldown, unconfirmed, offline] of [
      [chromium, "en", "Wake machine", "Waking the machine…", "InstaBox accepted the request.", "InstaBox is in cooldown.", "Not reported yet", "Offline"],
      [webkit, "zh-Hant", "喚醒機器", "正在喚醒機器……", "InstaBox 已接受請求", "InstaBox 正在冷卻", "尚未回報", "離線"],
    ]) {
      const browser = await engine.launch({ headless: true, ...(engine === chromium ? {
        executablePath: process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
        args: ["--disable-features=OmniboxEverywhere"],
      } : {}) })
      browsers.push(browser)
      const page = await browser.newPage({ viewport: { width: 368, height: 800 }, hasTouch: true, isMobile: true })
      const lookups = []
      const posts = []
      let startedWake
      let completeWake
      const wakeStarted = new Promise(resolveStart => { startedWake = resolveStart })
      await page.route("**/v1/**", route => route.fulfill({ status: 404, json: {} }))
      await page.route(/\/v1\/machines\/[^/]+\/instabox-wake(?:\/wake)?$/, async route => {
        const [, id, action] = new URL(route.request().url()).pathname.match(/\/machines\/([^/]+)\/instabox-wake(\/wake)?$/)
        if (!action) {
          assert.equal(route.request().method(), "GET")
          lookups.push(id)
          if (id === "denied") return route.fulfill({ status: 404, json: { error: { code: "unknown_machine" } } })
          return route.fulfill({ json: { configured: id !== "unbound", credential_ready: id !== "no-key" } })
        }
        assert.equal(route.request().method(), "POST")
        posts.push(id)
        if (posts.length === 1) {
          await new Promise(resolvePost => { completeWake = resolvePost; startedWake() })
          return route.fulfill({ status: 409, json: { error: { code: "wake_cooldown" } } })
        }
        return route.fulfill({ json: { wake: "accepted" } })
      })
      const checked = ["eligible", "relay-offline", "unbound", "no-key", "denied"].map(id =>
        page.waitForResponse(response => response.url().endsWith(`/machines/${id}/instabox-wake`), { timeout: 8000 }))
      await page.goto(`${origin}/${basename(fixture)}/?lang=${lang}`)
      await Promise.all(checked)
      const eligible = page.getByRole("region", { name: /^eligible eligible/ })
      const disconnected = page.getByRole("region", { name: /^relay-offline relay-offline/ })
      const incomplete = page.getByRole("region", { name: /^incomplete incomplete/ })
      const wake = eligible.getByRole("button", { name: wakeLabel, exact: true })
      const wakeIncomplete = incomplete.getByRole("button", { name: wakeLabel, exact: true })
      await wake.waitFor()
      await eligible.getByRole("status").filter({ hasText: unconfirmed }).waitFor()
      await disconnected.getByRole("status").filter({ hasText: offline }).waitFor()
      await wakeIncomplete.waitFor({ timeout: 5000 })
      const topWarning = page.locator(".cloud-read-problem")
      await topWarning.waitFor({ state: "hidden", timeout: 3000 })
      for (const [id, code] of [
        ["unbound", "cloud_read_timeout"], ["no-key", "cloud_read_timeout"],
        ["online", "cloud_read_timeout"], ["eligible", "machine_pairing_required"],
      ]) {
        await page.evaluate(([machine, code]) => window.failRead(machine, code), [id, code])
        await topWarning.getByText(id + " read failed", { exact: true }).waitFor()
      }
      await page.evaluate(() => window.failRead("eligible", "cloud_read_timeout"))
      await topWarning.waitFor({ state: "hidden" })
      assert.equal(await incomplete.getByRole("status").filter({ hasText: offline }).count(), 0)
      for (const group of [eligible, disconnected]) {
        assert.equal(await group.locator('[data-loading-id="SK-02"]').count(), 0)
      }
      assert.equal(await disconnected.getByRole("button", { name: wakeLabel, exact: true }).isVisible(), true)
      assert.equal(await page.getByRole("button", { name: wakeLabel, exact: true }).count(), 3)
      assert.deepEqual(lookups.sort(), ["denied", "eligible", "incomplete", "no-key", "relay-offline", "unbound"])
      assert.deepEqual(posts, [])
      const box = await wake.boundingBox()
      assert.ok(box.height >= 44 && box.x + box.width <= 368, JSON.stringify(box))
      await eligible.locator('button[aria-expanded="true"]').click()
      assert.equal(await wake.isVisible(), true)
      await wakeIncomplete.tap()
      const waking = incomplete.getByRole("button", { name: wakingLabel, exact: true })
      await waking.waitFor()
      assert.equal(await waking.isDisabled(), true)
      await wakeStarted
      await waking.evaluate(button => button.click())
      assert.deepEqual(posts, ["incomplete"])
      completeWake()
      await page.getByRole("alert").filter({ hasText: cooldown }).waitFor()
      await wakeIncomplete.tap()
      await page.getByRole("status").filter({ hasText: accepted }).waitFor()
      assert.deepEqual(posts, ["incomplete", "incomplete"])
      await page.evaluate(() => window.reconnect())
      await wake.waitFor({ state: "detached" })
      await topWarning.getByText("eligible read failed", { exact: true }).waitFor()
      await wakeIncomplete.waitFor({ state: "detached" })
      await page.getByText(accepted, { exact: false }).waitFor({ state: "detached" })
      await page.close()
    }
  } finally {
    for (const browser of browsers) await browser.close()
    if (vite?.exitCode === null) { vite.kill(); await once(vite, "exit") }
    rmSync(fixture, { recursive: true, force: true })
  }
})
