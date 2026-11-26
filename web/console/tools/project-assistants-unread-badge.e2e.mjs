/** Reading a Bot chat moves the status-bar "All Bots" badge with the roster, while Bots is open and after it closes. */
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {mkdir} from 'node:fs/promises'
import {fileURLToPath,pathToFileURL} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
const artifacts=process.env.BOTS_ARTIFACTS
if(!artifacts||!process.env.PLAYWRIGHT_MODULE)throw Error('Set BOTS_ARTIFACTS and installed PLAYWRIGHT_MODULE')
await mkdir(artifacts,{recursive:true})
const built=await build({entryPoints:[fileURLToPath(new URL('../src/project-assistants/unread-badge.fixture.tsx',import.meta.url))],bundle:true,write:false,outfile:'unread.js',format:'esm',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"','import.meta.env':'{}'},external:['/hero-orchestration-v4-task-clinic.webp']})
const files=new Map(built.outputFiles.map(f=>['/'+path.basename(f.path),f.contents]))
const server=createServer((req,res)=>{
  const name=new URL(req.url,'http://fixture').pathname
  if(files.has(name)){res.setHeader('Content-Type',name.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(name));return}
  if(name.startsWith('/v1/')){res.writeHead(500);res.end('No real API in this fixture');return}
  res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0;background:#080808;color:#fafafa;font:15px system-ui}.pane-list{height:80vh}footer{position:fixed;bottom:0;padding:12px}</style><link rel="stylesheet" href="/unread.css"><div id="app"></div><script type="module" src="/unread.js"></script>')
})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const browser=await pw.chromium.launch({headless:true})
// The orb's label ends with its unread count when anything is counted.
const badgeUnread=page=>page.locator('.pa-dock').getAttribute('aria-label').then(label=>Number(/(\d+)$/.exec(label??'')?.[1]??0))
const waitUnread=(page,expected,message)=>page.waitForFunction(expected=>Number(/(\d+)$/.exec(document.querySelector('.pa-dock')?.getAttribute('aria-label')??'')?.[1]??0)===expected,expected,{timeout:3000})
  .catch(async()=>assert.fail(`${message}: badge shows ${await badgeUnread(page)}, roster expects ${expected}`))
const failures=[]
try{
  for(const [language,width,height] of [['en',1280,900],['zh-Hant',390,844]]){
    const page=await browser.newPage({viewport:{width,height}}),errors=[]
    page.on('pageerror',e=>errors.push(String(e)))
    await page.route('**/v1/**',route=>route.abort())
    await page.goto(origin+'/?lang='+language)
    await page.locator('.pa-dock').waitFor()
    await waitUnread(page,2,'The badge starts from the source roster')
    await page.locator('.pa-dock').click()
    const row=page.locator(`.pa-bot-row[data-bot-key='${JSON.stringify(['unread','bot-0'])}']`)
    await row.waitFor()
    assert.equal(await row.getAttribute('data-unread'),'true')
    await row.click()
    await page.locator('.pa-chat-scroll').waitFor()
    await page.locator('.pa-chat-scroll').focus()
    if(width<=700)await page.getByRole('button',{name:language==='en'?'Back to Bots':'返回 Bots',exact:true}).click()
    await row.waitFor()
    await page.waitForFunction(selector=>!document.querySelector(selector)?.hasAttribute('data-unread'),`.pa-bot-row[data-bot-key='${JSON.stringify(['unread','bot-0'])}']`,{timeout:3000})
    await page.waitForTimeout(300)
    // The status bar sits under the open window; caption the screenshot with what it says.
    await page.evaluate(()=>{const note=document.createElement('div');note.id='badge-caption';note.textContent='Status bar badge: '+document.querySelector('.pa-dock')?.getAttribute('aria-label');note.style.cssText='position:fixed;left:0;right:0;bottom:0;z-index:2000;padding:8px 12px;background:#b00020;color:#fff;font:600 14px system-ui';document.body.append(note)})
    await page.screenshot({path:path.join(artifacts,`unread-badge-open-${width}.png`)})
    await page.evaluate(()=>document.querySelector('#badge-caption')?.remove())
    console.log(`${width} open: roster unread 0, badge label "${await page.locator('.pa-dock').getAttribute('aria-label')}"`)
    await waitUnread(page,0,'While Bots is open the badge follows the roster that was just read').catch(error=>failures.push(`${width}: ${error.message}`))
    await page.getByRole('button',{name:language==='en'?'Collapse Bots':'收起 Bots',exact:true}).click()
    await page.locator('.pa-window').waitFor({state:'detached'})
    const closed=await badgeUnread(page)
    await page.screenshot({path:path.join(artifacts,`unread-badge-closed-${width}.png`)})
    if(closed!==0)failures.push(`${width}: Closing Bots shows ${closed} instead of 0`)
    assert.deepEqual(errors,[])
    await page.close()
    console.log(`checked ${language} ${width}`)
  }
}finally{await browser.close();server.close()}
assert.deepEqual(failures,[],'The status-bar badge follows the roster after a read')
console.log('ok')
