package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Actual Bots/room navigation over authenticated HTTP and SQLite. The only
// provider is the in-process scripted fixture; no native process or model runs.
func TestGroupMemberNavigationBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_GROUP_NAV_ARTIFACTS")
	if dir == "" {
		t.Skip("set CLAWDLINE_GROUP_NAV_ARTIFACTS and PLAYWRIGHT_MODULE for room/member navigation")
	}
	f := newGroupHTTPFixture(t)
	f.create(t)
	for i := 0; i < 6; i++ {
		status, body := f.request(t, http.MethodPost, "/v1/project-assistants/rooms/groups/room/messages", "person", contract.ProjectAssistantGroupSendRequest{Command: f.command(pa.SendOperation, fmt.Sprintf("navigation-seed-%d", i)), Text: fmt.Sprintf("Stored discussion line %d with enough context to exercise the reader's position.", i)})
		if status != http.StatusOK {
			t.Fatalf("seed room: %d %s", status, body)
		}
		f.rooms.Idle()
	}
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bridge := cloudops.Bridge{MachineID: "machine", Router: groupUIRouter{origin: f.server.URL, token: "person"}, AllowCommands: func() bool { return true }}
		answer := bridge.Handle(r.Context(), cloudops.Command{Channel: "ctl/machine", Class: cloudops.ClassCtl, Sender: "synthetic-navigation-viewer", Sequence: 1, Plaintext: data})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.Status)
		_, _ = w.Write(answer.Payload)
	}))
	defer cloud.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "../../../web/console/tools/project-assistants-group-navigation.e2e.mjs")
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+f.server.URL, "BOTS_CLOUD_ORIGIN="+cloud.URL, "BOTS_ARTIFACTS="+dir)
	out, err := cmd.CombinedOutput()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), out, 0600)
	if err != nil {
		t.Fatalf("room/member navigation: %v\n%s", err, out)
	}
	t.Log(string(out))
}

// The production Bots host and source footer must share the visible viewport
// with a room portal. All history and replies come from the local SQLite fixture.
func TestGroupComposerViewportBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_GROUP_LAYOUT_ARTIFACTS")
	if dir == "" {
		t.Skip("set CLAWDLINE_GROUP_LAYOUT_ARTIFACTS and PLAYWRIGHT_MODULE")
	}
	f := newGroupHTTPFixture(t)
	f.create(t)
	for i := 0; i < 12; i++ {
		status, body := f.request(t, http.MethodPost, "/v1/project-assistants/rooms/groups/room/messages", "person", contract.ProjectAssistantGroupSendRequest{Command: f.command(pa.SendOperation, fmt.Sprintf("layout-%d", i)), Text: fmt.Sprintf("Synthetic discussion %d — retain the original text 👍", i)})
		if status != http.StatusOK {
			t.Fatalf("seed: %d %s", status, body)
		}
		f.rooms.Idle()
		if os.Getenv("CLAWDLINE_GROUP_LAYOUT_SHORT") == "1" {
			break
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-e", groupComposerViewportScript)
	cmd.Dir = "../../../web"
	cmd.Env = append(os.Environ(), "BOTS_ORIGIN="+f.server.URL, "BOTS_ARTIFACTS="+dir)
	out, err := cmd.CombinedOutput()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), out, 0600)
	if err != nil {
		t.Fatalf("group composer viewport: %v\n%s", err, out)
	}
	t.Log(string(out))
}

