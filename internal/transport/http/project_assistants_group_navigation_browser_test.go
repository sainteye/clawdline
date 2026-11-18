package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
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
	runGroupComposerViewportBrowser(t, dir, "")
}

// Terminal outcomes stay different in the stored member turns even though
// none of them produces a member answer. The room must not silently vanish
// into an idle transcript or present a synthetic answer under a Bot's name.
func TestGroupNoReplyVisibleBrowser(t *testing.T) {
	dir := os.Getenv("CLAWDLINE_GROUP_NO_REPLY_ARTIFACTS")
	if dir == "" {
		t.Skip("set CLAWDLINE_GROUP_NO_REPLY_ARTIFACTS and PLAYWRIGHT_MODULE")
	}
	for _, outcome := range []string{"pass", "failed", "offline", "denied"} {
		t.Run(outcome, func(t *testing.T) { runGroupComposerViewportBrowser(t, filepath.Join(dir, outcome), outcome) })
	}
}

type noReplyRoomProvider struct {
	*routineHumanProvider
	outcome string
}

func (p *noReplyRoomProvider) Open(ctx context.Context, q app.OpenRequest, sink func(context.Context, app.ProtocolEvent) error) (app.ProviderSession, error) {
	if p.outcome == "offline" {
		return nil, &pa.Failure{Code: pa.Offline, Field: "synthetic_provider"}
	}
	if p.outcome == "denied" {
		return nil, &pa.Failure{Code: pa.ScopeDenied, Field: "synthetic_provider"}
	}
	inner, err := p.routineHumanProvider.Open(ctx, q, sink)
	return &noReplyRoomSession{ProviderSession: inner, outcome: p.outcome}, err
}

type noReplyRoomSession struct {
	app.ProviderSession
	outcome string
}

func (s *noReplyRoomSession) Send(ctx context.Context, q app.PromptRequest) (app.PendingPrompt, error) {
	pending, err := s.ProviderSession.Send(ctx, q)
	return &noReplyRoomPrompt{PendingPrompt: pending, outcome: s.outcome}, err
}

type noReplyRoomPrompt struct {
	app.PendingPrompt
	outcome string
}

func (p *noReplyRoomPrompt) Wait(context.Context) (app.PromptResult, error) {
	if p.outcome == "failed" {
		return app.PromptResult{}, fmt.Errorf("synthetic connection lost after send")
	}
	return app.PromptResult{FinalText: pa.RoomPass}, nil
}

