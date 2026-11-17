/** Authenticated HTTP/SQLite facts -> production validation -> React rendering.
 * No browser, installed Agent, external API or model is used. */
import assert from "node:assert/strict"
import {build} from "esbuild"
import {createRequire} from "node:module"
import {mkdir,writeFile} from "node:fs/promises"
import path from "node:path"
import {fileURLToPath} from "node:url"

const {BOTS_ORIGIN:origin,BOTS_TOKEN:token,BOTS_VIEW_PATH:viewPath,BOTS_ARTIFACTS:artifacts,BOTS_PHASE:phase}=process.env
assert.ok(origin&&token&&viewPath&&artifacts&&phase)
assert.equal(new URL(origin).hostname,"127.0.0.1","fixture must stay on loopback")
const response=await fetch(origin+viewPath,{headers:{Authorization:`Bearer ${token}`}})
assert.equal(response.status,200)
const page=await response.json()
const root=fileURLToPath(new URL("..",import.meta.url))
const bundle=await build({entryPoints:[path.join(root,"src/project-assistants/fallback-progress.fixture.tsx")],bundle:true,platform:"node",format:"cjs",write:false,jsx:"automatic"})
const module={exports:{}},require=createRequire(import.meta.url)
new Function("require","module","exports",bundle.outputFiles[0].text)(require,module,module.exports)
await mkdir(artifacts,{recursive:true})
const turn=page.snapshot.pending_turns.find(t=>t.id===page.head.conversation.active_turn_id)??page.snapshot.last_turn
assert.ok(turn?.attempts?.length,"snapshot must retain durable progress after a fresh read")
const fact=turn.attempts.at(-1)
assert.equal(fact.agent,"codex");assert.equal(fact.send_returned,true)
assert.equal(fact.tool_activity,true);assert.equal(fact.effect,"unknown")
assert.equal(!!fact.answer_observed,phase==="closed")
assert.equal(fact.cleanup_confirmed,phase==="closed")
for(const lang of ["en","zh-Hant"]) {
  const html=module.exports.renderStoredProgress(page,lang)
  assert.ok(html.includes("Claude Code")&&html.includes("Codex"))
  assert.ok(html.includes(lang==="en"?"Effects remain unknown":"副作用仍不明"))
  assert.ok(html.includes(lang==="en"?"not model completion":"不代表模型完成"))
  assert.ok(html.includes(lang==="en"?"Tool activity journaled or observed":"已記錄或觀測到工具活動"))
  assert.ok(html.includes(lang==="en"?"Prepared carry: quoted recap":"預備交接：引用摘要"))
  assert.ok(!html.includes("scripted-session"),"opaque native identity is not displayed")
  if(phase==="sent")assert.ok(!html.includes(lang==="en"?"Answer received":"已收到回覆"))
  await writeFile(path.join(artifacts,`fallback-${phase}-${lang}.html`),html)
}
await writeFile(path.join(artifacts,`fallback-${phase}-snapshot.json`),JSON.stringify(page,null,2)+"\n")
console.log(`PASS ${phase}: real authenticated HTTP + SQLite -> validated snapshot -> production React markup, English and Taiwan Chinese; no browser/native/model requests`)
