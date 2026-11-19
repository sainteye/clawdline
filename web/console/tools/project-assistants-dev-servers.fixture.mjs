/** Local synthetic UI fixture. No daemon, Cloud route, model or real command runs. */
import {createServer} from 'node:http'
import {createHash} from 'node:crypto'
import {readFile,mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath} from 'node:url'
import path from 'node:path'
import assert from 'node:assert/strict'
import {build} from 'esbuild'
const artifacts=process.env.BOTS_ARTIFACTS
assert.ok(artifacts,'BOTS_ARTIFACTS must name the task-owned evidence directory')
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL('..',import.meta.url))
const built=await build({entryPoints:[path.join(root,'tools/fixtures/project-assistants-dev-servers.tsx')],bundle:true,write:false,outfile:'servers.js',format:'esm',platform:'browser',target:'es2022',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'}})
const files=new Map(built.outputFiles.map(f=>['/'+path.basename(f.path),f.contents])),traffic=[]
const hashes={}
for(const file of ['tools/fixtures/project-assistants-dev-servers.tsx','src/project-assistants/DevServers.tsx','src/project-assistants/dev-servers.css','src/project-assistants/words.ts','src/project-assistants/MessageMenu.tsx','src/project-assistants/message-reactions.css','src/project-assistants/styles.css','src/cloud/project-assistants.ts','public/catalogs/en.json','public/catalogs/zh-Hant.json']){const bytes=await readFile(path.join(root,file));hashes[file]={bytes:bytes.length,sha256:createHash('sha256').update(bytes).digest('hex')}}
for(const file of built.outputFiles)hashes[path.basename(file.path)]={bytes:file.contents.length,sha256:createHash('sha256').update(file.contents).digest('hex')}
await writeFile(path.join(artifacts,'devserver-ui-built-inputs.json'),JSON.stringify(hashes,null,2)+'\n')
const preview=createServer((_req,res)=>{res.setHeader('Content-Type','text/plain');res.end('Synthetic owned preview fixture')})
await new Promise(resolve=>preview.listen(0,'127.0.0.1',resolve))
const previewPort=preview.address().port,previewOrigin=`http://127.0.0.1:${previewPort}`
const row=(id,name,state)=>({id,name,state,command:'synthetic-watch --foreground',folder:'/synthetic/project',attempt:'synthetic-attempt',fingerprint:'f'.repeat(64),revision:1,ports_known:true,ports:state==='running'?[{port:previewPort,bind:'127.0.0.1',local:previewOrigin,remote:[]}]:[]})
const network={...row('network-server','Network preview','running'),ports:[{port:43124,bind:'0.0.0.0',local:'http://127.0.0.1:43124',remote:['http://192.0.2.23:43124']}]}
const unknown={...row('unknown-server','Unconfirmed watcher','running'),ports_known:false,ports:[]}
const rows=new Map([['Builder',[row('server-one','Web preview','running'),row('server-two','Watch build','stopped'),network,unknown]],['Other Bot',[row('other-server','Other watcher','running')]]])
let loseNext=false,invalidNext=false,holdRead=false,releaseRead
const json=(res,status,body)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(body))}
const failure=(res,code)=>json(res,409,{contract_version:'project-assistants.v1',error:{code,field:'synthetic-control',retry_after_seconds:0}})
const server=createServer(async(req,res)=>{
 try{
  const url=new URL(req.url,'http://synthetic.invalid')
  if(['/fixture/copy','/fixture/open'].includes(url.pathname)&&req.method==='POST'){
   const chunks=[];for await(const chunk of req)chunks.push(chunk)
   traffic.push({fixture:url.pathname,...JSON.parse(Buffer.concat(chunks))});await writeFile(path.join(artifacts,'devserver-ui-synthetic-traffic.json'),JSON.stringify(traffic,null,2)+'\n');json(res,200,{recorded:true});return
  }
  if(url.pathname==='/fixture/lose-next'&&req.method==='POST'){loseNext=true;json(res,200,{ready:true});return}
  if(url.pathname==='/fixture/invalid-next'&&req.method==='POST'){invalidNext=true;json(res,200,{ready:true});return}
  if(url.pathname==='/fixture/hold-read'){holdRead=true;json(res,200,{ready:true});return}
  if(url.pathname==='/fixture/release-read'){releaseRead?.();releaseRead=undefined;json(res,200,{released:true});return}
  if(url.pathname==='/fixture/add-server'){const list=rows.get('Builder');if(!list.some(r=>r.id==='late-server'))list.push(row('late-server','New watcher','running'));json(res,200,{added:true});return}
  if(url.pathname==='/v1/project-assistants/status'){json(res,200,{contract_version:'project-assistants.v1',sources:[{machine_id:'synthetic-machine',access:'interactive',state:'online',fresh:true,readable:true,writable:true,observed_at:'',capabilities:[]}]});return}
  const route=url.pathname.match(/^\/v1\/project-assistants\/([^/]+)\/dev-servers(?:\/([^/]+)\/(logs|start|restart|stop|remove))?$/)
  if(route){
   const [,encoded,id,action]=route,bot=decodeURIComponent(encoded),list=rows.get(bot)??[],item=list.find(r=>r.id===id)
   const record={method:req.method,bot,id,action};traffic.push(record)
   if(req.method==='GET'){
    const answer=structuredClone({contract_version:'project-assistants.v1',servers:id?[]:list,...id?{server:item,log:{text:'Synthetic log <script>never executes</script>\nWatcher ready',truncated:true}}:{}})
    if(holdRead&&!id){holdRead=false;record.held=true;await new Promise(resolve=>{releaseRead=resolve})}
    json(res,200,answer)
   }
   else{
    const chunks=[];for await(const chunk of req)chunks.push(chunk);const body=JSON.parse(Buffer.concat(chunks));record.body=body
    if(!item||body.id!==id||body.key.machine_id!=='synthetic-machine'||body.key.bot_id!==bot||body.fingerprint!==item.fingerprint||body.attempt!==item.attempt||body.revision!==item.revision||Object.keys(body).some(k=>!['key','id','fingerprint','attempt','revision','idempotency_key'].includes(k)))failure(res,'pa_stale_version')
    else{
     if(action==='remove')rows.set(bot,list.filter(r=>r!==item))
     else{item.state=action==='stop'?'stopped':'running';item.attempt=body.idempotency_key;item.revision++}
     record.applied=true
     if(loseNext){loseNext=false;record.replyLost=true;res.destroy()}
     else if(invalidNext){invalidNext=false;record.replyInvalid=true;res.writeHead(200,{'Content-Type':'application/json'});res.end('{"contract_version":')}
     else json(res,200,{contract_version:'project-assistants.v1',servers:rows.get(bot)??[]})
    }
   }
   await writeFile(path.join(artifacts,'devserver-ui-synthetic-traffic.json'),JSON.stringify(traffic,null,2)+'\n');return
  }
  if(url.pathname.startsWith('/catalog/')){const tag=path.basename(url.pathname);if(!['en.json','zh-Hant.json'].includes(tag)){res.writeHead(404);res.end();return}res.setHeader('Content-Type','application/json');res.end(await readFile(path.join(root,'public/catalogs',tag)));return}
  if(files.has(url.pathname)){res.setHeader('Content-Type',url.pathname.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(url.pathname));return}
  if(url.pathname!=='/'){res.writeHead(404);res.end();return}
  res.setHeader('Content-Type','text/html');res.end('<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#080808}.pa-server-fixture{max-width:400px;padding:20px;margin:auto}nav{display:flex;flex-wrap:wrap;gap:8px;padding:12px}</style><link rel="stylesheet" href="/servers.css"><div id="app"></div><script type="module" src="/servers.js"></script></html>')
 }catch(error){res.writeHead(500);res.end(String(error))}
})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const address={pid:process.pid,origin:`http://127.0.0.1:${server.address().port}`,previewOrigin,synthetic:true}
await writeFile(path.join(artifacts,'devserver-ui-fixture-address.json'),JSON.stringify(address,null,2)+'\n');console.log(JSON.stringify(address))
const stop=()=>server.close(()=>preview.close(()=>process.exit(0)));process.once('SIGTERM',stop);process.once('SIGINT',stop)
