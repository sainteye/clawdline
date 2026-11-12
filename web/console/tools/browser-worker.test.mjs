// Run the real extension in a fresh local Chrome for Testing profile:
// CLAWDLINE_BROWSER_E2E_CHROMIUM=<binary> node --test web/console/tools/browser-worker.test.mjs
import assert from "node:assert/strict"
import { test } from "node:test"
import { spawn } from "node:child_process"
import { createServer } from "node:http"
import { once } from "node:events"
import { mkdtemp, cp, writeFile, readFile, rm } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { runInNewContext } from "node:vm"

const workerSource = await readFile(new URL("../../../extension/chrome/worker.js", import.meta.url), "utf8")
const pageSource = await readFile(new URL("../../../extension/chrome/page.js", import.meta.url), "utf8")

test("every successful type result is private even when focus classification is stale", async (t) => {
  for (const secret of [false, true]) for (const person of [false, true]) {
    await t.test(`secret=${secret}, person=${person}`, async () => {
      let receive, finish
      const commands = []
      const listener = { addListener() {} }
      const chrome = {
        storage: { session: { set: async () => {} } },
        runtime: {
          getManifest: () => ({version:"test"}), onMessage: listener,
          connectNative: () => ({ onMessage: { addListener(fn) { receive = fn } },
            onDisconnect: listener, postMessage(msg) { if (msg.results) finish(msg.results[0]) } }),
        },
        tabs: {get: async () => ({url:"https://example.test/"}), onRemoved: listener},
        debugger: {attach: async () => {}, onDetach: listener,
          sendCommand: async (_source, method, params) => {
            commands.push({method, params})
            // A plain field at classification time can become a password
            // before insertText. No DOM snapshot can rule that out.
            if (method === "Runtime.evaluate") return {result:{value:false}}
            if (method === "Page.getLayoutMetrics") return {cssLayoutViewport:{clientWidth:100,clientHeight:100}}
            if (method === "Page.captureScreenshot") return {data:"page-picture"}
            return {}
          } },
      }
      runInNewContext(pageSource.replaceAll("export function", "function") + "\n" + workerSource.replace(/^import .*$/m, ""),
        {chrome, navigator:{userAgent:"test"}, setTimeout, clearTimeout})
      const result = await new Promise(resolve => {
        finish = resolve
        receive({command:{id:"type",op:"type",tab:1,person,args:{text:"synthetic-🔐",secret}}})
      })
      assert.equal(result.ok, true)
      assert.equal(result.password, true)
      assert.equal(result.thumb, undefined)
      assert.equal(result.url, undefined)
      assert.equal(result.target, undefined)
      assert.equal(result.value.typed, 11)
      assert.ok(!JSON.stringify(result).includes("synthetic"))
      assert.equal(commands.filter(c => c.method === "Input.insertText").length, 1)
      assert.equal(commands.find(c => c.method === "Input.insertText").params.text, "synthetic-🔐")
      assert.equal(commands.filter(c => c.method === "Page.captureScreenshot").length, 0)
    })
  }
})

const browser = process.env.CLAWDLINE_BROWSER_E2E_CHROMIUM

