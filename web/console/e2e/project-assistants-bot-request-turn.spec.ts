/** Run with node --test and an existing PLAYWRIGHT_MODULE. CHROME and BOTS_ARTIFACTS are optional. */
import assert from "node:assert/strict"
import {mkdir, readFile} from "node:fs/promises"
import {createServer} from "node:http"
import path from "node:path"
import {test} from "node:test"
import {fileURLToPath, pathToFileURL} from "node:url"
import {build} from "esbuild"

const consoleRoot = fileURLToPath(new URL("../", import.meta.url))
const artifacts = process.env.BOTS_ARTIFACTS
assert.ok(process.env.PLAYWRIGHT_MODULE, "Set PLAYWRIGHT_MODULE to an existing Playwright entry")
const {chromium} = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const bundle = await build({
  entryPoints: [path.join(consoleRoot, "src/project-assistants/fixtures.ts")],
  outfile: "project-assistants-fixture.js", write: false, bundle: true, format: "esm",
  platform: "browser", jsx: "automatic", define: {"process.env.NODE_ENV": '"development"'},
})
const files = new Map(bundle.outputFiles.map(file => [
  "/" + path.basename(file.path), [file.contents, file.path.endsWith(".css") ? "text/css" : "text/javascript"] as const,
]))
files.set("/", [await readFile(path.join(consoleRoot, "tools/fixtures/project-assistants.html")), "text/html"])
for (const tag of ["en", "zh-Hant"]) {
  files.set(`/catalog/${tag}.json`, [await readFile(path.join(consoleRoot, `public/catalogs/${tag}.json`)), "application/json"])
}
const server = createServer((req, res) => {
  const file = files.get(new URL(req.url ?? "/", "http://fixture").pathname)
  if (!file) { res.writeHead(404).end(); return }
  res.writeHead(200, {"Content-Type": file[1]}).end(file[0])
})

test("Bot-request reply progress appears once and hides the previous completion", async t => {
  await new Promise<void>((resolve, reject) => server.once("error", reject).listen(0, "127.0.0.1", resolve))
  t.after(() => new Promise<void>(resolve => server.close(() => resolve())))
  const address = server.address()
  assert.ok(address && typeof address !== "string")
  const origin = `http://127.0.0.1:${address.port}`
  const browser = await chromium.launch({
    headless: true, executablePath: process.env.CHROME, args: ["--disable-features=OmniboxEverywhere"],
  })
  t.after(() => browser.close())
  if (artifacts) await mkdir(artifacts, {recursive: true})

  for (const [width, height] of [[1280, 800], [390, 844]]) {
    await t.test(`${width}×${height} incoming request in a background thread`, async () => {
      const mobile = width === 390
      const context = await browser.newContext({
        viewport: {width, height}, isMobile: mobile, hasTouch: mobile, deviceScaleFactor: 1,
        locale: "zh-TW", colorScheme: "dark", reducedMotion: "reduce",
      })
      try {
        const page = await context.newPage()
        page.setDefaultTimeout(5000)
        const errors: string[] = []
        page.on("pageerror", error => errors.push(error.message))
        if (mobile) {
          const cdp = await context.newCDPSession(page)
          await cdp.send("Emulation.setSafeAreaInsetsOverride", {
            insets: {top: 47, bottom: 34, topMax: 47, bottomMax: 34, left: 0, right: 0},
          })
        }
        await page.clock.setFixedTime(new Date("2026-11-12T06:23:56Z"))
        await page.addInitScript(() => localStorage.setItem("ui_language", "zh-Hant"))
        for (const scenario of ["running", "without-card", "idle"]) {
          await page.goto(origin + "/?exchange-layout=review&lang=zh-Hant")
          await page.addStyleTag({content: "#voice-owner { display: none }"})
          await page.locator("#voice-owner").waitFor({state: "attached"})
          await page.evaluate(scenario => {
            const {a: source, open} = window.botsFixture
            const view = source.view.bind(source)
            source.view = async (query, signal) => {
              const answer = await view(query, signal)
              if (query.key.bot_id !== "bot-2") return answer
              // The hidden worker owns its active turn. The parent read view
              // carries only the previous terminal turn and the worker summary.
              answer.head.conversation.active_turn_id = ""
              answer.items = answer.items.filter(row => {
                const message = row.message ?? row.event?.message
                return row.event?.turn?.id !== "exchange-layout-current"
                  && message?.turn_id !== "exchange-layout-current"
                  && (scenario !== "without-card" || message?.exchange?.id !== "exchange-layout-incoming")
              })
              if (answer.snapshot) answer.snapshot.pending_turns = []
              answer.head.background_tasks = answer.head.background_tasks.map(task => task.id !== "exchange-layout-incoming" ? task : {
                ...task, state: scenario === "idle" ? "succeeded" : "active",
                ...(scenario === "idle" ? {ended_at: "2026-11-12T06:23:55Z"} : {activity: "mcp__clawdline__browser_command"}),
              })
              return answer
            }
            open()
          }, scenario)
          await page.locator(".pa-bot-row").filter({hasText: "Beacon"}).click()
          const chat = page.locator(".pa-chat")
          const card = chat.locator('.pa-background-handoff[data-task-id="exchange-layout-incoming"]')
          const stopped = chat.locator('.pa-background-handoff-done[data-task-id="exchange-layout-incoming"]')
          const bar = chat.locator(".pa-chat-work .pa-background-capsule")
          const completed = chat.locator("p.pa-turn").filter({hasText: /^已完成$/})
          const status = chat.getByText("正在使用瀏覽器", {exact: true})
          const stops = chat.locator("button.pa-background-stop, button.pa-send[data-stop]")
          const workerStop = card.locator("button.pa-background-stop")
          if (scenario === "idle") {
            await stopped.waitFor()
            assert.deepEqual([await completed.count(), await stops.count()], [1, 0])
            continue
          }
          if (scenario === "without-card") {
            await bar.getByText("正在使用瀏覽器", {exact: true}).waitFor()
            assert.deepEqual([await card.count(), await status.count(), await stops.count()], [0, 1, 1])
            continue
          }
          await card.getByText("正在使用瀏覽器", {exact: true}).waitFor()
          await card.scrollIntoViewIfNeeded()
          if (artifacts) await page.screenshot({path: path.join(artifacts, `running-${width}x${height}.png`)})
          assert.deepEqual({
            previousCompletions: await completed.count(),
            browserActivityLines: await status.count(),
            stopButtons: await stops.count(),
          }, {
            previousCompletions: 0, browserActivityLines: 1, stopButtons: 1,
          })
          assert.equal(await workerStop.isEnabled(), true)
          const box = await workerStop.boundingBox()
          assert.ok(box && box.x >= 0 && box.x + box.width <= width && box.width >= (mobile ? 44 : 38))
          await workerStop.click()
          await card.getByRole("dialog").waitFor()
          await page.keyboard.press("Escape")
          await card.getByRole("dialog").waitFor({state: "hidden"})
          assert.equal(await workerStop.evaluate(el => el === document.activeElement), true)
        }
        assert.deepEqual(errors, [])
      } finally {
        await context.close()
      }
    })
  }
})
