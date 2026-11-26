import {test} from "node:test"
import assert from "node:assert/strict"
import {createRequire} from "node:module"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"
import React from "react"
import {renderToStaticMarkup} from "react-dom/server"

// An Attention filter with nothing matching. When the reading missed part of
// the machine, "No Sessions need your attention" would be a claim the list
// cannot make. Both lists must pass `incomplete`; the type makes it required.
async function load() {
  const {outputFiles}=await build({stdin:{contents:'export {SessionStateEmpty} from "./SessionStateEmpty"',
    resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
  const compiled={exports:{} as {SessionStateEmpty:typeof import("./SessionStateEmpty").SessionStateEmpty}}
  new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
  return compiled.exports
}

test("an incomplete reading does not say no Session needs attention", async () => {
  const {SessionStateEmpty}=await load()
  const card=(incomplete:boolean)=>renderToStaticMarkup(React.createElement(SessionStateEmpty,{filter:"attention",incomplete,onReset:()=>{}}))
  assert.match(card(true),/Some statuses are still unavailable/)
  assert.doesNotMatch(card(true),/No Sessions need your attention/)
  assert.match(card(false),/No Sessions need your attention/)
})
