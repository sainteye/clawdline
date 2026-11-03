import assert from "node:assert/strict"
import {test} from "node:test"
import {createRequire} from "node:module"
import {fileURLToPath} from "node:url"
import {build} from "esbuild"
import React from "react"
import {renderToStaticMarkup} from "react-dom/server"

const {outputFiles}=await build({stdin:{contents:'export * from "./NativeToolActivity.tsx"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
const compiled={exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {NativeToolCard,NativeToolRun,nativeToolTitle,nativeRunLength}=compiled.exports
const labels={toolLabel:"tool",countLabel:count=>`${count} steps`}
const entry=(id,path,agentID)=>({id,role:"tool",tool:"read",text:"Running view_file",activity:{kind:"called",actions:[{kind:"read",path,line:12,endLine:20}],durationMs:-1,status:"completed",agentID,result:"<script>output</script>",resultKnown:true}})

test("native read labels use the supplied filename and one-based range",()=>{
 const row=entry("a","/workspace/sub/example.go")
 assert.equal(nativeToolTitle(row),"Read example.go:12–20")
 const html=renderToStaticMarkup(React.createElement(NativeToolCard,{...labels,entry:row,expanded:true}))
 assert.match(html,/example.go:12–20/)
 assert.match(html,/\/workspace\/sub\/example.go:12–20/)
 assert.match(html,/&lt;script&gt;output&lt;\/script&gt;/)
 assert.doesNotMatch(html,/<script>|Running view_file/)
})

test("a collapsed native tool does not read or render its stored body",()=>{
 const prior=globalThis.fetch;globalThis.fetch=()=>assert.fail("collapsed result fetched")
 try {
  const row={...entry("a","/workspace/a.go"),detail:{reference:"1:"+"a".repeat(64),textBytes:90000,lineCount:0}}
  const html=renderToStaticMarkup(React.createElement(NativeToolCard,{...labels,entry:row,sessionID:"owned"}))
  assert.match(html,/Read a.go/);assert.doesNotMatch(html,/output|<pre|Reading stored/)
 }finally{globalThis.fetch=prior}
})

test("unidentified reads and different explicit agents never collapse into one agent",()=>{
 const a=entry("a","/workspace/a.go"),b=entry("b","/workspace/b.go"),c=entry("c","/workspace/c.go","child-a"),d=entry("d","/workspace/d.go","child-a"),e=entry("e","/workspace/e.go","child-b")
 assert.equal(nativeRunLength([a,b,c,d,e],0),2)
 assert.equal(nativeRunLength([a,b,c,d,e],2),2)
 assert.equal(nativeRunLength([a,{role:"assistant",text:"interruption"},b],0),1)
 const html=renderToStaticMarkup(React.createElement(NativeToolRun,{...labels,entries:[a,b]}))
 assert.match(html,/2 steps/);assert.match(html,/a.go.*b.go/)
 assert.doesNotMatch(html,/Subagent|<pre|output/)
 const child=renderToStaticMarkup(React.createElement(NativeToolRun,{...labels,entries:[c,d]}))
 assert.match(child,/Subagent child-a/)
})

test("delegated roles are visible without inventing child completion or executing output",()=>{
 const row={role:"tool",text:"Running invoke_subagent",activity:{kind:"called",actions:[],durationMs:-1,status:"completed",delegates:[{name:"Architecture reviewer",model:"inherit"},{name:"Security reviewer"}]}}
 const html=renderToStaticMarkup(React.createElement(NativeToolCard,{...labels,entry:row,expanded:true}))
 assert.match(html,/Architecture reviewer/);assert.match(html,/Security reviewer/)
 assert.match(html,/This record has no readable tool result/)
 assert.doesNotMatch(html,/all subagents.*complete|passed/)
 const empty=renderToStaticMarkup(React.createElement(NativeToolCard,{...labels,entry:{...row,activity:{...row.activity,resultKnown:true}},expanded:true}))
 assert.match(empty,/native interface supplied an empty text result/)
})
