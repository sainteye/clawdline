import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const { outputFiles } = await build({entryPoints:[fileURLToPath(new URL("./ToolResult.tsx",import.meta.url))],bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic"})
const compiled = {exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {ToolResult} = compiled.exports
const result = {reference:"0:"+"a".repeat(64),preview:"<script>unsafe</script>\nsecond",textBytes:34,rawBytes:100,images:0,unknownBlocks:0,sourceTruncated:false}
test("short text renders inline and no detail or image is fetched by the initial render",()=>{
 const prior=globalThis.fetch
 globalThis.fetch=()=>assert.fail("collapsed result read")
 try {
  const html=renderToStaticMarkup(React.createElement(ToolResult,{sessionID:"s",result:{...result,textBytes:0}}))
  assert.match(html,/&lt;script&gt;unsafe&lt;\/script&gt;\nsecond/)
  assert.doesNotMatch(html,/<script>|Read full text|<img/)
  assert.match(html,/Stored native output/)
 }finally{globalThis.fetch=prior}
})
test("long and mixed results expose stored detail and source truncation without claiming verification",()=>{
 const html=renderToStaticMarkup(React.createElement(ToolResult,{sessionID:"s",result:{...result,textBytes:2000,images:2,unknownBlocks:1,sourceTruncated:true}}))
 assert.match(html,/Read full text/)
 assert.match(html,/not preloaded/)
 assert.match(html,/already shortened/)
 assert.doesNotMatch(html,/passed|<img|open=""/)
})
