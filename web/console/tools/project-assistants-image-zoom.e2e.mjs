/** The Bot picture viewer, zoomed by touch, mouse and keys in chromium and webkit (BOTS_ARTIFACTS, PLAYWRIGHT_MODULE). */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath,pathToFileURL} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
/** The fixture page and its bundle: the real ImageViewer, nothing else. */
export async function imageZoomBundle(){
 const built=await build({entryPoints:[fileURLToPath(new URL('../src/project-assistants/image-zoom.fixture.tsx',import.meta.url))],bundle:true,write:false,outfile:'zoom.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"','import.meta.env':'{}'}})
 // The console's own viewport: the page itself never zooms (web/console/index.html).
 const html='<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no"><style>body{margin:0;background:#080808;color:#fafafa;font:15px system-ui}</style><link rel="stylesheet" href="/zoom.css"><div id="app"></div><script type="module" src="/zoom.js"></script>'
 return {html,files:new Map(built.outputFiles.map(f=>['/'+path.basename(f.path),f.contents]))}
}
async function serve(){
 const {html,files}=await imageZoomBundle()
 const server=createServer((req,res)=>{
  const name=new URL(req.url,'http://fixture').pathname
  if(files.has(name)){res.setHeader('Content-Type',name.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(name));return}
  if(name.startsWith('/v1/')){res.writeHead(500);res.end('No real API in this fixture');return}
  res.setHeader('Content-Type','text/html');res.end(html)
 })
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
 return {server,origin:`http://127.0.0.1:${server.address().port}`}
}
const near=(a,b,slack,message)=>assert.ok(Math.abs(a-b)<=slack,`${message}: ${a} is not within ${slack} of ${b}`)

/** What is drawn: the zoom the viewer reports and the picture's box on screen. */
const look=page=>page.evaluate(()=>{
 const img=document.querySelector('.pa-image-viewer-stage img'),stage=document.querySelector('.pa-image-viewer-stage')
 const r=img.getBoundingClientRect(),s=stage.getBoundingClientRect()
 return {scale:Number(img.dataset.scale??1),left:r.left,top:r.top,width:r.width,height:r.height,cx:r.left+r.width/2,cy:r.top+r.height/2,
  fitWidth:img.offsetWidth,fitHeight:img.offsetHeight,natural:img.naturalWidth,stage:{left:s.left,top:s.top,width:s.width,height:s.height,cx:s.left+s.width/2,cy:s.top+s.height/2},
  zoomed:stage.hasAttribute('data-zoomed'),open:!!document.querySelector('.pa-image-viewer'),pageScroll:[scrollX,scrollY,document.documentElement.scrollWidth>innerWidth]}
})
// Settled: no spring-back transition is still moving the picture.
const settled=async page=>{
 let last=await look(page)
 for(let i=0;i<40;i++){
  await page.waitForFunction(()=>!document.querySelector('.pa-image-viewer-stage img[data-animate]')||document.querySelector('.pa-image-viewer-stage img').getAnimations().length===0)
  const now=await look(page)
  if(Math.abs(now.width-last.width)<0.5&&Math.abs(now.cx-last.cx)<0.5&&Math.abs(now.cy-last.cy)<0.5)return now
  last=now;await page.waitForFunction(()=>new Promise(r=>requestAnimationFrame(()=>r(true))))
 }
 return last
}

/**
 * Real touches. Chromium takes them through CDP, so they arrive as the browser's own touch
 * and pointer events under touch-action:none. WebKit has no protocol for a second finger, so
 * there the same fingers are dispatched as touch pointer events on what each went down on.
 */
