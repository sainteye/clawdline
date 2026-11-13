import {test} from 'node:test'
import assert from 'node:assert/strict'
import {build} from 'esbuild'
import {createRequire} from 'node:module'
import {fileURLToPath} from 'node:url'
import {renderToStaticMarkup} from 'react-dom/server'
import {createElement} from 'react'
const {outputFiles}=await build({stdin:{contents:'export {CopilotProgress} from "./CopilotProgress.tsx"',resolveDir:fileURLToPath(new URL('.',import.meta.url)),loader:'ts'},bundle:true,platform:'node',format:'cjs',packages:'external',write:false,loader:{'.css':'empty'}})
const module={exports:{}};new Function('require','module','exports',outputFiles[0].text)(createRequire(import.meta.url),module,module.exports)
const {CopilotProgress}=module.exports
test('running work is visibly active, while confirmation and failure show a stopped state with a next step',()=>{
 globalThis.document={documentElement:{lang:'en'}}
 for(const phase of ['reading','planning','running']){
  const html=renderToStaticMarkup(createElement(CopilotProgress,{progress:{phase,since:Date.now()}}))
  assert.match(html,/working-indicator/);assert.match(html,/role="status"/)
 }
 for(const phase of ['waiting_confirmation','waiting_batch','failed']){
  const html=renderToStaticMarkup(createElement(CopilotProgress,{progress:{phase,since:Date.now()}}))
  assert.doesNotMatch(html,/working-indicator/);assert.match(html,/#page=voice-assistant/)
 }
 assert.equal(renderToStaticMarkup(createElement(CopilotProgress,{})),'')
})