func runGroupComposerViewportBrowser(t *testing.T, dir, outcome string) {
	t.Helper()
	var providers []app.Provider
	if outcome != "" {
		providers = append(providers, &noReplyRoomProvider{routineHumanProvider: &routineHumanProvider{}, outcome: outcome})
	}
	f := newGroupHTTPFixture(t, providers...)
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
	if outcome != "" {
		cmd.Env = append(cmd.Env, "BOTS_EXPECT_NO_REPLY=1")
	}
	out, err := cmd.CombinedOutput()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "browser.log"), out, 0600)
	if err != nil {
		t.Fatalf("group composer viewport: %v\n%s", err, out)
	}
	t.Log(string(out))
	if outcome != "" {
		f.rooms.Idle()
		var diagnostics []map[string]string
		for _, bot := range f.bots[:2] {
			exported, err := f.repo.Export(t.Context(), bot.Key)
			if err != nil {
				t.Fatal(err)
			}
			if len(exported.Turns) == 0 {
				t.Fatal("fixture produced no member turns")
			}
			foundDiagnosis := false
			wantCode := map[string]pa.ErrorCode{"failed": pa.OutcomeUnknown, "offline": pa.Offline, "denied": pa.ScopeDenied}[outcome]
			for _, turn := range exported.Turns {
				var code pa.ErrorCode
				if turn.Failure != nil {
					code = turn.Failure.Code
				}
				diagnostics = append(diagnostics, map[string]string{"member": bot.Name, "state": string(turn.State), "code": string(code)})
				// A later room request can be refused by the existing unknown-effect
				// barrier. Preserve that refusal rather than relabeling it as pass.
				if outcome == "pass" && (turn.State != pa.TurnSucceeded || turn.Failure != nil) || outcome != "pass" && (turn.Failure == nil || turn.State != pa.TurnUnknown && turn.State != pa.TurnFailed || code != wantCode && code != pa.ExecutionEffectUnknown) {
					t.Fatalf("%s lost its distinct terminal diagnosis: %+v", outcome, turn)
				}
				foundDiagnosis = foundDiagnosis || code == wantCode
			}
			if outcome != "pass" && !foundDiagnosis {
				t.Fatalf("%s lost its original failure code %s", outcome, wantCode)
			}
			if outcome == "pass" {
				found := false
				for _, message := range exported.Messages {
					found = found || message.Kind == pa.AssistantMessage && strings.TrimSpace(message.Text) == pa.RoomPass
				}
				if !found {
					t.Fatal("the real pass was lost from the member conversation")
				}
			}
		}
		data, err := json.MarshalIndent(diagnostics, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "member-diagnostics.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
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
let code='import "../../src/legacy/tokens.css";\nimport "../../src/legacy/base.css";\nimport "../../src/legacy/js/core/env.js";\n'+await readFile(host,'utf8')
if(process.env.BOTS_APP_SHELL)code=code.replace('import React from "react"','import React from "react"; import App from "../../src/App"; import "../../src/legacy/shell.css"; import "../../src/legacy/pages.css"; import "../../src/legacy/responsive.css"; import "../../src/shell.css"').replace('return <main><p>Synthetic origin view</p>{host.entry}{host.content}</main>','return <App projectAssistants={registry}/>').replace('const host=useProjectAssistantsHost({registry,apiLevel:89,cloud:false,language})','').replace('getElementById("app")','getElementById("root")')
const bundle=await build({stdin:{contents:code,resolveDir:path.dirname(host),loader:'tsx'},bundle:true,write:false,outfile:'host.js',format:'esm',platform:'browser',target:'es2022',jsx:'automatic',loader:{'.html':'text'},external:['/hero-orchestration-v4-task-clinic.webp'],define:{'process.env.NODE_ENV':'"development"',TEMPLATE_MACHINE:'"machine"','import.meta.env':'{}'},plugins:[{name:'app-shell',setup(p){p.onLoad({filter:/\/App\.tsx$/},async a=>({contents:(await readFile(a.path,'utf8')).replace('import.meta.glob<{ page?: PageModule }>("./pages/*.tsx", { eager: true })','({})'),loader:'tsx',resolveDir:path.dirname(a.path)}))}},...baseline?[{name:'baseline',setup(p){p.onLoad({filter:/project-assistants\/(GroupRoom\.tsx|styles\.css)$/},a=>({contents:execFileSync('git',['show',baseline+':'+path.relative(path.resolve(root,'../..'),a.path)],{encoding:'utf8'}),loader:a.path.endsWith('.css')?'css':'tsx',resolveDir:path.dirname(a.path)}))}}]:[]]})
const files=new Map(bundle.outputFiles.map(f=>['/'+path.basename(f.path),f.contents])),writes=[];let nativeDone;const nativeFinished=new Promise(r=>nativeDone=r)
const server=createServer(async(req,res)=>{try{
 const u=new URL(req.url,'http://fixture')
 if(u.pathname==='/done'){nativeDone();res.end('ok');return}
 if(u.pathname==='/evidence'){const chunks=[];for await(const c of req)chunks.push(c);await writeFile(path.join(artifacts,'native-events.jsonl'),Buffer.concat(chunks)+'\n',{flag:'a'});res.end('ok');return}
 if(u.pathname.startsWith('/v1/')){const chunks=[];for await(const c of req)chunks.push(c);const body=Buffer.concat(chunks);if(req.method!=='GET')writes.push({path:u.pathname,body:JSON.parse(body)});const h={Authorization:'Bearer person'};for(const k of ['content-type','idempotency-key'])if(req.headers[k])h[k]=req.headers[k];const a=await fetch(process.env.BOTS_ORIGIN+u.pathname+u.search,{method:req.method,headers:h,...body.length?{body}:{}});res.writeHead(a.status,{'Content-Type':'application/json'});res.end(Buffer.from(await a.arrayBuffer()));return}
 if(u.pathname.startsWith('/catalog/')){res.setHeader('Content-Type','application/json');res.end(await readFile(path.join(root,'public/catalogs',path.basename(u.pathname))));return}
 if(files.has(u.pathname)){res.setHeader('Content-Type',u.pathname.endsWith('.css')?'text/css':'text/javascript');res.end(files.get(u.pathname));return}
 res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/host.css">'+(process.env.BOTS_APP_SHELL?'<div id="root"></div>':'<style>html,body,#app{height:100%;margin:0;background:#080808}</style><div id="app"></div>')+'<script type="module" src="/host.js"></script>')
}catch(e){res.writeHead(500);res.end(String(e))}})
await new Promise(r=>server.listen(Number(process.env.BOTS_NATIVE_PORT??0),'127.0.0.1',r))
if(process.env.BOTS_NATIVE){
 const recordNativeEvents=function(){
 const snapshot=(event)=>{const room=document.querySelector('.pa-room-layout'),rect=n=>n?.getBoundingClientRect().toJSON(),ancestors=[];for(let n=room;n;n=n.parentElement){const s=getComputedStyle(n);ancestors.push({tag:n.tagName,class:n.className,hidden:n.hidden,display:s.display,visibility:s.visibility,overflow:s.overflow,rect:rect(n),scrollTop:n.scrollTop})}const text=room?.querySelector('textarea'),r=text?.getBoundingClientRect();fetch('/evidence',{method:'POST',body:JSON.stringify({event,time:performance.now(),active:document.activeElement?.outerHTML.slice(0,200),room:!!room,ancestors,viewport:{height:visualViewport.height,top:visualViewport.offsetTop,pageTop:visualViewport.pageTop,layout:innerHeight,scrollY},rootStyle:document.documentElement.style.cssText,history:history.state,text:rect(text),hit:r?document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)?.outerHTML.slice(0,200):null})})};for(const name of ['pointerdown','touchstart','focusin','focusout','click'])document.addEventListener(name,()=>snapshot(name),true);visualViewport.addEventListener('resize',()=>snapshot('viewport-resize'));visualViewport.addEventListener('scroll',()=>snapshot('viewport-scroll'));setInterval(()=>snapshot('settled'),1000);
 }
 files.set('/host.js',Buffer.concat([Buffer.from('('+recordNativeEvents.toString()+')();\n'),files.get('/host.js')]))
 console.log('NATIVE_READY http://127.0.0.1:'+server.address().port);await Promise.race([nativeFinished,new Promise(r=>setTimeout(r,150000))]);server.closeAllConnections();await new Promise(r=>server.close(r));process.exit(0)
}
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
   await room.locator('[data-room-seq]').last().waitFor()
   if(process.env.BOTS_EXPECT_NO_REPLY){await room.locator('.pa-room-unanswered').waitFor();assert.match(await room.locator('.pa-room-unanswered').innerText(),/目前沒有成員回覆/);assert.equal(await room.locator('.pa-room-author').count(),0);assert.equal(await room.locator('.pa-room-unanswered').getAttribute('role'),'status')}
   await page.waitForFunction(()=>document.querySelector('.pa-window').getAnimations().every(a=>a.playState==='finished'))
   const inspect=async(state)=>{await page.evaluate(()=>new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r))));const observation=await room.evaluate(el=>{const rect=n=>n.getBoundingClientRect().toJSON(),text=el.querySelector('textarea'),send=el.querySelector('.pa-send'),scroll=el.querySelector('.pa-chat-scroll'),last=el.querySelector('[data-room-seq]:last-child'),footer=document.querySelector('.pa-status-bar'),checks=footer.querySelector('.pa-source-checks'),hit=n=>{const r=n.getBoundingClientRect();return n.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2))};return {room:rect(el),text:rect(text),send:rect(send),scroll:rect(scroll),last:rect(last),footer:rect(footer),checks:checks?rect(checks):null,viewport:{height:visualViewport.height,top:visualViewport.offsetTop,width:visualViewport.width},textHit:hit(text),sendHit:hit(send),following:Math.abs(scroll.scrollHeight-scroll.scrollTop-scroll.clientHeight)<=1}});results.push({tag,state,...observation});await page.screenshot({path:path.join(artifacts,tag+'-'+state+'.png')});{assert.ok(observation.textHit&&observation.sendHit,JSON.stringify(observation));assert.ok(observation.send.bottom<=observation.viewport.height+observation.viewport.top+1);assert.ok(observation.last.bottom<=observation.scroll.bottom+1,'last message clipped while following');assert.ok(observation.following,'resize released bottom following')}}
   const fieldMetrics=async(state)=>{const value=await text.evaluate(el=>{const form=el.closest('form'),mirror=form.querySelector('.pa-editor-mirror'),send=form.querySelector('.pa-send'),r=el.getBoundingClientRect(),f=form.getBoundingClientRect(),m=mirror.getBoundingClientRect(),b=send.getBoundingClientRect(),s=getComputedStyle(el),mstyle=getComputedStyle(mirror);return {value:el.value,leftInset:r.left-f.left+parseFloat(s.paddingLeft),rightInset:b.left-r.right+parseFloat(s.paddingRight),lineHeight:parseFloat(s.lineHeight),padding:[s.paddingTop,s.paddingRight,s.paddingBottom,s.paddingLeft],mirrorPadding:[mstyle.paddingTop,mstyle.paddingRight,mstyle.paddingBottom,mstyle.paddingLeft],height:r.height,mirrorHeight:m.height,singleLineCenter:r.top+parseFloat(s.paddingTop)+parseFloat(s.lineHeight)/2,sendCenter:b.top+b.height/2}});results.push({tag,state,...value});await page.screenshot({path:path.join(artifacts,tag+'-'+state+'.png')});if(!baseline){assert.ok(value.leftInset>=14,'text/caret too close to rounded left edge: '+JSON.stringify(value));assert.ok(value.rightInset>=14,'text/caret too close to send button');assert.deepEqual(value.padding,value.mirrorPadding,'measurement differs from editable content');if(!value.value.includes('\n'))assert.ok(Math.abs(value.singleLineCenter-value.sendCenter)<=2,'single line and send are not centered')}}
   await text.fill('');await fieldMetrics('empty')
   await text.fill('嗨，大家好 👍');await fieldMetrics('filled')
   await text.fill('第一行：群聊問候\n第二行：保留多行與游標\n第三行：確認文字不會貼邊');await fieldMetrics('multiline');await inspect('multiline-viewport')
   await text.fill('Synthetic reply 👍');await inspect('collapsed')
   if(width<700){
    // Start from a genuinely unfocused input, then replay the client/document
    // geometry captured from the iOS soft-keyboard pan (pageTop === scrollY,
    // while offsetTop still contains the pan).
    await text.evaluate(el=>el.blur());await click(text)
    assert.equal(await text.evaluate(el=>document.activeElement===el),true)
    await page.evaluate(()=>{const saved=['scrollY','innerHeight'].map(k=>[k,Object.getOwnPropertyDescriptor(window,k)]);window.restoreGroupPan=()=>{for(const [k,d]of saved)Object.defineProperty(window,k,d)};document.querySelector('.pa-window').style.transform='translateY(-360px)';for(const [k,v]of Object.entries({scrollY:360,innerHeight:420}))Object.defineProperty(window,k,{configurable:true,value:v});for(const [k,v]of Object.entries({height:420,offsetTop:360,pageTop:360}))Object.defineProperty(visualViewport,k,{configurable:true,value:v});visualViewport.dispatchEvent(new Event('scroll'))})
    await inspect('native-client-pan')
    await page.evaluate(()=>{document.querySelector('.pa-window').style.removeProperty('transform');window.restoreGroupPan();delete window.restoreGroupPan;for(const k of ['height','offsetTop','pageTop'])delete visualViewport[k];visualViewport.dispatchEvent(new Event('resize'))})
   }
   if(baseline)continue
   assert.equal(await room.locator('.pa-room-reply').last().innerText(),'回覆')
   await click(room.locator('.pa-room-reply').last());await room.locator('.pa-room-draft-quote').waitFor();await page.waitForFunction(()=>document.activeElement===document.querySelector('.pa-room-layout textarea'))
   if(width<700){await click(page.locator('.pa-source-summary'));await inspect('expanded');await click(page.locator('.pa-source-summary'))
    for(const [state,height,top,scale] of [['keyboard',420,0,1],['zoom',500,26,1.5],['restored',780,0,1],['keyboard-reopened',420,0,1],['restored-again',780,0,1]]){
     await page.evaluate(({height,top,scale})=>{for(const [k,v]of Object.entries({height,offsetTop:top,pageTop:scrollY+top,scale}))Object.defineProperty(visualViewport,k,{configurable:true,value:v});visualViewport.dispatchEvent(new Event('resize'));visualViewport.dispatchEvent(new Event('scroll'))},{height,top,scale});await inspect(state);if(state==='keyboard'){await click(page.locator('.pa-source-summary'));await inspect('keyboard-expanded');await click(page.locator('.pa-source-summary'))}
    }
   }
   await text.fill('@Bo');await room.getByRole('option',{name:'Bob'}).waitFor();await click(room.getByRole('option',{name:'Bob'}));assert.equal(await text.inputValue(),'@Bob ');await fieldMetrics('mention');await text.fill('Synthetic reply 👍')
   const draft=await text.inputValue();await click(room.getByRole('button',{name:'成員 (2)',exact:true}));await page.waitForFunction(()=>document.querySelector('.pa-room-members')?.contains(document.activeElement));const motion=await room.locator('.pa-room-members').evaluate(n=>({duration:getComputedStyle(n).animationDuration,name:getComputedStyle(n).animationName}));assert.equal(motion.duration,width===320?'0s':'0.3s');results.push({tag,state:'member-motion',...motion});await page.keyboard.press('Escape');await room.locator('.pa-room-members').waitFor({state:'hidden'});assert.equal(await text.inputValue(),draft)
   await click(send);await page.waitForFunction(()=>!document.querySelector('.pa-room-layout textarea').value);const quoted=room.locator('[data-quote-seq]').last();await quoted.waitFor();assert.match(await quoted.innerText(),/👍|Synthetic|Alice|Bob/)
   assert.equal(writes.at(-1).body.text,'Synthetic reply 👍');assert.ok(writes.at(-1).body.quote.seq>0)
   assert.deepEqual(errors,[])
  }catch(e){await page.screenshot({path:path.join(artifacts,tag+'-failure.png')});throw e}finally{await context.close()}
 }}finally{await browser.close()}
}console.log('PASS production host group composer viewport; cases='+results.length+'; external model calls=0')
}finally{await writeFile(path.join(artifacts,'layout-evidence.json'),JSON.stringify({baseline,results,writes,externalModelCalls:0},null,2));server.closeAllConnections();await new Promise(r=>server.close(r))}
`