function fingers(page,engine){
 if(engine==='chromium'){
  const cdp=page.context().newCDPSession(page)
  const send=async(type,points)=>(await cdp).send('Input.dispatchTouchEvent',{type,touchPoints:points.map((p,i)=>({x:p.x,y:p.y,id:i+1,radiusX:4,radiusY:4,force:1}))})
  return {start:points=>send('touchStart',points),move:points=>send('touchMove',points),end:()=>send('touchEnd',[])}
 }
 let down=[]
 const fire=(type,points)=>page.evaluate(([type,points])=>{
  // Like pointer capture: a finger's later events go where it went down, even off screen.
  const held=window.fixtureFingers??=new Map()
  for(const [i,p] of points.entries()){
   if(type==='pointerdown')held.set(i,document.elementFromPoint(p.x,p.y)??document.body)
   const target=held.get(i)??document.body
   if(type==='pointerup')held.delete(i)
   target.dispatchEvent(new PointerEvent(type,{pointerId:100+i,pointerType:'touch',isPrimary:i===0,clientX:p.x,clientY:p.y,bubbles:true,cancelable:true,composed:true,button:type==='pointermove'?-1:0,buttons:type==='pointerup'?0:1}))
  }
 },[type,points])
 return {start:async points=>{down=points;await fire('pointerdown',points)},move:async points=>{down=points;await fire('pointermove',points)},end:async()=>{await fire('pointerup',down);down=[]}}
}
/** Two fingers from `gap` apart to `gap*factor` apart around `at`, in steps, then lifted. */
async function pinch(page,engine,at,factor,{gap=80,steps=12,shift={x:0,y:0},hold}={}){
 const f=fingers(page,engine),pair=(g,o)=>[{x:at.x-g/2+o.x,y:at.y+o.y},{x:at.x+g/2+o.x,y:at.y+o.y}]
 await f.start(pair(gap,{x:0,y:0}))
 for(let i=1;i<=steps;i++){const t=i/steps;await f.move(pair(gap*(1+(factor-1)*t),{x:shift.x*t,y:shift.y*t}))}
 const during=hold?await hold():null
 await f.end()
 return during
}
async function drag(page,engine,from,to,steps=10){
 const f=fingers(page,engine)
 await f.start([from])
 for(let i=1;i<=steps;i++)await f.move([{x:from.x+(to.x-from.x)*i/steps,y:from.y+(to.y-from.y)*i/steps}])
 await f.end()
}
async function doubleTap(page,engine,at){
 if(engine==='chromium'){await page.touchscreen.tap(at.x,at.y);await page.touchscreen.tap(at.x,at.y);return}
 const f=fingers(page,engine)
 for(let i=0;i<2;i++){await f.start([at]);await f.end()}
}
async function tap(page,engine,at){
 if(engine==='chromium'){await page.touchscreen.tap(at.x,at.y);return}
 const f=fingers(page,engine);await f.start([at]);await f.end()
}

