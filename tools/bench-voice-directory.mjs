#!/usr/bin/env node
// Controlled delayed-transcript probe. It measures the directory/snapshot's
// dependency on transcript I/O; it does not reproduce production network time.
import {execFileSync} from 'node:child_process'
import {writeFileSync,unlinkSync} from 'node:fs'
import {fileURLToPath,pathToFileURL} from 'node:url'
import {dirname,join} from 'node:path'
import {performance} from 'node:perf_hooks'
const root=dirname(dirname(fileURLToPath(import.meta.url)))
const revision=process.argv[2]
if(!revision||!/^[0-9a-f]{40}$/.test(revision))throw new Error('Pass the exact baseline commit as the first argument.')
const file=join(root,'web/console/src/realtime',`.voice-directory-bench-${process.pid}.ts`)
writeFileSync(file,execFileSync('git',['show',`${revision}:web/console/src/realtime/copilot-tools.ts`],{cwd:root}),{flag:'wx'})
try {
 const before=await import(pathToFileURL(file).href)
 const after=await import(pathToFileURL(join(root,'web/console/src/realtime/copilot-tools.ts')).href)
 const rows=Array.from({length:8},(_,i)=>({id:`%${i}`,label:`Fixture ${i}`,assistant:'codex',state:'working',work_state:'working'}))
 const payload=JSON.stringify({entries:[{role:'assistant',text:'x'.repeat(50000)}]})
 const stats={}
 for(const action of ['directory','snapshot']){
  const samples={before:[],after:[]};const counts={before:[],after:[]};const bytes={before:[],after:[]}
  for(let run=0;run<33;run++)for(const side of run%2?['after','before']:['before','after']){
   const api=side==='before'?before:after;let requests=0,totalBytes=0
   const deps={fetch:async path=>{requests++;let body
    if(path==='/v1/sessions')body=JSON.stringify({sessions:rows})
    else if(path.startsWith('/v1/transcript?')){await new Promise(r=>setTimeout(r,30));body=payload}
    else throw new Error(`Unexpected request ${path}`)
    totalBytes+=Buffer.byteLength(body);return new Response(body,{headers:{'Content-Type':'application/json'}})
   },sleep:async()=>{},now:()=>1000}
   const start=performance.now()
   if(action==='directory')await api.runCopilotTool('list_sessions','{}',deps);else await api.sessionsSnapshot(deps)
   const elapsed=performance.now()-start
   if(run>=3){samples[side].push(elapsed);counts[side].push(requests);bytes[side].push(totalBytes)}
  }
  stats[action]=Object.fromEntries(['before','after'].map(side=>{
   const s=samples[side].sort((a,b)=>a-b);return [side,{runs:s.length,p50_ms:s[Math.ceil(s.length*.5)-1],p95_ms:s[Math.ceil(s.length*.95)-1],requests:[...new Set(counts[side])],response_bytes:[...new Set(bytes[side])]}]
  }))
 }
 console.log(JSON.stringify({baseline:revision,node:process.version,fixture:{sessions:8,transcript_delay_ms:30,assistant_text_bytes:50000,warmups:3,interleaved:true},stats},null,2))
}finally{unlinkSync(file)}
