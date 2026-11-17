// Run against the isolated Vite fixture. PLAYWRIGHT_MODULE points to an
// existing Playwright install; this test never downloads a browser or package.
import assert from "node:assert/strict"
import { mkdir, writeFile } from "node:fs/promises"
import { join } from "node:path"
import { pathToFileURL } from "node:url"
import { test } from "node:test"

const enabled = !!process.env.VOICE_UI_ORIGIN && !!process.env.PLAYWRIGHT_MODULE
test("voice recovery uses real browser UI, closes capture and preserves manual retry", { skip: !enabled }, async () => {
  const { webkit, chromium } = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
  const directory = process.env.VOICE_UI_OUTPUT
  if (directory) await mkdir(directory, { recursive: true })
  const results = []
  const until = (page, predicate) => page.waitForFunction(predicate, undefined, { polling: 50, timeout: 15000 })
  for (const [engine, type] of [["webkit", webkit], ["chromium", chromium]]) {
    const browser = await type.launch({ headless: true })
    try {
      async function open(mode, width = 390, lang = "zh-Hant") {
        const page = await browser.newPage({ viewport: { width, height: 740 }, deviceScaleFactor: 2, reducedMotion: "reduce" })
        const errors = []
        page.on("pageerror", error => errors.push(error.message))
        await page.goto(`${process.env.VOICE_UI_ORIGIN}/tools/fixtures/voice-recovery.html?mode=${mode}&lang=${lang}`)
        await until(page, () => !!window.voiceFixture)
        await page.locator(".mic").click()
        await until(page, () => window.voiceFixture.snapshot().state === "recording")
        assert.deepEqual(errors, [])
        return page
      }
      async function shot(page, id, caption) {
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "page must not overflow horizontally")
        const buttons = await page.locator(".voice button").evaluateAll(nodes => nodes.map(node => { const r = node.getBoundingClientRect(); return { left: r.left, right: r.right, top: r.top, bottom: r.bottom } }))
        for (const button of buttons) assert.ok(button.left >= 0 && button.right <= await page.evaluate(() => innerWidth) && button.bottom <= 740)
        if (directory && engine === "webkit") {
          await page.locator("#test-case").evaluate((node, value) => { node.textContent = value }, `${id} ${caption}｜本機故障注入`)
          await page.screenshot({ path: join(directory, `${id}.png`) })
        }
        results.push({ engine, id, ...await page.evaluate(() => window.voiceFixture.snapshot()) })
      }
      const delayed = await open("delayed")
      await shot(delayed, "V-01", "錄音中")
      await delayed.locator(".mic").click()
      assert.deepEqual((await delayed.evaluate(() => window.voiceFixture.snapshot())).tracks, ["ended"])
      await shot(delayed, "V-02", "停止即停止收音")
      await until(delayed, () => window.voiceFixture.snapshot().state === "recoverable")
      await shot(delayed, "V-03", "瀏覽器尚未交出錄音")
      await delayed.evaluate(() => window.voiceFixture.finish())
      await until(delayed, () => document.querySelector(".voice")?.textContent.includes("可重試"))
      await shot(delayed, "V-04", "遲到的錄音仍能恢復")
      await delayed.locator(".voice .go").click()
      await until(delayed, () => window.voiceFixture.snapshot().state === "off")
      assert.equal((await delayed.evaluate(() => window.voiceFixture.snapshot())).opens, 1)
      await shot(delayed, "V-05", "重試成功，文字回到輸入框")
      await delayed.close()

      const partial = await open("partial", 320)
      await partial.locator(".mic").click()
      await until(partial, () => window.voiceFixture.snapshot().state === "recoverable")
      await shot(partial, "V-06", "保留片段並標示可能不完整")
      await partial.locator(".voice .go").click()
      await until(partial, () => window.voiceFixture.snapshot().state === "off")
      await partial.close()

      const failed = await open("failed")
      await failed.locator(".mic").click()
      await until(failed, () => window.voiceFixture.snapshot().state === "recoverable")
      await shot(failed, "V-07", "轉文字失敗，錄音仍保留")
      await failed.evaluate(() => window.voiceFixture.succeed())
      await failed.locator(".voice .go").click()
      await until(failed, () => window.voiceFixture.snapshot().state === "off")
      assert.equal((await failed.evaluate(() => window.voiceFixture.snapshot())).opens, 1)
      await failed.close()

      const reading = await open("reading")
      await reading.locator(".mic").click()
      await until(reading, () => window.voiceFixture.snapshot().state === "reading")
      await shot(reading, "V-08", "轉文字中，收音已停止")
      await reading.locator(".voice .drop").click()
      await shot(reading, "V-09", "取消處理仍保留錄音")
      await reading.locator(".voice .go").click()
      await until(reading, () => window.voiceFixture.snapshot().state === "reading")
      assert.equal((await reading.evaluate(() => window.voiceFixture.snapshot())).posts, 1)
      await reading.evaluate(() => window.voiceFixture.succeed())
      await until(reading, () => window.voiceFixture.snapshot().state === "off")
      await reading.close()

      const preparing = await open("preparing")
      await preparing.locator(".mic").click()
      await until(preparing, () => window.voiceFixture.snapshot().state === "preparing")
      await shot(preparing, "V-10", "音訊處理與轉文字分開顯示")
      await preparing.locator(".voice .drop").click()
      await preparing.locator(".voice .drop").click()
      await preparing.close()

      const desktop = await open("failed", 1100, "en")
      await desktop.locator(".mic").click()
      await until(desktop, () => window.voiceFixture.snapshot().state === "recoverable")
      await shot(desktop, "V-11", "英文桌機重試介面")
      await desktop.close()

      const empty = await open("empty")
      await empty.locator(".mic").click()
      await until(empty, () => window.voiceFixture.snapshot().state === "recoverable")
      assert.equal(await empty.locator(".voice .go").count(), 0)
      await shot(empty, "V-12", "結束後沒有音訊，明確說明")
      await empty.locator(".voice .drop").click()
      await empty.close()

      // Native recorder + a synthetic MediaStream, exercising actual final
      // encoded bytes, AudioContext decoding and resampling in both engines.
      const native = await open("native")
      await native.waitForTimeout(1200)
      await native.locator(".mic").click()
      assert.deepEqual((await native.evaluate(() => window.voiceFixture.snapshot())).tracks, ["ended"])
      await until(native, () => window.voiceFixture.snapshot().state === "off")
      const proof = await native.evaluate(() => window.voiceFixture.snapshot())
      assert.equal(proof.posts, 1); assert.equal(proof.opens, 1)
      assert.match(await native.locator(".msg").innerText(), /不必重新/)
      results.push({ engine, id: "native-recorder", ...proof })
      await native.close()
    } finally { await browser.close() }
  }
  if (directory) await writeFile(join(directory, "results.json"), JSON.stringify(results, null, 2) + "\n")
  console.log(JSON.stringify(results))
})