/** Every gesture in each engine and size; throws on the first behaviour that is wrong. */
export async function runImageZoomAcceptance({artifacts,playwrightModule,engines=['chromium','webkit'],sizes=[['phone',390,844],['desktop',1280,800]]}){
 await mkdir(artifacts,{recursive:true})
 const pw=await import(pathToFileURL(playwrightModule).href),results=[]
 const {server,origin}=await serve()
try{for(const engine of engines){
 const browser=await pw[engine].launch({headless:true})
 try{for(const [label,width,height] of sizes){
  const context=await browser.newContext({viewport:{width,height},hasTouch:true})
  const page=await context.newPage(),errors=[]
  page.on('pageerror',e=>errors.push(String(e)))
  await page.goto(origin+'/?lang=en')
  const viewer=page.getByRole('dialog',{name:'design-board.png'}),img=viewer.getByRole('img',{name:'design-board.png'})
  await img.waitFor()
  await page.waitForFunction(()=>document.querySelector('.pa-image-viewer-stage img')?.complete)
  const fit=await settled(page)
  assert.equal(fit.scale,1,'The picture opens fitted');assert.equal(fit.zoomed,false)
  const max=Math.max(8,fit.natural/fit.fitWidth*2)
  const shot=name=>page.screenshot({path:path.join(artifacts,`${engine}-${label}-${name}.png`)})
  await shot('1-fit')

  // A single tap on the picture no longer jumps to actual size.
  await tap(page,engine,{x:fit.cx,y:fit.cy});await page.waitForTimeout(350)
  assert.equal((await settled(page)).scale,1,'One tap on the picture leaves it fitted')
  assert.equal((await look(page)).open,true)

  // Pinch out around a point off-center: the picture grows and the point under the fingers stays put.
  const at={x:fit.left+fit.width*0.3,y:fit.cy}
  const ratio=(at.x-fit.left)/fit.width
  await pinch(page,engine,at,2.5)
  const pinched=await settled(page)
  assert.ok(pinched.scale>2&&pinched.scale<3,`A 2.5x pinch zooms about 2.5x, got ${pinched.scale}`)
  assert.equal(pinched.zoomed,true)
  near(pinched.left+pinched.width*ratio,at.x,6,'The pinched point stays under the fingers')
  assert.deepEqual(pinched.pageScroll,[0,0,false],'The page itself neither scrolls nor zooms')
  await shot('2-pinched')

  // One finger pans the zoomed picture, and only as far as its edge.
  await drag(page,engine,{x:fit.stage.cx,y:fit.stage.cy},{x:fit.stage.cx-120,y:fit.stage.cy})
  const panned=await settled(page)
  near(panned.left,pinched.left-120,4,'A drag moves the zoomed picture with the finger')
  near(panned.scale,pinched.scale,0.001,'A drag does not change the zoom')
  await shot('3-panned')
  await drag(page,engine,{x:fit.stage.left+20,y:fit.stage.cy},{x:fit.stage.left+fit.stage.width-20,y:fit.stage.cy},6)
  await drag(page,engine,{x:fit.stage.left+20,y:fit.stage.cy},{x:fit.stage.left+fit.stage.width-20,y:fit.stage.cy},6)
  const edge=await settled(page)
  near(edge.left,fit.stage.left,1,'A pan stops at the picture\'s left edge')

  // Past the top limit the picture stretches while held and springs back to the limit on release.
  let held=null
  await pinch(page,engine,{x:fit.stage.cx,y:fit.stage.cy},12,{gap:40,steps:16,hold:async()=>held=await look(page)})
  const stretched=held.scale
  assert.ok(held.scale>Math.max(8,held.natural/held.fitWidth*2)+0.01,`Held past the limit (${max.toFixed(2)}) it stretches, got ${held.scale}`)
  const top=await settled(page)
  near(top.scale,Math.max(8,top.natural/top.fitWidth*2),0.01,'Released past the limit it springs back to the limit')

  // A tap on the empty stage while zoomed in does not close.
  const blank=await page.evaluate(()=>{
   const s=document.querySelector('.pa-image-viewer-stage').getBoundingClientRect()
   for(let y=s.top+4;y<s.bottom-4;y+=8)for(const x of [s.left+4,s.left+s.width/2]){if(document.elementFromPoint(x,y)?.classList.contains('pa-image-viewer-stage'))return {x,y}}
   return null
  })
  // Zoomed out to just above fit, the stage around the picture is empty again.
  await page.keyboard.press('0');const back=await settled(page)
  assert.equal(back.scale,1,'0 returns to fit')
  await pinch(page,engine,{x:fit.cx,y:fit.cy},1.1)
  const little=await settled(page);assert.ok(little.scale>1.05,`A small pinch zooms a little, got ${little.scale}`)
  const empty=await page.evaluate(()=>{
   const s=document.querySelector('.pa-image-viewer-stage').getBoundingClientRect()
   for(let y=s.top+4;y<s.bottom-4;y+=8)for(const x of [s.left+4,s.left+s.width/2]){if(document.elementFromPoint(x,y)?.classList.contains('pa-image-viewer-stage'))return {x,y}}
   return null
  })
  assert.ok(empty||blank,'There is empty stage to tap at this size')
  await tap(page,engine,empty??blank);await page.waitForTimeout(350)
  assert.equal((await look(page)).open,true,'A tap on the empty stage while zoomed in does not close')
  assert.equal(await page.evaluate(()=>window.fixtureZoom.closes),0)

  // Pinched in below fit, it gives way and springs back to fit.
  held=null
  await pinch(page,engine,{x:fit.cx,y:fit.cy},0.3,{gap:200,hold:async()=>held=await look(page)})
  assert.ok(held.scale<1,`Held below fit it stretches smaller, got ${held.scale}`)
  assert.equal((await settled(page)).scale,1,'Released below fit it springs back to fit')

  // Double tap zooms 2.5x into the tapped spot; another double tap fits again.
  const spot={x:fit.left+fit.width*0.7,y:fit.top+fit.height*0.4}
  await doubleTap(page,engine,spot)
  const doubled=await settled(page)
  near(doubled.scale,2.5,0.01,'A double tap zooms to 2.5x')
  near(doubled.left+doubled.width*0.7,spot.x,6,'A double tap zooms into the tapped spot horizontally')
  await page.waitForTimeout(350)
  await doubleTap(page,engine,{x:fit.stage.cx,y:fit.stage.cy})
  assert.equal((await settled(page)).scale,1,'A second double tap fits again')
  assert.equal((await look(page)).open,true)

  // Keys and ctrl/⌘ + wheel zoom too.
  await page.keyboard.press('+');assert.ok((await settled(page)).scale>1.2,'+ zooms in')
  await page.keyboard.press('-');near((await settled(page)).scale,1,0.01,'- zooms back out')
  await page.mouse.move(fit.cx,fit.cy);await page.keyboard.down('Control');await page.mouse.wheel(0,-200);await page.keyboard.up('Control')
  assert.ok((await settled(page)).scale>1.5,'ctrl + wheel zooms in')
  assert.deepEqual((await look(page)).pageScroll,[0,0,false],'ctrl + wheel never zooms the page')
  await page.keyboard.press('0');await settled(page);await page.waitForTimeout(350)

  // At fit, a tap on the empty stage closes.
  const outside=await page.evaluate(()=>{
   const s=document.querySelector('.pa-image-viewer-stage').getBoundingClientRect(),i=document.querySelector('.pa-image-viewer-stage img').getBoundingClientRect()
   return i.top-s.top>8?{x:s.left+s.width/2,y:s.top+4}:{x:s.left+4,y:s.top+s.height/2}
  })
  await tap(page,engine,outside)
  await viewer.waitFor({state:'detached'})
  assert.equal(await page.evaluate(()=>window.fixtureZoom.closes),1,'A tap beside the fitted picture closes')

  // Escape and the close button close; download is unaffected by zooming.
  await page.getByRole('button',{name:'Reopen'}).click();await img.waitFor()
  await page.waitForFunction(()=>document.querySelector('.pa-image-viewer-stage img')?.complete)
  await pinch(page,engine,{x:fit.stage.cx,y:fit.stage.cy},2)
  assert.equal(await viewer.getByRole('button',{name:'Download file'}).isEnabled(),true)
  await page.keyboard.press('Escape');await viewer.waitFor({state:'detached'})
  await page.getByRole('button',{name:'Reopen'}).click();await img.waitFor()
  assert.equal((await settled(page)).scale,1,'A reopened picture starts fitted')
  await viewer.getByRole('button',{name:'Close'}).click();await viewer.waitFor({state:'detached'})
  assert.equal(await page.evaluate(()=>window.fixtureZoom.closes),3)

  await page.emulateMedia({reducedMotion:'reduce'})
  await page.getByRole('button',{name:'Reopen'}).click();await img.waitFor()
  await page.keyboard.press('+')
  assert.equal(await page.evaluate(()=>getComputedStyle(document.querySelector('.pa-image-viewer-stage img')).transitionDuration),'0s','Reduced motion springs back without animating')

  assert.deepEqual(errors,[])
  results.push({engine,label,width,height,max:Number(max.toFixed(3)),pinched:pinched.scale,stretched,belowFit:held.scale,top:top.scale,doubled:doubled.scale})
  console.log(engine,label,'passed');await context.close()
 }}finally{await browser.close()}
}}finally{server.closeAllConnections();await new Promise(resolve=>server.close(resolve))}
 await writeFile(path.join(artifacts,'image-zoom-evidence.json'),JSON.stringify(results,null,2)+'\n')
 return results
}

if(process.argv[1]===fileURLToPath(import.meta.url)){
 if(process.argv.includes('--serve')){const {origin}=await serve();console.log(origin);await new Promise(()=>{})}
 const artifacts=process.env.BOTS_ARTIFACTS
 if(!artifacts||!process.env.PLAYWRIGHT_MODULE)throw Error('Set BOTS_ARTIFACTS and installed PLAYWRIGHT_MODULE')
 console.log(JSON.stringify(await runImageZoomAcceptance({artifacts,playwrightModule:process.env.PLAYWRIGHT_MODULE})))
}
