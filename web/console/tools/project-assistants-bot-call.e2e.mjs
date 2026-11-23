/** Browser verification against synthetic Bot facts and audio, with external traffic denied. */
import assert from "node:assert/strict"
import { createServer } from "node:http"
import { mkdir, writeFile } from "node:fs/promises"
import { fileURLToPath, pathToFileURL } from "node:url"
import path from "node:path"
import { build } from "esbuild"

const { BOTS_ARTIFACTS: artifacts, PLAYWRIGHT_MODULE: module, CHROME: executablePath } = process.env
assert.ok(artifacts && module)
await mkdir(artifacts, { recursive: true })
const bundle = await build({
  entryPoints: [fileURLToPath(new URL("./fixtures/project-assistants-bot-call.tsx", import.meta.url))],
  bundle: true, write: false, outfile: "bot-call.js", format: "esm", platform: "browser", target: "es2022", jsx: "automatic",
})
const files = new Map(bundle.outputFiles.map(file => ["/" + path.basename(file.path), file.contents]))
const server = createServer((req, res) => {
  const url = new URL(req.url, "http://localhost")
  const body = files.get(url.pathname)
  if (body) {
    res.setHeader("Content-Type", url.pathname.endsWith(".css") ? "text/css" : "text/javascript")
    return res.end(body)
  }
  res.setHeader("Content-Type", "text/html")
  res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0}</style><link rel="stylesheet" href="/bot-call.css"><div id="app"></div><script type="module" src="/bot-call.js"></script>')
})
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve))
const origin = "http://127.0.0.1:" + server.address().port
const pw = await import(pathToFileURL(module).href)
const evidence = []
try {
  const browser = await pw.chromium.launch({
    executablePath, headless: true,
    args: ["--disable-features=OmniboxEverywhere", "--autoplay-policy=no-user-gesture-required", "--mute-audio"],
  })
  try {
    for (const [lang, width] of [["en", 1440], ["zh-Hant", 390]]) {
      const context = await browser.newContext({ viewport: { width, height: 820 }, hasTouch: width < 700 })
      const page = await context.newPage()
      const errors = [], external = []
      page.on("pageerror", error => errors.push(String(error)))
      await page.route("**/*", route => {
        if (route.request().url().startsWith(origin + "/")) return route.continue()
        external.push(route.request().url())
        return route.abort()
      })
      const w = lang === "en"
        ? { start: "Voice call", opening: "Connecting", mute: "Mute microphone", end: "Hang up",
          wait: "Waiting for approval in chat", listen: "Listening", interrupt: "Stop playback", disconnected: "Call ended", unknown: "outcome is unknown" }
        : { start: "語音通話", opening: "連線中", mute: "麥克風靜音", end: "掛斷",
          wait: "等待在聊天中核准", listen: "正在聆聽", interrupt: "停止播放", disconnected: "通話已結束", unknown: "結果不明" }
      const fixture = (name, ...args) => page.evaluate(([method, values]) => window.callFixture[method](...values), [name, args])
      const set = (key, field, value) => page.evaluate(([key, field, value]) => { window.callFixture[key][field] = value }, [key, field, value])
      const get = (key, field) => page.evaluate(([key, field]) => field ? window.callFixture[key][field] : window.callFixture[key], [key, field])
      const wait = (fn, arg) => page.waitForFunction(fn, arg, { timeout: 15000 })
      const click = locator => width < 700 ? locator.tap() : locator.click()
      const button = name => page.getByRole("button", { name, exact: true })
      const dial = name => button(w.start + " · " + name)
      const status = text => page.locator(".pa-bot-call-status").filter({hasText:text}).waitFor()
      const editor = page.locator(".pa-editor textarea")
      const screenshot = stage => page.screenshot({ path: path.join(artifacts, lang + "-" + stage + ".png") })
      const start = "Project assistant"
      try {
        await page.goto(origin + "/?handoff=1&lang=" + lang)
        await wait(() => window.callFixture.handoffState.pending)
        assert.equal(await dial(start).count(), 0, "an unresolved handoff read cannot prove no draft exists")
        await fixture("releaseHandoff")
        await page.locator(".pa-handoff-card").waitFor()
        assert.equal(await dial(start).count(), 0, "a draft handoff must not become an unversioned call input")
        assert.equal(await get("audio", "opens"), 0)
        assert.equal((await get("received")).length, 0)
        await click(button(lang === "en" ? "Keep current conversation" : "保留目前對話"))
        await wait(() => window.callFixture.received.length === 0 && !document.querySelector(".pa-bot-call-start button")?.disabled)
        await editor.fill("Unsent text draft")
        assert.equal(await get("audio", "opens"), 0)
        await set("audio", "defer", true)
        await click(dial(start))
        await status(w.opening)
        await wait(() => window.callFixture.audio.opens === 1)
        await screenshot("permission")
        await click(button(w.end))
        await fixture("grant")
        await wait(() => window.callFixture.trackStatus().state === "ended")
        assert.equal((await get("received")).length, 0)

        await click(dial(start))
        await status(w.listen)
        await click(button(w.mute))
        assert.equal((await fixture("trackStatus")).enabled, false)
        await fixture("tone", 300, "Discard muted capture", true)
        assert.equal((await get("received")).length, 0)
        await click(button(w.mute))
        assert.equal(await fixture("tone", 330, "First synthetic utterance"), true)
        await wait(() => window.callFixture.received.length === 1)
        await fixture("permission", 0)
        await status(w.wait)
        await screenshot("approval")
        assert.equal((await fixture("trackStatus")).enabled, false)
        await fixture("tone", 350, "Do not decide permission by voice", true)
        assert.equal((await get("approved")).length, 0)
        await click(page.locator(".pa-permissions [data-option-kind=allow_once]"))
        await wait(() => window.callFixture.approved.length === 1)
        await fixture("complete", 0, 0, "First synthetic reply")
        await wait(() => window.callFixture.audio.energy[0] > 0)
        assert.deepEqual(await get("audio", "plays"), ["First synthetic reply"])
        await screenshot("playing")
        await click(button(w.interrupt))
        await status(w.listen)
        assert.equal(await get("audio", "cancelled"), 1)
        assert.equal(await editor.inputValue(), "Unsent text draft")

        await fixture("tone", 390, "Second synthetic utterance")
        await wait(() => window.callFixture.received.length === 2)
        await fixture("complete", 0, 1, "Second synthetic reply")
        await wait(() => window.callFixture.audio.energy[1] > 0)
        await fixture("finishAudio")
        await status(w.listen)
        const firstTwo = await get("received")
        for (const request of firstTwo) {
          assert.equal(request.fence.key.machine_id, "fixture-one")
          assert.equal(request.fence.key.bot_id, "bot-0")
          assert.equal(request.fence.conversation_id, "chat-0")
        }
        const keys = firstTwo.map(r => r.command.idempotency_key)
        assert.notEqual(keys[0], keys[1])
        assert.equal(keys[0].split("/")[0], keys[1].split("/")[0])

        await fixture("anotherHandoff")
        await fixture("tone", 400, "A draft created by another viewer")
        await page.locator(".pa-bot-call-start [role=status]").filter({hasText:lang === "en" ? "could not complete" : "無法完成"}).waitFor()
        assert.equal((await get("handoffState")).rejected, 1)
        assert.equal((await get("received")).length, 2)
        assert.equal(await page.locator(".pa-held-send").count(), 0, "explicit pre-admission refusal must release the call journal")
        await click(button(lang === "en" ? "Keep current conversation" : "保留目前對話"))
        await click(dial(start))
        await status(w.listen)

        await set("network", "holdSend", true)
        await fixture("tone", 410, "Original Bot only")
        await wait(() => window.callFixture.received.length === 3)
        await fixture("select", 1)
        await dial("Interface design").waitFor()
        assert.equal((await fixture("trackStatus")).state, "ended")
        assert.equal(await page.getByText("Original Bot only").count(), 0)
        await fixture("complete", 0, 2, "Late reply after Bot switch")
        await fixture("release")
        await fixture("tone", 420, "Late capture after Bot switch", true)
        await click(dial("Interface design"))
        await status(w.listen)
        await fixture("select", 2)
        await dial(start).waitFor()
        assert.equal((await fixture("trackStatus")).state, "ended")
        await click(dial(start))
        await status(w.listen)
        await fixture("tone", 460, "Other source only")
        await wait(() => window.callFixture.received.length === 4)
        assert.equal((await get("received"))[3].fence.key.machine_id, "fixture-two")
        await fixture("disconnect")
        await page.locator(".pa-bot-call-start [role=status]").filter({hasText:w.disconnected}).waitFor()
        assert.equal((await fixture("trackStatus")).state, "ended")
        await fixture("complete", 2, 3, "Late reply after source disconnect")
        await fixture("reconnect")
        await fixture("select", 0, "other-account")
        await dial(start).waitFor()
        await click(dial(start))
        await status(w.listen)
        await fixture("holdPreflight")
        await fixture("tone", 490, "Hang up before source admission")
        await wait(() => window.callFixture.preflightCount() > 0)
        await click(button(w.end))
        await fixture("releasePreflight")
        await fixture("tone", 490, "Late capture after hangup", true)
        assert.equal((await get("received")).length, 4)

        await fixture("select", 1)
        await click(dial("Interface design"))
        await status(w.listen)
        await set("network", "loseReceipt", true)
        await fixture("tone", 520, "One request with lost acknowledgement")
        await page.locator(".pa-bot-call-start [role=status]").filter({hasText:w.unknown}).waitFor()
        await fixture("complete", 1, 4, "Late reply with unknown admission")
        await page.getByText("Late reply with unknown admission", {exact:true}).waitFor()
        await screenshot("unknown")
        const captures = await get("audio", "captures")
        const plays = await get("audio", "plays")
        assert.equal(captures.length, 7)
        assert.ok(captures.every(peak => peak > 0))
        assert.deepEqual(plays, ["First synthetic reply", "Second synthetic reply"])
        assert.equal((await get("received")).length, 5)
        assert.equal((await fixture("trackStatus")).state, "ended")
        assert.deepEqual(external, [])
        assert.deepEqual(errors, [])
        evidence.push({lang, width, handoffDraftBlocked: true, handoffReadPendingBlocked: true, handoffInputRefusals: (await get("handoffState")).rejected, sends: await get("received"), plays, captures, externalRequests: external.length})
      } catch (error) {
        await screenshot("failure")
        console.error(JSON.stringify({lang, errors, external, text: await page.locator("#app").innerText(),
          plays: await get("audio", "plays"), energy: await get("audio", "energy")}))
        throw error
      } finally {
        await context.close()
      }
    }
    for (const [engine, lang, width] of [["live", "zh-Hant", 390], ["realtime", "en", 1440]]) {
      const context = await browser.newContext({ viewport: {width, height: 820}, hasTouch: width < 700 })
      const page = await context.newPage()
      const errors = [], unexpected = [], openings = []
      page.on("pageerror", error => errors.push(String(error)))
      await page.addInitScript(engine => {
        const voice = { sent: [], current: null, emit(event) {
          const data = JSON.stringify(event)
          voice.current?.onmessage?.({data})
          voice.current?.dispatchEvent(new MessageEvent("message", {data}))
        } }
        window.voiceFixture = voice
        class Channel extends EventTarget {
          readyState = "connecting"
          send(raw) {
            const event = JSON.parse(raw)
            voice.sent.push(event)
            if (event.type === "session.update") queueMicrotask(() => voice.emit({type:"session.updated",session:event.session}))
            if (event.type === "session.close") queueMicrotask(() => voice.emit({type:"session.closed",usage:{seconds:15}}))
          }
          close() { this.readyState = "closed"; this.dispatchEvent(new Event("close")); this.onclose?.() }
        }
        window.RTCPeerConnection = class {
          iceGatheringState = "complete"
          addTrack() {}
          createDataChannel() { return voice.current = new Channel() }
          async createOffer() { return {type:"offer",sdp:"v=0 fixture-offer"} }
          async setLocalDescription(sdp) { this.localDescription = sdp }
          async setRemoteDescription() {
            voice.current.readyState = "open"
            queueMicrotask(() => {
              if (engine === "live") voice.emit({type:"session.started"})
              else voice.current.onopen?.()
            })
          }
          close() {}
        }
      }, engine)
      await page.route("**/*", async route => {
        const request = route.request()
        const url = new URL(request.url())
        if (url.origin === origin) {
          if (url.pathname === "/v1/settings/copilot") return route.fulfill({json:{key_set:true,engine,voice:"marin"}})
          if (url.pathname === "/v1/voice/copilot-live") {
            openings.push(request.postDataJSON())
            return route.fulfill({json:{sdp:"v=0 fixture-answer",session_id:"synthetic"}})
          }
          if (url.pathname === "/v1/voice/copilot-session") return route.fulfill({json:{client_secret:"fixture-token",model:"gpt-realtime-mini"}})
          if (url.pathname === "/v1/voice/copilot-journal") return route.fulfill({json:{ok:true}})
          if (!url.pathname.startsWith("/v1/")) return route.continue()
        }
        if (url.origin === "https://api.openai.com" && url.pathname === "/v1/realtime/calls") {
          openings.push({sdp:request.postData()})
          return route.fulfill({body:"v=0 fixture-answer",headers:{"access-control-allow-origin":origin}})
        }
        unexpected.push(url.pathname)
        return route.abort()
      })
      const emit = event => page.evaluate(event => window.voiceFixture.emit(event), event)
      const sent = () => page.evaluate(() => window.voiceFixture.sent)
      const speak = text => emit(engine === "live" ? {type:"session.input_transcript.delta",delta:text} : {type:"conversation.item.input_audio_transcription.completed",transcript:text})
      let serial = 0
      const tool = async (name, args = {}) => {
        const id = "fixture-tool-" + ++serial
        const call = {call_id:id,name,arguments:JSON.stringify(args)}
        await emit(engine === "live"
          ? {type:"response.event",event:{type:"response.output_item.done",item:{type:"function_call",...call}}}
          : {type:"response.function_call_arguments.done",...call})
        await page.waitForFunction(id => window.voiceFixture.sent.some(event => event.item?.call_id === id), id)
        return JSON.parse((await sent()).find(event => event.item?.call_id === id).item.output)
      }
      try {
        await page.goto(origin + "/?provider=1&lang=" + lang)
        const callLabel = lang === "en" ? "Voice call" : "語音通話"
        await page.getByRole("button", {name:callLabel+" · Project assistant",exact:true}).click()
        const panel = page.locator(".pa-bot-voice-panel")
        await panel.getByText(lang === "en" ? "Listening" : "正在聆聽",{exact:true}).waitFor()
        assert.equal(await panel.locator("svg.pa-avatar").count(), 1)
        const update = (await sent()).find(event=>event.type==="session.update")
        const configured = engine === "live" ? update.session.delegation.responses : update.session
        assert.deepEqual(configured.tools.map(tool=>tool.name),
          ["bot_status","recent_messages","search_memory","send_to_bot","answer_approval","stop_bot"])
        assert.match(configured.instructions,/Project assistant/)
        assert.equal((await tool("bot_status")).status,"idle")
        assert.ok((await tool("recent_messages",{count:1})).messages[0].text)
        assert.deepEqual((await tool("search_memory",{query:"favorite color"})).matches,[{text:"fixture favorite color is teal"}])
        assert.equal((await tool("list_sessions")).error,"tool_not_available_in_this_call")
        assert.equal((await tool("send_to_bot",{text:"do not send"})).error,"new_spoken_confirmation_required")
        const mute = panel.getByRole("button",{name:lang === "en" ? "Mute microphone" : "麥克風靜音"})
        await mute.click()
        assert.equal(await page.evaluate(()=>window.callFixture.trackStatus().enabled),false)
        await mute.click()
        await speak("Please send the confirmed request.")
        assert.equal((await tool("send_to_bot",{text:"Synthetic first request"})).status,"accepted")
        assert.equal((await tool("send_to_bot",{text:"Duplicate request"})).error,"new_spoken_confirmation_required")
        await page.evaluate(()=>window.callFixture.permission(0))
        await page.waitForFunction(()=>window.voiceFixture.sent.some(e=>JSON.stringify(e).includes("needs approval")))
        assert.equal((await tool("answer_approval")).status,"review_in_chat")
        assert.equal(await page.evaluate(()=>window.callFixture.approved.length),0)
        await page.locator(".pa-permissions [data-option-kind=allow_once]").click()
        await page.waitForFunction(()=>window.callFixture.approved.length===1)
        await page.evaluate(()=>window.callFixture.complete(0,0,"Synthetic finished reply"))
        await page.waitForFunction(()=>window.voiceFixture.sent.some(e=>JSON.stringify(e).includes("Synthetic finished reply")))
        await emit({type:engine==="live"?"session.output_transcript.delta":"response.output_audio_transcript.delta",delta:"Read back the result."})
        await panel.getByText("Read back the result.",{exact:true}).waitFor()
        await page.screenshot({path:path.join(artifacts,engine+"-bot-voice.png")})
        await speak("Please send the second request.")
        assert.equal((await tool("send_to_bot",{text:"Synthetic second request"})).status,"accepted")
        await emit({type:engine==="live"?"session.output_transcript.delta":"response.output_audio_transcript.delta",delta:"The Bot accepted it."})
        await speak("Please stop the Bot.")
        assert.equal((await tool("stop_bot")).status,"accepted")
        const writes = await page.evaluate(()=>({sends:window.callFixture.received,stops:window.callFixture.stops}))
        assert.equal(writes.sends.length,2)
        assert.ok([...writes.sends,...writes.stops].every(write=>write.fence.key.machine_id==="fixture-one" && write.fence.key.bot_id==="bot-0"))
        assert.equal(writes.stops.length,1)
        await page.evaluate(()=>window.callFixture.select(2))
        await panel.waitFor({state:"hidden"})
        await page.waitForFunction(()=>window.callFixture.trackStatus().state==="ended")
        assert.equal(openings.length,1)
        assert.deepEqual(unexpected,[])
        assert.deepEqual(errors,[])
        evidence.push({engine,lang,width,opens:openings.length,assertedTools:configured.tools.length,writes:writes.sends.length+writes.stops.length,providerRequests:0})
      } catch (error) {
        await page.screenshot({path:path.join(artifacts,engine+"-bot-voice-failure.png")})
        console.error(JSON.stringify({engine, errors, unexpected, text: await page.locator("#app").innerText(),events:await sent()}))
        throw error
      } finally {
        await context.close()
      }
    }
  } finally {
    await browser.close()
  }
  await writeFile(path.join(artifacts, "evidence.json"), JSON.stringify(evidence, null, 2))
  console.log(JSON.stringify(evidence))
} finally {
  server.closeAllConnections()
  await new Promise(resolve => server.close(resolve))
}
