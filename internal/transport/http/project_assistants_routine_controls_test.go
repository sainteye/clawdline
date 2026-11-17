package http

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The production TypeScript client crosses authenticated TCP into the real Host
// and SQLite store. Only the provider is scripted; no native/model API is used.
func TestProjectAssistantRoutineControlsSourceClientTCP(t *testing.T) {
	f, p, server := newRoutineHumanFixture(t)
	paServerOK(t, f.send(t, f.local, "controls-main-before"))
	routineHumanIdle(t, f)
	client, err := filepath.Abs("../../../web/console/src/cloud/project-assistants.ts")
	if err != nil {
		t.Fatal(err)
	}
	key, _ := json.Marshal(f.key)
	script := `
import {pathToFileURL} from 'node:url';
const {ProjectAssistantSourceClient}=await import(pathToFileURL(process.env.CONTROLS_CLIENT));
const {default:assert}=await import('node:assert/strict');
const key=JSON.parse(process.env.CONTROLS_KEY),origin=process.env.CONTROLS_ORIGIN;
const grant={accountKey:'local',readerID:'local',machineID:key.machine_id,authorizationRevision:'synthetic',apiLevel:75,paired:true,online:true,writable:true};
let writes=0;
const transport=(path,init)=>{const headers=new Headers(init?.headers);headers.set('Authorization','Bearer '+process.env.CONTROLS_TOKEN);if(init?.method==='POST')writes++;return fetch(origin+path,{...init,headers})};
const source=new ProjectAssistantSourceClient(grant,transport,()=>grant),signal=new AbortController().signal;
const command=id=>({key,operation:'edit',idempotency_key:id,content_hash:''});
try {
 await source.status(signal);
 const got=await source.get(key,signal);
 const query={key,project:got.bot.project,conversation_id:got.bot.conversation_id,routine_id:'',cursor:'',page_size:100};
 let page=await source.routines(query,signal);
 await source.saveRoutine({command:command('controls-save'),fence:page.fence,id:'',expected_revision:0,name:'Synthetic event',instruction:'Report the synthetic event',triggers:[{type:'event',seconds:0,at:0,expression:'',timeZone:'',source:'github',event:'issues.opened',filters:[{key:'repo',value:'synthetic/repo'}]}],timeout_seconds:3600,enabled:true},signal);
 page=await source.routines(query,signal);
 assert.ok(page.definitions.length===1,'one saved event definition');
 const d=page.definitions[0];
 assert.ok(d.triggers[0].filters[0].key==='repo','persisted filter');

 const request={command:command('controls-view'),fence:page.fence,id:d.id,expected_revision:d.version,rotate:false};
 let hook=await source.routineHook(request,signal);
 assert.ok(!JSON.stringify(page).includes(hook.key),'metadata has no hook key');
 const oldKey=hook.key,rotation={...request,command:command('controls-rotate'),rotate:true};
 hook=await source.routineHook(rotation,signal);
 assert.ok(hook.key!==oldKey,'rotation changes capability');
 const replay=await source.routineHook(rotation,signal);
 assert.ok(replay.key===hook.key,'same rotation command returns same key');
 const hookSend=(secret,id)=>fetch(origin+hook.url,{method:'POST',headers:{Authorization:'Bearer '+secret,'Content-Type':'application/json','X-GitHub-Event':'issues','X-GitHub-Delivery':id},body:JSON.stringify({action:'opened',repository:{full_name:'synthetic/repo'},issue:{title:'synthetic'}})});
 assert.ok((await hookSend(oldKey,'controls-old-key')).status===401,'old capability refused');
 const admitted=await hookSend(hook.key,'controls-delivery');
 assert.ok(admitted.status===200,'delivery admitted');
 const first=await admitted.json();
 const duplicate=await hookSend(hook.key,'controls-delivery');
 assert.ok(duplicate.status===200,'same delivery acknowledged');
 assert.ok(JSON.stringify(await duplicate.json())===JSON.stringify(first),'delivery preserves original run');
 let history;
 const until=Date.now()+5000;
 do {history=await source.routines({...query,routine_id:d.id},signal);if(history.runs[0]?.state==='succeeded')break;await new Promise(r=>setTimeout(r,10))}while(Date.now()<until);
 assert.ok(history.runs.length===1&&history.runs[0].state==='succeeded','actual scripted run completed');
 assert.ok(history.runs[0].source==='webhook'&&history.runs[0].delivery_id==='controls-delivery','history retains event provenance');
 assert.ok(!JSON.stringify(history).includes(hook.key),'history excludes key');
 const readerGrant={...grant,writable:false},reader=new ProjectAssistantSourceClient(readerGrant,transport,()=>readerGrant);
 await reader.status(signal);
 const before=writes;
 await assert.rejects(reader.routineHook({...request,command:command('controls-reader')},signal),e=>e.error?.code==='pa_read_only');
 assert.ok(writes===before,'reader never transports key operation');
 await assert.rejects(source.routineHook({...request,command:command('controls-wrong-project'),fence:{...page.fence,project:{...query.project,canonical_id:'foreign'}}},signal),e=>['pa_scope_denied','pa_stale_generation'].includes(e.error?.code));
 console.log('REAL_ROUTINE_CONTROLS_TCP_OK save/filter/view/rotate/exact-replay/admit/dedupe/history/read-only/scope; native=0 model=0');
} catch(error) {console.error('ROUTINE_CONTROLS_FAILED '+(error.error?.code??error.message??error.name));process.exitCode=1}
`
	cmd := exec.Command("node", "--experimental-strip-types", "--input-type=module", "-e", script)
	cmd.Env = append(os.Environ(), "CONTROLS_CLIENT="+client, "CONTROLS_ORIGIN="+server.URL, "CONTROLS_TOKEN="+f.local, "CONTROLS_KEY="+string(key))
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("production SourceClient TCP: %v\n%s", err, raw)
	}
	if !strings.Contains(string(raw), "REAL_ROUTINE_CONTROLS_TCP_OK") {
		t.Fatal("missing client receipt")
	}
	var count int
	var state, source, delivery string
	err = f.s.store.ProjectAssistantDatabase().QueryRow(`SELECT count(*),state,json_extract(CAST(data AS TEXT),'$.source'),json_extract(CAST(data AS TEXT),'$.delivery_id') FROM project_assistant_routine_runs WHERE json_extract(CAST(data AS TEXT),'$.delivery_id')='controls-delivery'`).Scan(&count, &state, &source, &delivery)
	if err != nil || count != 1 || state != "succeeded" || source != "webhook" || delivery != "controls-delivery" {
		t.Fatal("persisted event receipt mismatch", count, state, source, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sends) != 2 {
		t.Fatal("dedupe sent an extra scripted prompt", len(p.sends))
	}
	t.Log(strings.TrimSpace(string(raw)))
}