test("Bots composer centers borderless controls and shares dictation recovery", { skip: !enabled }, async () => {
  const { webkit, chromium } = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
  const directory = process.env.VOICE_UI_OUTPUT
  if (directory) await mkdir(directory, { recursive: true })
  const results = []
  for (const [engine, type] of [["webkit", webkit], ["chromium", chromium]]) {
    const browser = await type.launch({ headless: true })
    try {
      for (const [lang, width] of [["zh-Hant", 320], ["zh-Hant", 390], ["en", 1100]]) {
        const page = await browser.newPage({ viewport: { width, height: 740 }, reducedMotion: "reduce" })
        const errors = []
        page.on("pageerror", error => errors.push(error.message))
        const open = async (mode = "normal") => {
          await page.goto(`${process.env.VOICE_UI_ORIGIN}/tools/fixtures/voice-recovery.html?composer=bots&mode=${mode}&lang=${lang}`)
          await page.locator(".pa-mic").waitFor()
        }
        const state = expected => page.waitForFunction(expected => window.voiceFixture.snapshot().state === expected, expected)
        const snapshot = () => page.evaluate(() => window.voiceFixture.snapshot())
        await open()
        const layout = await page.locator(".pa-attach").evaluate(node => {
          const button = node.getBoundingClientRect(), icon = node.querySelector("svg").getBoundingClientRect()
          const editor = document.querySelector(".pa-editor").getBoundingClientRect()
          return { border: getComputedStyle(node).borderWidth, horizontal: (button.left + button.right - icon.left - icon.right) / 2,
            vertical: (button.top + button.bottom - icon.top - icon.bottom) / 2, editor: (button.top + button.bottom - editor.top - editor.bottom) / 2 }
        })
        assert.equal(layout.border, "0px")
        for (const axis of ["horizontal", "vertical", "editor"]) assert.ok(Math.abs(layout[axis]) < 1, `${axis}: ${JSON.stringify(layout)}`)
        const editor = page.locator(".pa-editor textarea"), mic = page.locator(".pa-mic")
        const typed = lang === "en" ? "Existing draft" : "既有草稿"
        await editor.fill(typed)
        await mic.click(); await state("recording")
        assert.equal(await mic.getAttribute("aria-pressed"), "true")
        assert.equal(await page.locator(".pa-send").isDisabled(), true)
        // Typing continues during recording; the latest draft must receive speech.
        await editor.fill(typed + " updated")
        await editor.press("Enter")
        assert.equal((await snapshot()).sends, 0)
        await mic.click()
        assert.deepEqual((await snapshot()).tracks, ["ended"])
        await state("off")
        // Recorder off is distinct from React committing the controlled draft to the screen.
        await page.waitForFunction(prefix => document.querySelector(".pa-editor textarea")?.value.startsWith(prefix), typed + " updated ")
        assert.ok((await editor.inputValue()).startsWith(typed + " updated "), JSON.stringify(await snapshot()))
        assert.match(await editor.inputValue(), lang === "en" ? /recovered recording/ : /這段錄音已恢復/)
        assert.equal((await snapshot()).posts, 1)
        assert.equal((await snapshot()).sends, 0, "dictation never sends automatically")
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
        if (directory) await page.screenshot({ path: join(directory, `bots-${engine}-${lang}-${width}.png`) })
        await page.locator(".pa-send").click()
        assert.equal((await snapshot()).sends, 1)

        await open("failed"); await mic.click(); await state("recording"); await mic.click(); await state("recoverable")
        assert.deepEqual((await snapshot()).tracks, ["ended"])
        await page.evaluate(() => window.voiceFixture.succeed())
        await page.locator(".pa-voice .go").click(); await state("off")
        assert.equal((await snapshot()).opens, 1, "retry reuses the recording")
        assert.equal((await snapshot()).posts, 2)

        await open(); await mic.click(); await state("recording")
        await page.getByRole("button", { name: "Toggle disabled", exact: true }).click()
        assert.equal(await mic.isDisabled(), false, "the active microphone can always be stopped")
        await page.locator(".pa-voice .drop").click(); await state("off")
        assert.deepEqual((await snapshot()).tracks, ["ended"])
        assert.equal(await mic.isDisabled(), true)
        assert.equal((await snapshot()).posts, 0)

        await open(); await mic.click(); await state("recording")
        await page.getByRole("button", { name: "Unmount input", exact: true }).click(); await state("off")
        assert.deepEqual((await snapshot()).tracks, ["ended"])
        assert.equal((await snapshot()).posts, 0)

        await open("reading"); await mic.click(); await state("recording"); await mic.click(); await state("reading")
        await page.getByRole("button", { name: "Unmount input", exact: true }).click(); await state("off")
        await page.evaluate(() => window.voiceFixture.succeed())
        assert.equal((await snapshot()).sends, 0, "late transcription cannot send after leaving the Bot")
        assert.deepEqual(errors, [])
        results.push({ engine, lang, width, layout, recording: "stopped", retry: "same recording", unmount: "cancelled" })
        await page.close()
      }
    } finally { await browser.close() }
  }
  if (directory) await writeFile(join(directory, "bots-results.json"), JSON.stringify(results, null, 2) + "\n")
  console.log(JSON.stringify(results))
})