test("type never discloses field text across focus boundaries", { skip: !browser, timeout: 90000 }, async (t) => {
  const dir = await mkdtemp(join(process.env.CLAWDLINE_BROWSER_TEST_TMP || tmpdir(), "worker-privacy-"))
  let chromeProcess, ws, server, closeTab
  t.after(async () => {
    if (closeTab) await closeTab().catch(() => {})
    ws?.close()
    if (chromeProcess && chromeProcess.exitCode === null && chromeProcess.signalCode === null) {
      chromeProcess.kill(); await once(chromeProcess, "exit")
    }
    if (server?.listening) await new Promise(resolve => { server.closeAllConnections(); server.close(resolve) })
    await rm(dir, { recursive: true, force: true, maxRetries: 5 })
  })
  const ext = join(dir, "extension")
  await cp(new URL("../../../extension/chrome/", import.meta.url), ext, { recursive: true })
  const manifest = JSON.parse(await readFile(join(ext, "manifest.json"), "utf8"))
  manifest.background.service_worker = "test-entry.js"
  await writeFile(join(ext, "manifest.json"), JSON.stringify(manifest))
  // Native transport alone is substituted. The product worker and actual
  // chrome.debugger commands run unchanged inside Chrome's extension realm.
  await writeFile(join(ext, "test-entry.js"), `
    let receive; const waiting = new Map(); let next = 0;
    chrome.runtime.connectNative = () => ({
      onMessage: { addListener(fn) { receive = fn } },
      onDisconnect: { addListener() {} },
      postMessage(msg) { for (const r of msg.results || []) { waiting.get(r.id)(r); waiting.delete(r.id) } }
    });
    const send = chrome.debugger.sendCommand.bind(chrome.debugger);
    globalThis.race = "";
    chrome.debugger.sendCommand = async (source, method, params) => {
      if (method === "Input.insertText" && globalThis.race) {
        const expression = globalThis.race; globalThis.race = "";
        await send(source, "Runtime.evaluate", { expression });
      }
      return send(source, method, params);
    };
    ${workerSource}
    globalThis.command = c => new Promise(resolve => {
      c.id = String(++next); waiting.set(c.id, resolve); receive({command:c});
    });
    globalThis.pageEval = async (tab, expression) => {
      const r = await send({tabId:tab}, "Runtime.evaluate", {expression,returnByValue:true,awaitPromise:true});
      if (r.exceptionDetails) throw Error(r.exceptionDetails.text);
      return r.result.value;
    };
    globalThis.ready = true;
  `)

  server = createServer((_req, res) => {
    res.setHeader("Content-Type", "text/html")
    res.end(`<!doctype html><input id=plain><input id=password type=password>
      <textarea id=area></textarea><div id=editable contenteditable=true></div>
      <div id=shadow></div><div id=nested></div><div id=closed></div>
      <iframe id=frame srcdoc="<input id=field type=password>"></iframe>
      <iframe id=opaque sandbox="allow-scripts" srcdoc="<input type=password><script>onmessage=()=>{document.querySelector('input').focus();parent.postMessage({opaque:true},'*')};document.querySelector('input').oninput=()=>parent.postMessage({opaqueLength:document.querySelector('input').value.length},'*')<\/script>"></iframe>
      <script>
      shadow.attachShadow({mode:'open'}).innerHTML='<input type=password>';
      const outer=nested.attachShadow({mode:'open'}); outer.innerHTML='<div id=inner></div>';
      outer.firstChild.attachShadow({mode:'open'}).innerHTML='<input type=password>';
      const hidden=document.getElementById('closed').attachShadow({mode:'closed'}); hidden.innerHTML='<input type=password>';
      window.focusClosed=()=>hidden.firstChild.focus();
      window.closedLength=()=>hidden.firstChild.value.length;
      window.opaqueLength=0;
      window.focusOpaque=()=>new Promise(resolve=>{
        const listener=event=>{if(event.source===opaque.contentWindow&&event.data.opaque){window.removeEventListener('message',listener);resolve(true)}};
        window.addEventListener('message',listener);opaque.contentWindow.postMessage('focus','*');
      });
      window.addEventListener('message',event=>{if(event.source===opaque.contentWindow&&typeof event.data.opaqueLength==='number')window.opaqueLength=event.data.opaqueLength});
      window.ready=true;
      </script>`)
  })
  server.listen(0, "127.0.0.1"); await once(server, "listening")
  chromeProcess = spawn(browser, ["--headless=new", "--no-first-run", "--no-default-browser-check",
    "--use-mock-keychain", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0",
    `--user-data-dir=${join(dir, "profile")}`, `--load-extension=${ext}`, `--disable-extensions-except=${ext}`, "about:blank"], { stdio: ["ignore", "ignore", "pipe"] })
  const endpoint = await new Promise((resolve, reject) => {
    let stderr = ""
    const timer = setTimeout(() => reject(Error(`Chrome did not start: ${stderr}`)), 15000)
    chromeProcess.stderr.on("data", data => {
      stderr += data
      const hit = stderr.match(/DevTools listening on (ws:\/\/\S+)/)
      if (hit) { clearTimeout(timer); resolve(hit[1]) }
    })
    chromeProcess.once("error", reject)
    chromeProcess.once("exit", (code, signal) => { clearTimeout(timer); reject(Error(`Chrome exited ${code}/${signal}: ${stderr}`)) })
  })
  ws = new WebSocket(endpoint); await once(ws, "open")
  let next = 0
  const pending = new Map()
  ws.onmessage = event => {
    const msg = JSON.parse(event.data), p = pending.get(msg.id)
    if (!p) return
    pending.delete(msg.id); clearTimeout(p.timer)
    msg.error ? p.reject(Error(msg.error.message)) : p.resolve(msg.result)
  }
  const send = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
    const id = ++next
    const timer = setTimeout(() => { pending.delete(id); reject(Error(`CDP timeout: ${method} ${String(params.expression || "").slice(0,160)}`)) }, 15000)
    pending.set(id, {resolve, reject, timer}); ws.send(JSON.stringify({id, method, params, sessionId}))
  })
  let worker
  for (let i = 0; i < 100 && !worker; i++) {
    worker = (await send("Target.getTargets")).targetInfos.find(x => x.type === "service_worker" && x.url.endsWith("/test-entry.js"))
    if (!worker) await new Promise(resolve => setTimeout(resolve, 50))
  }
  assert.ok(worker, "extension worker started")
  const {sessionId} = await send("Target.attachToTarget", {targetId: worker.targetId, flatten:true})
  await send("Runtime.enable", {}, sessionId)
  await send("Runtime.runIfWaitingForDebugger", {}, sessionId)
  const evaluate = async expression => {
    const r = await send("Runtime.evaluate", {expression, returnByValue:true, awaitPromise:true}, sessionId)
    assert.equal(r.exceptionDetails, undefined, r.exceptionDetails?.text)
    return r.result.value
  }
  for (let i = 0; i < 100 && !await evaluate("globalThis.ready === true"); i++) await new Promise(resolve => setTimeout(resolve, 50))
  const command = c => evaluate(`command(${JSON.stringify(c)})`)
  t.diagnostic("extension is ready; opening synthetic form")
  const opened = await command({op:"open", args:{url:`http://127.0.0.1:${server.address().port}/`}})
  t.diagnostic("synthetic form opened")
  assert.equal(opened.ok, true)
  const tab = opened.tab
  closeTab = () => command({op:"close", tab})
  const page = expression => evaluate(`pageEval(${tab},${JSON.stringify(expression)})`)
  assert.equal(await page("window.ready"), true)
  const cases = [
    ["plain", "plain.focus()", "plain.value.length"],
    ["password", "password.focus()", "password.value.length"],
    ["shadow password", "shadow.shadowRoot.firstChild.focus()", "shadow.shadowRoot.firstChild.value.length"],
    ["nested shadow password", "nested.shadowRoot.firstChild.shadowRoot.firstChild.focus()", "nested.shadowRoot.firstChild.shadowRoot.firstChild.value.length"],
    ["same-origin iframe", "frame.contentDocument.querySelector('input').focus()", "frame.contentDocument.querySelector('input').value.length"],
    ["closed shadow", "focusClosed()", "closedLength()"],
    ["opaque iframe", "focusOpaque()", "window.opaqueLength"],
    ["textarea", "area.focus()", "area.value.length"],
    ["contenteditable", "editable.focus()", "editable.textContent.length"],
  ]
  for (const [name, focus, length] of cases) {
    await t.test(name, async () => {
      if (name === "opaque iframe") {
        const rect = await page("(()=>{const r=opaque.getBoundingClientRect();return {x:r.left+20,y:r.top+20}})()")
        await command({op:"click",tab,args:rect})
      }
      await page(focus)
      const before = length ? await page(length) : null
      const result = await command({op:"type", tab, args:{text:"synthetic-🔐"}})
      assert.equal(result.ok, true)
      assert.equal(result.password, true, "typing must always have a private audit result")
      assert.equal(result.thumb, undefined, "typing must never take a thumbnail")
      assert.equal(result.value.typed, 11)
      assert.ok(!JSON.stringify(result).includes("synthetic"))
      if (length) {
        const expected = before + "synthetic-🔐".length
        // Opaque frames report only the synthetic fixture's length by message.
        for (let i = 0; i < 100 && await page(length) !== expected; i++) await new Promise(resolve => setTimeout(resolve, 10))
        assert.equal(await page(length), expected, "ordinary CDP insertion is preserved")
      }
    })
  }
  for (const person of [false, true]) {
    await t.test(`focus changes before insertion (person=${person})`, async () => {
      await page("plain.focus()")
      await evaluate(`globalThis.race = "password.focus()"`)
      const before = await page("password.value.length")
      const result = await command({op:"type", tab, person, args:{text:"race"}})
      assert.equal(result.password, true)
      assert.equal(result.thumb, undefined)
      assert.equal(await page("password.value.length"), before + 4)
    })
  }
  await t.test("field type changes only during insertion", async () => {
    await page("plain.focus()")
    const before = await page("plain.value.length")
    await evaluate(`globalThis.race = "plain.type='password'; plain.addEventListener('input',()=>plain.type='text',{once:true})"`)
    const result = await command({op:"type", tab, args:{text:"transient"}})
    assert.equal(result.password, true)
    assert.equal(result.thumb, undefined)
    assert.equal(await page("plain.type"), "text")
    assert.equal(await page("plain.value.length"), before + 9)
  })
  await t.test("explicit secret flag is preserved", async () => {
    await page("plain.focus()")
    const result = await command({op:"type", tab, args:{text:"secret",secret:true}})
    assert.equal(result.password, true)
    assert.equal(result.thumb, undefined)
  })
})
