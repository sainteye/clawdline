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
