/** A picture-only group line sent over the Cloud relay words, at phone and desktop widths. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath,pathToFileURL} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
const {BOTS_CLOUD_ORIGIN:cloud,BOTS_ARTIFACTS:artifacts,PLAYWRIGHT_MODULE:module}=process.env
assert.ok(cloud&&artifacts&&module)
await mkdir(artifacts,{recursive:true})
const root=fileURLToPath(new URL('..',import.meta.url))
const bundle=await build({entryPoints:[path.join(root,'tools/fixtures/project-assistants-group-pictures.tsx')],bundle:true,write:false,outfile:'pictures.js',format:'esm',platform:'browser',target:'es2022',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"'}})
const files=new Map(bundle.outputFiles.map(file=>['/'+path.basename(file.path),file.contents])),traffic=[]
const server=createServer(async(req,res)=>{
 try{
  const url=new URL(req.url,'http://fixture')
  if(url.pathname.startsWith('/relay/')){
   const chunks=[];for await(const chunk of req)chunks.push(chunk);const raw=Buffer.concat(chunks).toString(),word=url.pathname.slice(7)
   const record={word,body:raw?JSON.parse(raw):null};traffic.push(record)
   const answer=await fetch(cloud,{method:'POST',body:JSON.stringify({type:word,session:'__clawdline_machine__',request:req.headers['idempotency-key']??'read-'+crypto.randomUUID(),...record.body})})
   const payload=await answer.json();record.status=answer.status;res.writeHead(answer.status,{'Content-Type':'application/json'});res.end(JSON.stringify(payload.body??{error:payload.error}));return
  }
  if(url.pathname.startsWith('/catalog/')){res.setHeader('Content-Type','application/json');res.end(await readFile(path.join(root,'public/catalogs',path.basename(url.pathname))));return}
  if(files.has(url.pathname)){res.setHeader('Content-Type',url.pathname.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(url.pathname));return}
  res.setHeader('Content-Type','text/html');res.end('<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body,#app{height:100%;margin:0}</style><link rel="stylesheet" href="/pictures.css"><div id="app"></div><script type="module" src="/pictures.js"></script></html>')
 }catch(error){res.writeHead(500);res.end(JSON.stringify({error:String(error)}))}
})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const origin=`http://127.0.0.1:${server.address().port}`,pw=await import(pathToFileURL(module).href),evidence=[]
// A 96x64 synthetic picture in three colour blocks, so the preview is visibly a picture.
const picture=Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAGAAAABACAIAAABqVuVZAAAAeklEQVR42u3QMQ0AIAwAMCTuRtOUIAI53EjAAfcgTaqgLXKVskeU0gQJEiRIkCBBggQJEiRIkCBBggQJEiRIkCBBggQJEiRIkCBBggQJEiRIkCBBggQJEvRcUJ/JhSBBggQJEiRIkCAECRIkSJAgQYIQJEiQIEGCvnAAOMhQd3tmNc8AAAAASUVORK5CYII=','base64')
try{
 const browser=await pw.chromium.launch({headless:true,args:['--disable-features=OmniboxEverywhere']})
 try{
  for(const [name,language,width,height] of [['phone-390','zh-Hant',390,780],['desktop-1440','zh-Hant',1440,900]]){
   const context=await browser.newContext({viewport:{width,height},hasTouch:width===390,deviceScaleFactor:2}),page=await context.newPage(),errors=[],start=traffic.length
   page.on('pageerror',error=>errors.push(String(error)))
   const click=locator=>width===390?locator.tap():locator.click()
   await page.goto(`${origin}/?lang=${language}`)
   await click(page.locator('[data-group-key]').first())
   const room=page.locator('.pa-room-layout');await room.waitFor({state:'visible'})
   await room.locator('input[type=file]').setInputFiles({name:'synthetic.png',mimeType:'image/png',buffer:picture})
   await room.locator('.pa-picture-preview img').waitFor({state:'visible'})
   await page.screenshot({path:path.join(artifacts,`${name}-1-preview.png`)})
   await click(room.locator('button',{hasText:'↑'}))
   await room.locator('.pa-message-pictures img').first().waitFor({state:'visible',timeout:15000})
   await page.waitForFunction(()=>[...document.querySelectorAll('.pa-message-pictures img')].every(img=>img.complete&&img.naturalWidth>0))
   await page.waitForTimeout(300)
   await page.screenshot({path:path.join(artifacts,`${name}-2-sent.png`)})
   const sends=traffic.slice(start).filter(r=>r.word==='project-assistants.group-send')
   assert.equal(sends.length,1,'one group-send carried')
   assert.equal(Object.hasOwn(sends[0].body,'text'),false,'the picture-only line travels without text')
   assert.equal(sends[0].status,200,'the machine accepted the picture-only line')
   assert.deepEqual(errors,[])
   evidence.push({name,width,height,send_status:sends[0].status,send_keys:Object.keys(sends[0].body).sort()})
   await context.close()
  }
 }finally{await browser.close()}
}finally{server.close()}
await writeFile(path.join(artifacts,'evidence.json'),JSON.stringify(evidence,null,2)+'\n')
console.log(JSON.stringify(evidence))