const groupComposerViewportScript = `
import assert from 'node:assert/strict'
import {createServer} from 'node:http'
import {readFile,mkdir,writeFile} from 'node:fs/promises'
import {execFileSync} from 'node:child_process'
import {pathToFileURL} from 'node:url'
import path from 'node:path'
import {build} from 'esbuild'
const root=path.resolve('console'),artifacts=process.env.BOTS_ARTIFACTS,baseline=process.env.BOTS_LAYOUT_BASELINE
await mkdir(artifacts,{recursive:true})
const host=path.join(root,'tools/fixtures/project-assistants-message-menu-host.tsx')
const code='import "../../src/legacy/tokens.css";\n'+await readFile(host,'utf8')
const bundle=await build({stdin:{contents:code,resolveDir:path.dirname(host),loader:'tsx'},bundle:true,write:false,outfile:'host.js',format:'esm',platform:'browser',target:'es2022',jsx:'automatic',define:{'process.env.NODE_ENV':'"development"',TEMPLATE_MACHINE:'"machine"'},plugins:baseline?[{name:'baseline',setup(p){p.onLoad({filter:/project-assistants\/(GroupRoom\.tsx|styles\.css)$/},a=>({contents:execFileSync('git',['show',baseline+':'+path.relative(path.resolve(root,'../..'),a.path)],{encoding:'utf8'}),loader:a.path.endsWith('.css')?'css':'tsx',resolveDir:path.dirname(a.path)}))}}]:[]})
const files=new Map(bundle.outputFiles.map(f=>['/'+path.basename(f.path),f.contents])),writes=[]
const server=createServer(async(req,res)=>{try{
 const u=new URL(req.url,'http://fixture')
 if(u.pathname.startsWith('/v1/')){const chunks=[];for await(const c of req)chunks.push(c);const body=Buffer.concat(chunks);if(req.method!=='GET')writes.push({path:u.pathname,body:JSON.parse(body)});const h={Authorization:'Bearer person'};for(const k of ['content-type','idempotency-key'])if(req.headers[k])h[k]=req.headers[k];const a=await fetch(process.env.BOTS_ORIGIN+u.pathname+u.search,{method:req.method,headers:h,...body.length?{body}:{}});res.writeHead(a.status,{'Content-Type':'application/json'});res.end(Buffer.from(await a.arrayBuffer()));return}
 if(u.pathname.startsWith('/catalog/')){res.setHeader('Content-Type','application/json');res.end(await readFile(path.join(root,'public/catalogs',path.basename(u.pathname))));return}
 if(files.has(u.pathname)){res.setHeader('Content-Type',u.pathname.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(u.pathname));return}
 res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css"><style>html,body,#app{height:100%;margin:0;background:#080808}</style><div id="app"></div><script type="module" src="/host.js"></script>')
}catch(e){res.writeHead(500);res.end(String(e))}})
await new Promise(r=>server.listen(0,'127.0.0.1',r))
const pw=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href),results=[]
try{for(const engine of (process.env.BOTS_ENGINES??'chromium,webkit').split(',')){
 const browser=await pw[engine].launch({headless:true,...engine==='chromium'?{args:['--disable-features=OmniboxEverywhere']}:{}})
 try{for(const width of (process.env.BOTS_WIDTHS??'390,320,1440').split(',').map(Number)){
  const context=await browser.newContext({viewport:{width,height:780},hasTouch:width<700,reducedMotion:width===320?'reduce':'no-preference'}),page=await context.newPage(),errors=[]
  page.on('pageerror',e=>errors.push(String(e)))
  const tag=engine+'-'+width,click=loc=>width<700?loc.tap():loc.click()
  try{
   await page.goto('http://127.0.0.1:'+server.address().port+'/?lang=zh-Hant');await click(page.locator('.pa-orb'));await click(page.locator('[data-group-key]').first())
   const room=page.locator('.pa-room-layout'),text=room.locator('textarea'),send=room.locator('.pa-send')
   await room.locator('[data-room-seq]').last().waitFor();await text.fill('Synthetic reply 👍')
   await page.waitForFunction(()=>document.querySelector('.pa-window').getAnimations().every(a=>a.playState==='finished'))
   const inspect=async(state)=>{await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));const observation=await room.evaluate(el=>{const rect=n=>n.getBoundingClientRect().toJSON(),text=el.querySelector('textarea'),send=el.querySelector('.pa-send'),scroll=el.querySelector('.pa-chat-scroll'),last=el.querySelector('[data-room-seq]:last-child'),footer=document.querySelector('.pa-status-bar'),checks=footer.querySelector('.pa-source-checks'),hit=n=>{const r=n.getBoundingClientRect();return n.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2))};return {room:rect(el),text:rect(text),send:rect(send),scroll:rect(scroll),last:rect(last),footer:rect(footer),checks:checks?rect(checks):null,viewport:{height:visualViewport.height,top:visualViewport.offsetTop,width:visualViewport.width},textHit:hit(text),sendHit:hit(send),following:Math.abs(scroll.scrollHeight-scroll.scrollTop-scroll.clientHeight)<=1}});results.push({tag,state,...observation});await page.screenshot({path:path.join(artifacts,tag+'-'+state+'.png')});{assert.ok(observation.textHit&&observation.sendHit,JSON.stringify(observation));assert.ok(observation.send.bottom<=observation.viewport.height+observation.viewport.top+1);assert.ok(observation.last.bottom<=observation.scroll.bottom+1,'last message clipped while following');assert.ok(observation.following,'resize released bottom following')}}
   await inspect('collapsed')
   if(baseline)continue
   assert.equal(await room.locator('.pa-room-reply').last().innerText(),'回覆')
   await click(room.locator('.pa-room-reply').last());await room.locator('.pa-room-draft-quote').waitFor();await page.waitForFunction(()=>document.activeElement===document.querySelector('.pa-room-layout textarea'))
   if(width<700){await click(page.locator('.pa-source-summary'));await inspect('expanded');await click(page.locator('.pa-source-summary'))
    for(const [state,height,top,scale] of [['keyboard',420,0,1],['zoom',500,26,1.5],['restored',780,0,1]]){
     await page.evaluate(({height,top,scale})=>{for(const [k,v]of Object.entries({height,offsetTop:top,scale}))Object.defineProperty(visualViewport,k,{configurable:true,value:v});visualViewport.dispatchEvent(new Event('resize'));visualViewport.dispatchEvent(new Event('scroll'))},{height,top,scale});await inspect(state);if(state==='keyboard'){await click(page.locator('.pa-source-summary'));await inspect('keyboard-expanded');await click(page.locator('.pa-source-summary'))}
    }
   }
   const draft=await text.inputValue();await click(room.getByRole('button',{name:'成員 (2)',exact:true}));await page.waitForFunction(()=>document.querySelector('.pa-room-members')?.contains(document.activeElement));const motion=await room.locator('.pa-room-members').evaluate(n=>({duration:getComputedStyle(n).animationDuration,name:getComputedStyle(n).animationName}));assert.equal(motion.duration,width===320?'0s':'0.3s');results.push({tag,state:'member-motion',...motion});await page.keyboard.press('Escape');await room.locator('.pa-room-members').waitFor({state:'hidden'});assert.equal(await text.inputValue(),draft)
   await click(send);await page.waitForFunction(()=>!document.querySelector('.pa-room-layout textarea').value);const quoted=room.locator('[data-quote-seq]').last();await quoted.waitFor();assert.match(await quoted.innerText(),/👍|Synthetic|Alice|Bob/)
   assert.equal(writes.at(-1).body.text,'Synthetic reply 👍');assert.ok(writes.at(-1).body.quote.seq>0)
   assert.deepEqual(errors,[])
  }catch(e){await page.screenshot({path:path.join(artifacts,tag+'-failure.png')});throw e}finally{await context.close()}
 }}finally{await browser.close()}
}console.log('PASS production host group composer viewport; cases='+results.length+'; external model calls=0')
}finally{await writeFile(path.join(artifacts,'layout-evidence.json'),JSON.stringify({baseline,results,writes,externalModelCalls:0},null,2));server.closeAllConnections();await new Promise(r=>server.close(r))}
`
