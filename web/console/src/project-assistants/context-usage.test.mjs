import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const {outputFiles}=await build({stdin:{contents:'export {ContextUsage} from "./ContextUsage";export {words} from "./words"',resolveDir:fileURLToPath(new URL(".",import.meta.url)),loader:"ts"},bundle:true,platform:"node",format:"cjs",packages:"external",write:false,jsx:"automatic",loader:{".css":"empty"}})
const compiled={exports:{}}
new Function("require","module","exports",outputFiles[0].text)(createRequire(import.meta.url),compiled,compiled.exports)
const {ContextUsage,words}=compiled.exports

const shown=(used,window,language)=>{
  const html=renderToStaticMarkup(React.createElement(ContextUsage,{usage:{used_tokens:used,window_tokens:window,live:true},w:words(language),language}))
  const bdi=html.match(/<bdi([^>]*)>([^<]*)<\/bdi>/)
  return {text:bdi[2],attrs:bdi[1],html}
}

test("context usage shows compact K/M counts in every language",()=>{
  for(const language of ["en","zh-Hant"]){
    assert.equal(shown(70618,1000000,language).text,"70.6K / 1M")
    assert.equal(shown(200000,1000000,language).text,"200K / 1M")
    assert.equal(shown(950,200000,language).text,"950 / 200K")
    assert.doesNotMatch(shown(70618,1000000,language).text,/萬/)
  }
})

test("context usage keeps the full counts in its hover title and meter",()=>{
  const {attrs,html}=shown(70618,1000000,"en")
  assert.match(attrs,/title="70,618 \/ 1,000,000"/)
  assert.match(html,/aria-valuemax="1000000"/)
  assert.match(html,/aria-valuenow="929382"/)
})
