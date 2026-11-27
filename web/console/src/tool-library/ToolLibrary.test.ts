import {test} from "node:test"
import assert from "node:assert/strict"
import {createRequire} from "node:module"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"
import React from "react"
import {renderToStaticMarkup} from "react-dom/server"
import type {SessionRow} from "@clawdline/contract"

async function library() {
  if(typeof document==="undefined")Object.defineProperty(globalThis,"document",{configurable:true,value:{getElementById:()=>null,querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{},createElement:()=>({style:{},setAttribute(){},appendChild(){},addEventListener(){}}),documentElement:{style:{},setAttribute(){},lang:"en"},body:{appendChild(){},classList:{add(){},remove(){},toggle(){},contains:()=>false}}}})
  const {outputFiles}=await build({stdin:{contents:'export {ToolLibrary} from "./ToolLibrary"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
  const base=createRequire(import.meta.url)
  // A portal cannot render on the server; the dialog's content is what is under test.
  const load=(id:string)=>id==="react-dom"?{...base("react-dom"),createPortal:(node:unknown)=>node}:base(id)
  const compiled={exports:{} as {ToolLibrary:typeof import("./ToolLibrary").ToolLibrary}}
  new Function("require","module","exports",outputFiles[0].text)(load,compiled,compiled.exports)
  return compiled.exports.ToolLibrary
}

test("the library opens on Recommended, which is the first tab", async () => {
  const ToolLibrary=await library()
  const html=renderToStaticMarkup(React.createElement(ToolLibrary,{row:{id:"row",sessionId:"s"} as SessionRow,onClose(){},onPrepared(){}}))
  const tabs=[...html.matchAll(/role="tab" aria-selected="(true|false)"[^>]*>([^<]*)</g)].map(m=>`${m[2].trim()}:${m[1]}`)
  assert.deepEqual(tabs,["Recommended:true","My collection:false"])
})
